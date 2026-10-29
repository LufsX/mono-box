package automation

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestUpdateIntervalWifiFailurePersistenceAndRestart(t *testing.T) {
	r := fixture(t)
	setJSON(t, r, `{"updateEnabled":true,"updateIntervalHours":1}`)
	var clock atomic.Int64
	clock.Store(time.Now().UnixMilli())
	var wifi atomic.Bool
	var calls atomic.Int32
	deps := workerDeps{poll: time.Millisecond, now: func() time.Time { return time.UnixMilli(clock.Load()) },
		wifi: func(context.Context) (bool, error) { return wifi.Load(), nil },
		update: func(context.Context, base.CommitGuard) (int, error) {
			calls.Add(1)
			return 1, errors.New("https://secret/token raw core")
		}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.updateWorker(ctx, deps) }()
	time.Sleep(10 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatal("first run ignored interval")
	}
	clock.Add(int64((time.Hour + time.Second) / time.Millisecond))
	waitFor(t, func() bool { state, _ := r.Get(); return state.Update.State == "waiting_wifi" })
	if calls.Load() != 0 {
		t.Fatal("updated on cellular")
	}
	wifi.Store(true)
	waitFor(t, func() bool { state, _ := r.Get(); return state.Update.State == "failed" })
	state, _ := r.Get()
	if state.Update.LastAttemptAt == 0 || state.Update.Updated != 1 || state.Update.LastSuccessAt != 0 || state.Update.LastError == "https://secret/token raw core" {
		t.Fatalf("%+v", state.Update)
	}
	cancel()
	<-done
	ctx, cancel = context.WithCancel(context.Background())
	done = make(chan struct{})
	go func() { defer close(done); r.updateWorker(ctx, deps) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	<-done
	if calls.Load() != 1 {
		t.Fatal("watch restart repeated failed attempt")
	}
	clock.Add(int64(time.Hour / time.Millisecond))
	deps.update = func(context.Context, base.CommitGuard) (int, error) { calls.Add(1); return 2, nil }
	ctx, cancel = context.WithCancel(context.Background())
	done = make(chan struct{})
	go func() { defer close(done); r.updateWorker(ctx, deps) }()
	waitFor(t, func() bool { state, _ := r.Get(); return state.Update.LastSuccessAt > 0 })
	cancel()
	<-done
	state, _ = r.Get()
	if state.Update.Updated != 2 || state.Update.LastError != "" {
		t.Fatalf("%+v", state.Update)
	}
}

func TestUpdateSettingsChangeAndShutdownCancelAndJoin(t *testing.T) {
	r := fixture(t)
	setJSON(t, r, `{"updateEnabled":true,"updateWifiOnly":false,"updateIntervalHours":1}`)
	_, err := r.locked(context.Background(), func(rec *record) error { rec.Update.NextRunAt = 1; return nil })
	if err != nil {
		t.Fatal(err)
	}
	var active, calls atomic.Int32
	deps := workerDeps{poll: time.Millisecond, now: time.Now, update: func(ctx context.Context, _ base.CommitGuard) (int, error) {
		calls.Add(1)
		active.Add(1)
		defer active.Add(-1)
		<-ctx.Done()
		return 9, ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.updateWorker(ctx, deps) }()
	waitFor(t, func() bool { return active.Load() == 1 })
	settings := defaultSettings()
	if _, err := r.set(settings); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return active.Load() == 0 })
	cancel()
	<-done
	state, _ := r.Get()
	if state.Update.State != "disabled" || state.Update.Updated != 9 {
		t.Fatalf("old result accepted: %+v", state)
	}
	// A new watch has no overdue work after a durable failed/cancelled attempt.
	var rec record
	if err := base.ReadPrivateJSON(filepath.Join(r.RunDir(), "automation-status.json"), &rec, stateLimit); err != nil {
		t.Fatal(err)
	}
	if rec.Update.LastAttemptAt == 0 || rec.Update.NextRunAt != 0 {
		t.Fatalf("%+v", rec)
	}
	if calls.Load() != 1 {
		t.Fatal("parallel tasks started")
	}
}
