package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWatchRetryBackoffAndNewEvent(t *testing.T) {
	var retry watchRetry
	now := time.Unix(100, 0)
	for _, delay := range []time.Duration{1, 2, 4, 8, 16, 30, 30} {
		if !retry.ready("same-event", now) {
			t.Fatal("retry not ready at deadline")
		}
		retry.failed(now, time.Second)
		want := delay * time.Second
		if retry.next.Sub(now) != want {
			t.Fatalf("retry delay %s, want %s", retry.next.Sub(now), want)
		}
		if retry.ready("same-event", now.Add(want-time.Nanosecond)) {
			t.Fatal("retry ran before deadline")
		}
		now = now.Add(want)
	}
	retry.failed(now, time.Second)
	if !retry.ready("new-event", now) || retry.delay != 0 {
		t.Fatal("new event inherited the old retry delay")
	}
	retry.failed(now, time.Second)
	retry.reset()
	if !retry.ready("new-event", now) || retry.delay != 0 {
		t.Fatal("successful operation did not clear retry state")
	}
}

func TestWatchErrorsLimitRepeatsAndReportRecovery(t *testing.T) {
	r := controllerTestRuntime(t, "")
	var output strings.Builder
	r.Err = &output
	var state watchError
	now := time.Unix(100, 0)
	first := errors.New("first failure")
	for _, seconds := range []int{0, 1, 29, 30} {
		state.report(r, first, now.Add(time.Duration(seconds)*time.Second))
	}
	second := errors.New("changed failure")
	state.report(r, second, now.Add(31*time.Second))
	state.report(r, nil, now.Add(32*time.Second))
	state.report(r, second, now.Add(33*time.Second))
	if strings.Count(output.String(), "controller:") != 4 ||
		!strings.Contains(output.String(), "first failure (2 repeated errors suppressed)") ||
		strings.Count(output.String(), "changed failure") != 2 {
		t.Fatal(output.String())
	}
}

func TestWatcherDisableRemainsResponsiveDuringBootBackoff(t *testing.T) {
	r := controllerTestRuntime(t, "")
	deps := controllerTestDeps(t)
	deps.interval = 10 * time.Millisecond
	actions := make(chan string, 32)
	deps.service = func(_ *Runtime, action string) error {
		actions <- action
		if action == "start" {
			return errors.New("invalid core config")
		}
		return nil
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
	// Five failed starts leave the boot retry waiting for sixteen intervals.
	for i := 0; i < 5; i++ {
		select {
		case action := <-actions:
			if action != "start" {
				t.Fatal(action)
			}
		case <-time.After(time.Second):
			t.Fatal("boot retry missing")
		}
	}
	disable := filepath.Join(r.ModuleDir, "disable")
	controllerTestWrite(t, disable, "")
	select {
	case action := <-actions:
		if action != "stop" {
			t.Fatal(action)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("disable waited for boot retry")
	}
	if err := os.Remove(disable); err != nil {
		t.Fatal(err)
	}
	select {
	case action := <-actions:
		if action != "start" {
			t.Fatal(action)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("enable waited for boot retry")
	}
}

func TestWatcherEnableReappliesUnchangedNetworkPolicy(t *testing.T) {
	r := controllerTestRuntime(t, "ctr_mode=switch\ntarget_wifi=stop")
	deps := controllerTestDeps(t)
	deps.interval = 10 * time.Millisecond
	actions := make(chan string, 20)
	// policyTarget sees a running core and requests a stop on Wi-Fi.
	deps.running = func(*Runtime) (int, error) { return 100, nil }
	deps.service = func(_ *Runtime, action string) error { actions <- action; return nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.watch(ctx, deps) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	expect := func(want string) {
		t.Helper()
		select {
		case got := <-actions:
			if got != want {
				t.Fatalf("got %s, want %s", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing %s", want)
		}
	}
	expect("start")
	expect("stop")
	disable := filepath.Join(r.ModuleDir, "disable")
	controllerTestWrite(t, disable, "")
	expect("stop")
	// Let the disabled iteration finish before changing the desired state again.
	time.Sleep(3 * deps.interval)
	if err := os.Remove(disable); err != nil {
		t.Fatal(err)
	}
	expect("start")
	expect("stop")
}
