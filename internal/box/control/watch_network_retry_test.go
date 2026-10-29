package control

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWatcherNewNetworkBypassesFailedPolicyBackoff(t *testing.T) {
	r := controllerTestRuntime(t, "ctr_mode=switch\ntarget_wifi=start\ntarget_cellular=stop")
	deps := controllerTestDeps(t)
	deps.interval = 10 * time.Millisecond
	attempts := make(chan string, 32)
	deps.service = func(*Runtime, string) error { return nil }
	deps.running = func(*Runtime) (int, error) {
		data, err := os.ReadFile(deps.netPath)
		if err != nil {
			return 0, err
		}
		attempts <- string(data)
		return 0, errors.New("process lookup failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.watch(ctx, deps) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	for i := 0; i < 6; i++ {
		select {
		case <-attempts:
		case <-time.After(time.Second):
			t.Fatal("policy retry missing")
		}
	}
	// The old network now waits thirty intervals. A new snapshot must run
	// without inheriting that delay.
	controllerTestWrite(t, deps.netPath, "200 rmnet_data0")
	select {
	case snapshot := <-attempts:
		if !strings.Contains(snapshot, "rmnet_data0") {
			t.Fatal(snapshot)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("new network waited for old policy retry")
	}
}
