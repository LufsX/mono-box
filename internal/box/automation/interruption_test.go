package automation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
	"github.com/LufsX/mono-box/internal/box/service"
	"github.com/LufsX/mono-box/internal/box/storage"
	"github.com/LufsX/mono-box/internal/box/testutil"
)

func TestPartialUpdateInterruptionPersistsOnSettingsChangeAndWatchStop(t *testing.T) {
	for _, kind := range []string{"settings", "disable", "watch-stop"} {
		t.Run(kind, func(t *testing.T) {
			r := guardedFixture(t)
			settings := defaultSettings()
			settings.UpdateEnabled = true
			settings.UpdateWifiOnly = false
			settings.UpdateIntervalHours = 168
			if _, err := r.set(settings); err != nil {
				t.Fatal(err)
			}
			secondStarted := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/a" {
					_, _ = w.Write([]byte("new-A"))
					return
				}
				close(secondStarted)
				<-req.Context().Done()
			}))
			defer server.Close()
			writeSubscription(t, r, "a.yaml", server.URL+"/a")
			writeSubscription(t, r, "b.yaml", server.URL+"/b")
			_, err := r.locked(context.Background(), func(rec *record) error { rec.Update.NextRunAt = 1; return nil })
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			deps := workerDeps{poll: time.Millisecond, now: time.Now, update: func(ctx context.Context, guard base.CommitGuard) (int, error) {
				calls.Add(1)
				return storage.New(r.Runtime).ScheduledUpdateGuarded(ctx, guard)
			}}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { defer close(done); r.updateWorker(ctx, deps) }()
			defer cancel()
			<-secondStarted
			state, err := r.Get()
			if err != nil || state.Update.Updated != 1 {
				t.Fatalf("commit progress missing while B downloading: %+v %v", state, err)
			}
			switch kind {
			case "settings":
				settings.UpdateWifiOnly = true
				if _, err := r.set(settings); err != nil {
					t.Fatal(err)
				}
			case "disable":
				settings.UpdateEnabled = false
				if _, err := r.set(settings); err != nil {
					t.Fatal(err)
				}
			case "watch-stop":
				cancel()
			}
			if kind != "watch-stop" {
				waitFor(t, func() bool { state, _ := r.Get(); return state.Update.LastError != "" })
				cancel()
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("worker did not join HTTP cancellation")
			}
			state, err = r.Get()
			expected := "interrupted"
			if kind == "disable" {
				expected = "disabled"
			}
			if err != nil || state.Update.State != expected || state.Update.Updated != 1 || !strings.Contains(state.Update.LastError, "interrupted") {
				t.Fatalf("%+v %v", state.Update, err)
			}
			if kind != "disable" {
				delay := time.Until(time.UnixMilli(state.Update.NextRunAt))
				if delay <= 0 || delay > interruptionRetry+time.Second {
					t.Fatalf("interrupted task retained 168 hour schedule: %s", delay)
				}
			} else if state.Update.NextRunAt != 0 {
				t.Fatal("disabled update remained scheduled")
			}
			// Restart leaves the interrupted state and committed count visible,
			// without immediately repeating a partial batch.
			restartCtx, restartCancel := context.WithCancel(context.Background())
			restartDone := make(chan struct{})
			go func() { defer close(restartDone); r.updateWorker(restartCtx, deps) }()
			time.Sleep(10 * time.Millisecond)
			restartCancel()
			<-restartDone
			state, _ = r.Get()
			if state.Update.State != expected || state.Update.Updated != 1 || calls.Load() != 1 {
				t.Fatalf("restart lost facts: %+v", state.Update)
			}
		})
	}
}

func TestRestartNormalizesResidualTaskStates(t *testing.T) {
	r := fixture(t)
	setJSON(t, r, `{"recoveryEnabled":true,"updateEnabled":true}`)
	_, err := r.locked(context.Background(), func(rec *record) error {
		rec.Recovery.State = "recovering"
		rec.Recovery.Attempts = 2
		rec.Update.State = "updating"
		rec.Update.Updated = 3
		rec.Update.NextRunAt = time.Now().Add(168 * time.Hour).UnixMilli()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	r.resumeInterrupted(true, now)
	r.resumeInterrupted(false, now)
	state, err := r.Get()
	if err != nil || state.Update.State != "interrupted" || state.Update.Updated != 3 || state.Update.LastError == "" || state.Recovery.State != "waiting" || state.Recovery.LastError == "" {
		t.Fatalf("%+v %v", state, err)
	}
	if state.Update.NextRunAt != now.Add(interruptionRetry).UnixMilli() {
		t.Fatal("residual update did not reschedule")
	}
	if !base.Exists(filepath.Join(r.RunDir(), "automation-status.json")) {
		t.Fatal("normalization was not persisted")
	}
}

func TestRecoveryWatchStopFinalizesRealValidation(t *testing.T) {
	r := guardedFixture(t)
	setJSON(t, r, `{"recoveryEnabled":true}`)
	// The direct guarded recovery tests cover the exact start barrier. This
	// worker test covers cancelling a real validator and persisting its result.
	lock, err := base.AcquireServiceLock(r.RunDir(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.WriteServiceIntent(true)
	_ = lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	testutil.Write(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte("block-recovery"), 0600)
	var clock atomic.Int64
	clock.Store(time.Now().UnixMilli())
	deps := workerDeps{poll: time.Millisecond, now: func() time.Time { return time.UnixMilli(clock.Load()) },
		snapshot: func() (intentSnapshot, error) {
			intent, pid, err := service.New(r.Runtime).RecoverySnapshot()
			return intentSnapshot{revision: intent.Revision, running: intent.Running, pid: pid}, err
		},
		recover: func(ctx context.Context, revision uint64, guard base.CommitGuard) error {
			return service.New(r.Runtime).RecoverGuarded(ctx, revision, guard)
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.recoveryWorker(ctx, deps) }()
	waitFor(t, func() bool { state, _ := r.Get(); return state.Recovery.NextAttemptAt > 0 })
	clock.Add(10000)
	waitMarker(t, r, "block-recovery")
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("real validator did not cancel")
	}
	state, err := r.Get()
	if err != nil || state.Recovery.State == "recovering" || !strings.Contains(state.Recovery.LastError, "interrupted") {
		t.Fatalf("%+v %v", state.Recovery, err)
	}
}

func TestShutdownIsBoundedEvenForUnresponsiveTask(t *testing.T) {
	r := fixture(t)
	setJSON(t, r, `{"updateEnabled":true,"updateWifiOnly":false}`)
	_, err := r.locked(context.Background(), func(rec *record) error { rec.Update.NextRunAt = 1; return nil })
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan struct{})
	deps := workerDeps{poll: time.Millisecond, now: time.Now, update: func(context.Context, base.CommitGuard) (int, error) {
		close(started)
		<-release
		close(returned)
		return 0, errors.New("broken transport")
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.updateWorker(ctx, deps) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(taskStopTimeout + time.Second):
		t.Fatal("shutdown waited indefinitely")
	}
	state, _ := r.Get()
	if state.Update.State != "interrupted" {
		t.Fatalf("%+v", state.Update)
	}
	close(release)
	<-returned
}
