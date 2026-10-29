package control

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestWatchLogMaintenancePollsAndCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	failure := errors.New("log cleanup failed")
	// Gate the first call to prove maintenance does not wait for a first tick.
	entered := make(chan struct{})
	release := make(chan struct{})
	results, done := watchLogMaintenance(ctx, 10*time.Millisecond, func() error {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			return failure
		}
		return nil
	})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("initial maintenance missing")
	}
	close(release)
	select {
	case err := <-results:
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("maintenance error not reported")
	}
	select {
	case err := <-results:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("periodic maintenance missing")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("maintenance did not cancel")
	}
	before := calls.Load()
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != before {
		t.Fatal("maintenance continued after exit")
	}
}

func TestWatcherMaintenanceDoesNotBlockDisableAndJoinsOnExit(t *testing.T) {
	r := controllerTestRuntime(t, "")
	deps := controllerTestDeps(t)
	deps.interval = 10 * time.Millisecond
	deps.logInterval = time.Hour
	entered, release := make(chan struct{}), make(chan struct{})
	deps.maintainLogs = func(*Runtime) error {
		close(entered)
		<-release
		return nil
	}
	deps.ready = func(ctx context.Context) (bool, error) { <-ctx.Done(); return false, ctx.Err() }
	actions := make(chan string, 4)
	deps.service = func(_ *Runtime, action string) error { actions <- action; return nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.watch(ctx, deps) }()
	defer cancel()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("maintenance did not start before boot completed")
	}
	controllerTestWrite(t, filepath.Join(r.ModuleDir, "disable"), "")
	select {
	case action := <-actions:
		if action != "stop" {
			t.Fatal(action)
		}
	case <-time.After(time.Second):
		t.Fatal("log maintenance blocked disable")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("watch returned before maintenance finished: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	released = true
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not finish")
	}
	if base.Exists(filepath.Join(r.RunDir(), "watcher.pid")) {
		t.Fatal("watcher PID leaked")
	}
}
