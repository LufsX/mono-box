//go:build linux || android

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServiceConfigCheckDiagnostics(t *testing.T) {
	for _, mode := range []string{"invalid", "check-hang", "valid"} {
		t.Run(mode, func(t *testing.T) {
			r := serviceTestRuntime(t)
			config := filepath.Join(r.CoreDir(), "config.yaml")
			serviceTestWrite(t, config, []byte(mode), 0600)
			log := filepath.Join(r.RunDir(), "check.log")
			serviceTestWrite(t, log, []byte("stale check output"), 0600)
			loc, err := r.serviceLocation()
			serviceTestRequire(t, err)
			timeout := time.Second
			if mode == "check-hang" {
				timeout = 100 * time.Millisecond
			}
			err = serviceValidateConfig(loc, config, nil, timeout)
			switch mode {
			case "invalid":
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
					t.Fatalf("lost validator exit status: %v", err)
				}
				want := fmt.Sprintf("configuration check failed; see %s: exit status 42\nCore log (%s):\nfake configuration rejected", log, log)
				if err.Error() != want {
					t.Fatalf("configuration failure changed:\ngot: %s\nwant: %s", err, want)
				}
			case "check-hang":
				want := fmt.Sprintf("configuration check timed out; see %s: context deadline exceeded", log)
				if !errors.Is(err, context.DeadlineExceeded) || err.Error() != want {
					t.Fatalf("configuration timeout changed: %v", err)
				}
			case "valid":
				serviceTestRequire(t, err)
			}
			data, readErr := os.ReadFile(log)
			serviceTestRequire(t, readErr)
			if strings.Contains(string(data), "stale check output") {
				t.Fatal("configuration check retained old diagnostics")
			}
		})
	}
}

func TestServiceStartupDiagnosticOrderAndCleanup(t *testing.T) {
	for _, mode := range []string{"delayed-exit", "break-metadata"} {
		t.Run(mode, func(t *testing.T) {
			r := serviceTestRuntime(t)
			serviceTestWrite(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte(mode), 0600)
			err := r.Service("start")
			if err == nil {
				t.Fatal("start succeeded")
			}
			// Inspect files before RunningPID, which can clean stale PID records.
			for _, path := range []string{r.PIDFile(), r.PIDFile() + ".identity"} {
				if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("startup left %s: %v", path, statErr)
				}
			}
			events := serviceTestEvents(t, r)
			if len(events) != 2 || !events[0].Test || events[1].Test {
				t.Fatalf("startup invocations: %+v", events)
			}
			if _, procErr := serviceProcIdentity(events[1].PID); !errors.Is(procErr, ErrStopped) {
				t.Fatalf("core survived failed startup: %v", procErr)
			}
			logs, globErr := filepath.Glob(filepath.Join(r.CoreDir(), "mihomo_*.log"))
			serviceTestRequire(t, globErr)
			if len(logs) != 1 {
				t.Fatalf("startup logs: %v", logs)
			}
			stdout, readErr := os.ReadFile(logs[0])
			serviceTestRequire(t, readErr)
			if !strings.HasPrefix(string(stdout), "fake stdout\n") {
				t.Fatalf("missing core stdout: %q", stdout)
			}
			wantTail := fmt.Sprintf("\nCore log (%s):\nfake stderr\nCore log (%s):\n%s",
				filepath.Join(r.RunDir(), "error_mihomo.log"), logs[0], strings.TrimSpace(string(stdout)))
			if mode == "break-metadata" {
				detail := "unsafe non-regular or hard-linked file: " + filepath.Join(r.ModuleDir, "module.prop")
				want := "update module.prop status to Running: " + detail + wantTail +
					"\nupdate module.prop status to Error: " + detail
				if err.Error() != want {
					t.Fatalf("startup abort diagnostics changed:\ngot: %s\nwant: %s", err, want)
				}
			} else {
				if !strings.HasPrefix(err.Error(), "core ") || !strings.Contains(err.Error(), "startup") ||
					!strings.HasSuffix(err.Error(), wantTail) || strings.Count(err.Error(), "\nCore log (") != 2 {
					t.Fatalf("early exit diagnostics changed: %v", err)
				}
				serviceTestState(t, r, "Error")
			}
		})
	}
}

func TestStartupErrorBoundedLogTails(t *testing.T) {
	dir := t.TempDir()
	stderr, stdout := filepath.Join(dir, "error.log"), filepath.Join(dir, "core.log")
	serviceTestWrite(t, stderr, []byte("old header\n"+strings.Repeat("x", serviceStartupLogLimit*3)+"\nfatal startup detail\n"), 0600)
	serviceTestWrite(t, stdout, []byte("core boot output\n"), 0600)
	cause := errors.New("startup failed")
	result := serviceStartupError(cause, stderr, stdout).Error()
	if !strings.Contains(result, "[truncated]") || !strings.Contains(result, "fatal startup detail") || !strings.Contains(result, "core boot output") || strings.Contains(result, "old header") {
		t.Fatalf("wrong log tails: %s", result)
	}
	if len(result) > 2*serviceStartupLogLimit+len(dir)*2+200 {
		t.Fatalf("unbounded startup error: %d", len(result))
	}
	if !errors.Is(serviceStartupError(cause, stderr), cause) {
		t.Fatal("original cause lost")
	}
}

func TestStartupErrorSkipsUnsafeOrEmptyLogs(t *testing.T) {
	dir := t.TempDir()
	missing, empty, linked, pipe := filepath.Join(dir, "missing"), filepath.Join(dir, "empty"), filepath.Join(dir, "linked"), filepath.Join(dir, "pipe")
	serviceTestWrite(t, empty, nil, 0600)
	serviceTestWrite(t, filepath.Join(dir, "secret"), []byte("unrelated contents"), 0600)
	serviceTestRequire(t, os.Symlink(filepath.Join(dir, "secret"), linked))
	// A directory is not a valid log and must not hide the original startup error.
	serviceTestRequire(t, os.Mkdir(pipe, 0700))
	cause := errors.New("original failure")
	if got := serviceStartupError(cause, missing, empty, linked, pipe); got != cause {
		t.Fatalf("changed original error: %v", got)
	}
}
