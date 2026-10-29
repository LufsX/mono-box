package control

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/automation"
	"github.com/LufsX/mono-box/internal/box/base"
)

func TestWatchAutomationWaitsForBootGateAndCancelsWhileWaiting(t *testing.T) {
	r := controllerTestRuntime(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := r.watchAutomation(ctx, filepath.Join(r.Root, "rt_tables"), ready)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("boot wait leaked")
	}
	if base.Exists(filepath.Join(r.RunDir(), "config.lock")) {
		t.Fatal("automation ran before readiness")
	}
}

func TestWatcherAutomationBootGateAndManualRecoveryGate(t *testing.T) {
	r := controllerTestRuntime(t, "")
	controllerTestWrite(t, filepath.Join(r.Root, "manual"), "")
	controllerTestWrite(t, filepath.Join(r.Root, "scripts", "box.config"), "auto_recovery=true\nauto_update_config=true\nconfig_update_wifi_only=false\nconfig_update_interval_hours=1\n")
	controllerTestWrite(t, filepath.Join(r.RunDir(), "automation-status.json"), `{"settings":{"recoveryEnabled":true,"updateEnabled":true,"updateWifiOnly":false,"updateIntervalHours":1},"recovery":{"state":"waiting"},"update":{"state":"scheduled","nextRunAt":1}}`)
	lock, err := base.AcquireServiceLock(r.RunDir(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.WriteServiceIntent(true)
	_ = lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := r.watchAutomation(ctx, filepath.Join(r.Root, "rt_tables"), ready)
	defer func() { cancel(); <-done }()
	path := filepath.Join(r.RunDir(), "automation-status.json")
	before, _ := os.ReadFile(path)
	time.Sleep(20 * time.Millisecond)
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("automation changed state before boot ready")
	}
	close(ready)
	controllerTestWait(t, func() bool {
		state, err := automation.New(r.Runtime).Get()
		return err == nil && state.Update.LastAttemptAt > 0
	})
	state, err := automation.New(r.Runtime).Get()
	if err != nil || state.Recovery.NextAttemptAt != 0 || state.Recovery.Attempts != 0 {
		t.Fatalf("manual mode scheduled recovery: %+v %v", state, err)
	}
}

func TestSwitchStopRecordsDeadCoreIntent(t *testing.T) {
	r := controllerTestRuntime(t, "")
	controllerTestWrite(t, filepath.Join(r.Root, "scripts", "box.config"), "ctr_mode=switch\ntarget_cellular=stop\n")
	deps := controllerTestDeps(t)
	controllerTestWrite(t, deps.netPath, "")
	lock, err := base.AcquireServiceLock(r.RunDir(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.WriteServiceIntent(true)
	_ = lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.controller(context.Background(), []string{"w", deps.netPath}, deps); err != nil {
		t.Fatal(err)
	}
	lock, err = base.AcquireServiceLock(r.RunDir(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := r.ReadServiceIntent()
	_ = lock.Close()
	if err != nil || intent.Running {
		t.Fatalf("%+v %v", intent, err)
	}
}
