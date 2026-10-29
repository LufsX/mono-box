//go:build linux || android

package box

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func serviceActionTestRuntime(t *testing.T) (*Runtime, *bytes.Buffer) {
	t.Helper()
	r := serviceTestRuntime(t)
	// Action reloads settings. Use the test process credentials on Linux.
	serviceTestWrite(t, filepath.Join(r.Root, "scripts", "box.config"),
		[]byte(fmt.Sprintf("CORE_USER_GROUP=%s\nctr_mode=mode\ntarget_wifi=global\n", r.Config.CoreUserGroup)), 0600)
	var output bytes.Buffer
	r.Err = &output
	return r, &output
}

func TestActionServiceLifecycle(t *testing.T) {
	r, output := serviceActionTestRuntime(t)
	apiCalls := 0
	r.HTTP = &http.Client{Transport: controllerRoundTrip(func(*http.Request) (*http.Response, error) {
		apiCalls++
		return nil, errors.New("API unavailable")
	})}
	serviceTestWrite(t, filepath.Join(r.RunDir(), "watcher.pid"), []byte("invalid watcher identity"), 0600)
	if err := r.Action("status", nil); !errors.Is(err, ErrStopped) {
		t.Fatalf("stopped status: %v", err)
	}
	for i := 0; i < 2; i++ {
		serviceTestRequire(t, r.Action("stop", nil))
	}
	output.Reset()
	serviceTestRequire(t, r.Action("start", nil))
	pid, err := r.RunningPID()
	serviceTestRequire(t, err)
	if strings.Contains(output.String(), ErrStopped.Error()) || !strings.Contains(output.String(), "mihomo started.") {
		t.Fatalf("first start: %s", output.String())
	}
	serviceTestRequire(t, r.Action("start", nil))
	serviceTestRequire(t, r.Action("status", nil))
	same, err := r.RunningPID()
	serviceTestRequire(t, err)
	if same != pid {
		t.Fatalf("repeated start replaced PID %d with %d", pid, same)
	}
	serviceTestRequire(t, r.Action("restart", nil))
	next, err := r.RunningPID()
	serviceTestRequire(t, err)
	if next == pid {
		t.Fatal("restart retained PID")
	}
	for i := 0; i < 2; i++ {
		serviceTestRequire(t, r.Action("stop", nil))
	}
	for _, action := range []string{"toggle", "", "toggle", ""} {
		serviceTestRequire(t, r.Action(action, nil))
	}
	if _, err := r.RunningPID(); !errors.Is(err, ErrStopped) {
		t.Fatalf("toggle did not stop: %v", err)
	}
	if apiCalls != 0 {
		t.Fatalf("service actions made %d API calls", apiCalls)
	}
	watcher, err := os.ReadFile(filepath.Join(r.RunDir(), "watcher.pid"))
	serviceTestRequire(t, err)
	if string(watcher) != "invalid watcher identity" {
		t.Fatalf("service action modified watcher: %q", watcher)
	}
}

func TestActionStartWithStalePID(t *testing.T) {
	for _, state := range []string{"missing", "malformed", "exited", "foreign"} {
		t.Run(state, func(t *testing.T) {
			r, output := serviceActionTestRuntime(t)
			switch state {
			case "malformed":
				serviceTestWrite(t, r.PIDFile(), []byte("not-a-pid"), 0600)
			case "exited":
				command := exec.Command("true")
				serviceTestRequire(t, command.Run())
				serviceTestWrite(t, r.PIDFile(), []byte(strconv.Itoa(command.Process.Pid)), 0600)
			case "foreign":
				command := exec.Command("sleep", "30")
				serviceTestRequire(t, command.Start())
				defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
				serviceTestWrite(t, r.PIDFile(), []byte(strconv.Itoa(command.Process.Pid)), 0600)
				defer func() { serviceTestRequire(t, command.Process.Signal(syscall.Signal(0))) }()
			}
			// A leftover identity cannot make a dead or unrelated PID match.
			serviceTestWrite(t, r.PIDFile()+".identity", []byte("{}"), 0600)
			serviceTestRequire(t, r.Action("start", nil))
			if strings.Contains(output.String(), ErrStopped.Error()) || !strings.Contains(output.String(), "mihomo started.") {
				t.Fatalf("stale PID startup: %s", output.String())
			}
			serviceTestRequire(t, r.Action("stop", nil))
		})
	}
}

func TestActionServiceMetadataFailure(t *testing.T) {
	r, output := serviceActionTestRuntime(t)
	serviceTestRequire(t, r.Action("start", nil))
	pid, err := r.RunningPID()
	serviceTestRequire(t, err)
	moduleProp := filepath.Join(r.ModuleDir, "module.prop")
	metadata, err := os.ReadFile(moduleProp)
	serviceTestRequire(t, err)
	serviceTestRequire(t, os.Remove(moduleProp))
	serviceTestRequire(t, os.Mkdir(moduleProp, 0700))
	// A bad module file must fail even when the core already has the requested
	// state. None of these failures may produce a lifecycle success message.
	for _, action := range []string{"start", "status", "stop", "stop"} {
		output.Reset()
		err := r.Action(action, nil)
		if err == nil || !strings.Contains(err.Error(), "update module.prop status to ") {
			t.Fatalf("%s lost metadata error: %v", action, err)
		}
		if output.Len() != 0 {
			t.Fatalf("%s logged success despite metadata failure: %s", action, output.String())
		}
	}
	if _, err := serviceProcIdentity(pid); !errors.Is(err, ErrStopped) {
		t.Fatalf("metadata error prevented process stop: %v", err)
	}
	if _, err := os.Stat(r.PIDFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("metadata error prevented PID cleanup: %v", err)
	}
	serviceTestRequire(t, os.Remove(moduleProp))
	serviceTestWrite(t, moduleProp, metadata, 0644)
	serviceTestRequire(t, r.Action("stop", nil))
	serviceTestState(t, r, "Stopped")
	serviceTestRequire(t, r.Action("start", nil))
	serviceTestRequire(t, r.Action("stop", nil))
}

func TestActionStartupMetadataFailureAbortsCore(t *testing.T) {
	r, output := serviceActionTestRuntime(t)
	serviceTestWrite(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte("break-metadata"), 0600)
	err := r.Action("start", nil)
	if err == nil || !strings.Contains(err.Error(), "update module.prop status to Running") {
		t.Fatalf("lost final startup metadata error: %v", err)
	}
	if strings.Contains(output.String(), "started.") {
		t.Fatalf("failed startup logged success: %s", output.String())
	}
	if _, err := r.RunningPID(); !errors.Is(err, ErrStopped) {
		t.Fatalf("core survived failed startup: %v", err)
	}
	events := serviceTestEvents(t, r)
	if len(events) != 2 || events[1].Test {
		t.Fatalf("startup events: %+v", events)
	}
	if _, err := serviceProcIdentity(events[1].PID); !errors.Is(err, ErrStopped) {
		t.Fatalf("startup abort left process alive: %v", err)
	}
}

func TestActionStopCleanupFailure(t *testing.T) {
	for _, action := range []string{"stop", "restart"} {
		t.Run(action, func(t *testing.T) {
			r, output := serviceActionTestRuntime(t)
			serviceTestWrite(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte("stop-pid-directory"), 0600)
			serviceTestRequire(t, r.Action("start", nil))
			pid, err := r.RunningPID()
			serviceTestRequire(t, err)
			output.Reset()
			err = r.Action(action, nil)
			if !errors.Is(err, syscall.EISDIR) || !strings.Contains(err.Error(), "remove "+r.PIDFile()) {
				t.Fatalf("lost PID cleanup failure: %v", err)
			}
			if output.Len() != 0 {
				t.Fatalf("cleanup failure logged success: %s", output.String())
			}
			if _, err := serviceProcIdentity(pid); !errors.Is(err, ErrStopped) {
				t.Fatalf("core survived stop: %v", err)
			}
			if events := serviceTestEvents(t, r); len(events) != 2 {
				t.Fatalf("cleanup failure started another core: %+v", events)
			}
			serviceTestState(t, r, "Stopped")
			serviceTestRequire(t, os.Remove(r.PIDFile()))
			serviceTestRequire(t, r.Action("stop", nil))
			serviceTestWrite(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte("valid"), 0600)
			serviceTestRequire(t, r.Action("start", nil))
			serviceTestRequire(t, r.Action("stop", nil))
		})
	}
}

func TestActionTogglePIDCleanupFailure(t *testing.T) {
	r, output := serviceActionTestRuntime(t)
	serviceTestWrite(t, r.PIDFile(), []byte("not-a-pid"), 0600)
	identity := r.PIDFile() + ".identity"
	serviceTestRequire(t, os.Mkdir(identity, 0700))
	err := r.Action("toggle", nil)
	if !errors.Is(err, syscall.EISDIR) || errors.Is(err, ErrStopped) {
		t.Fatalf("toggle lost PID cleanup failure: %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("toggle logged success: %s", output.String())
	}
	if _, err := os.Stat(filepath.Join(r.CoreDir(), "invocations.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("toggle started core after PID cleanup failure: %v", err)
	}
	serviceTestRequire(t, os.Remove(identity))
	serviceTestRequire(t, r.Action("toggle", nil))
	serviceTestRequire(t, r.Action("toggle", nil))
}

func TestControllerRunningErrorPreservesFailures(t *testing.T) {
	sentinel := errors.New("PID cleanup failed")
	for _, err := range []error{nil, ErrStopped, fmt.Errorf("status: %w", ErrStopped), errors.Join(ErrStopped)} {
		if got := controllerRunningError(err); got != nil {
			t.Fatalf("stopped check failed: %v", got)
		}
	}
	for _, err := range []error{
		sentinel,
		errors.Join(ErrStopped, sentinel),
		fmt.Errorf("status: %w", errors.Join(ErrStopped, sentinel)),
		errors.Join(fmt.Errorf("status: %w", ErrStopped), sentinel),
	} {
		got := controllerRunningError(err)
		if !errors.Is(got, sentinel) || errors.Is(got, ErrStopped) {
			t.Fatalf("wrong remaining error: %v", got)
		}
	}
}
