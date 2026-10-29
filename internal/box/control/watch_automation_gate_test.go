package control

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/automation"
)

func TestWatchSignalsAutomationOnlyAfterAndroidBootReady(t *testing.T) {
	r := controllerTestRuntime(t, "")
	controllerTestWrite(t, filepath.Join(r.Root, "manual"), "")
	controllerTestWrite(t, filepath.Join(r.Root, "scripts", "box.config"), "auto_update_config=true\nconfig_update_wifi_only=false\nconfig_update_interval_hours=1\n")
	controllerTestWrite(t, filepath.Join(r.RunDir(), "automation-status.json"), `{"settings":{"recoveryEnabled":false,"updateEnabled":true,"updateWifiOnly":false,"updateIntervalHours":1},"recovery":{"state":"disabled"},"update":{"state":"scheduled","nextRunAt":1}}`)
	deps := controllerTestDeps(t)
	var ready atomic.Bool
	var checked atomic.Int32
	deps.ready = func(context.Context) (bool, error) { checked.Add(1); return ready.Load(), nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.watch(ctx, deps) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	controllerTestWait(t, func() bool { return checked.Load() >= 3 })
	state, err := automation.New(r.Runtime).Get()
	if err != nil || state.Update.LastAttemptAt != 0 {
		t.Fatalf("automation ran before boot ready: %+v %v", state, err)
	}
	ready.Store(true)
	controllerTestWait(t, func() bool {
		state, err := automation.New(r.Runtime).Get()
		return err == nil && state.Update.LastAttemptAt > 0
	})
	// A persisted attempt prevents another update on subsequent watcher polls.
	state, _ = automation.New(r.Runtime).Get()
	attempt := state.Update.LastAttemptAt
	time.Sleep(5 * deps.interval)
	state, _ = automation.New(r.Runtime).Get()
	if state.Update.LastAttemptAt != attempt {
		t.Fatal("idle watcher repeated scheduled update")
	}
}
