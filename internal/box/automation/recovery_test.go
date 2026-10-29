package automation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestRecoveryBoundedBackoffStableResetAndExplicitIntent(t *testing.T) {
	now := time.Unix(1000, 0)
	rec := record{State: State{Settings: defaultSettings()}}
	stopped := intentSnapshot{}
	if advanceRecovery(&rec, stopped, now) || rec.Recovery.State != "stopped" {
		t.Fatal("adopted initially stopped install")
	}
	observed := intentSnapshot{revision: 1, running: true}
	advanceRecovery(&rec, observed, now)
	for attempt := 1; attempt <= recoveryLimit; attempt++ {
		if rec.Recovery.NextAttemptAt <= now.UnixMilli() {
			t.Fatal("missing backoff")
		}
		now = time.UnixMilli(rec.Recovery.NextAttemptAt)
		if !advanceRecovery(&rec, observed, now) || rec.Recovery.Attempts != attempt {
			t.Fatalf("%+v", rec.Recovery)
		}
		// A successful startup followed by another quick crash must retain count.
		observed.pid = 42
		advanceRecovery(&rec, observed, now)
		observed.pid = 0
		advanceRecovery(&rec, observed, now)
	}
	if rec.Recovery.State != "paused" || advanceRecovery(&rec, observed, now.Add(24*time.Hour)) {
		t.Fatal("unbounded recovery")
	}
	observed.revision++
	advanceRecovery(&rec, observed, now)
	if rec.Recovery.Attempts != 0 {
		t.Fatal("explicit start did not reset pause")
	}
	observed.pid = 43
	rec.Recovery.Attempts = 3
	advanceRecovery(&rec, observed, now)
	advanceRecovery(&rec, observed, now.Add(stablePeriod))
	if rec.Recovery.Attempts != 0 || rec.Recovery.State != "running" {
		t.Fatalf("%+v", rec.Recovery)
	}
	observed.running = false
	observed.pid = 0
	observed.revision++
	if advanceRecovery(&rec, observed, now) || rec.Recovery.State != "stopped" {
		t.Fatal("manual stop recovered")
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition timed out")
}

func TestRecoveryCancellationAndNoParallelRestart(t *testing.T) {
	r := fixture(t)
	setJSON(t, r, `{"recoveryEnabled":true}`)
	var clock atomic.Int64
	clock.Store(time.Now().UnixMilli())
	var calls, active, maxActive atomic.Int32
	deps := workerDeps{poll: time.Millisecond, now: func() time.Time { return time.UnixMilli(clock.Load()) },
		snapshot: func() (intentSnapshot, error) { return intentSnapshot{revision: 1, running: true}, nil },
		recover: func(ctx context.Context, _ uint64, _ base.CommitGuard) error {
			calls.Add(1)
			n := active.Add(1)
			maxActive.Store(max(maxActive.Load(), n))
			defer active.Add(-1)
			<-ctx.Done()
			return errors.New("secret raw core output")
		}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.recoveryWorker(ctx, deps) }()
	waitFor(t, func() bool { state, _ := r.Get(); return state.Recovery.NextAttemptAt > 0 })
	clock.Add(int64((10 * time.Second) / time.Millisecond))
	waitFor(t, func() bool { return calls.Load() == 1 })
	_, err := r.set(defaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return active.Load() == 0 })
	state, _ := r.Get()
	if state.Recovery.State != "disabled" || state.Recovery.LastError != "" {
		t.Fatalf("stale result overwrote disabled state: %+v", state)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker leaked")
	}
	if maxActive.Load() != 1 {
		t.Fatal("parallel recovery")
	}
}
