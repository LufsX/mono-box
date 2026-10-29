//go:build linux || android

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestRecoveryIntentManualStopAndFailedStart(t *testing.T) {
	r := serviceTestRuntime(t)
	serviceTestRequire(t, r.Service("start"))
	intent, _, err := r.RecoverySnapshot()
	serviceTestRequire(t, err)
	serviceTestRequire(t, r.Service("stop"))
	stopped, _, err := r.RecoverySnapshot()
	serviceTestRequire(t, err)
	if stopped.Running || stopped.Revision <= intent.Revision || stopped.AttemptedAt == 0 {
		t.Fatalf("%+v", stopped)
	}
	if err := r.Recover(context.Background(), intent.Revision); !errors.Is(err, ErrRecoverySuperseded) {
		t.Fatal(err)
	}
	// A second manual stop still supersedes previous lifecycle attempts.
	serviceTestRequire(t, r.Service("stop"))
	again, _, err := r.RecoverySnapshot()
	serviceTestRequire(t, err)
	if again.Revision <= stopped.Revision {
		t.Fatal("stop of dead core did not record attempt")
	}
	serviceTestRequire(t, os.Remove(r.CoreBinary()))
	if err := r.Service("start"); err == nil {
		t.Fatal("missing binary started")
	}
	failed, _, err := r.RecoverySnapshot()
	serviceTestRequire(t, err)
	if !failed.Running || failed.Revision <= again.Revision {
		t.Fatalf("%+v", failed)
	}
}

func TestRecoveryLegacyAdoptionModuleGatesAndNormalStart(t *testing.T) {
	r := serviceTestRuntime(t)
	intent, pid, err := r.RecoverySnapshot()
	serviceTestRequire(t, err)
	if intent.Revision != 0 || pid != 0 {
		t.Fatal("adopted stopped legacy install")
	}
	serviceTestRequire(t, r.Service("start"))
	serviceTestRequire(t, os.Remove(filepath.Join(r.RunDir(), "service-intent.json")))
	intent, pid, err = r.RecoverySnapshot()
	serviceTestRequire(t, err)
	if !intent.Running || intent.Revision == 0 || pid == 0 {
		t.Fatal("running legacy core not adopted")
	}
	serviceTestRequire(t, syscall.Kill(pid, syscall.SIGKILL))
	wait := time.Now().Add(time.Second)
	for time.Now().Before(wait) {
		_, err := r.RunningPID()
		if errors.Is(err, ErrStopped) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	for _, marker := range []string{filepath.Join(r.ModuleDir, "disable"), filepath.Join(r.ModuleDir, "remove"), filepath.Join(r.Root, "manual")} {
		serviceTestWrite(t, marker, nil, 0600)
		if err := r.Recover(context.Background(), intent.Revision); err == nil {
			t.Fatalf("ignored %s", marker)
		}
		serviceTestRequire(t, os.Remove(marker))
	}
	serviceTestRequire(t, r.Recover(context.Background(), intent.Revision))
	serviceTestWrite(t, filepath.Join(r.Root, "manual"), nil, 0600)
	serviceTestRequire(t, r.Service("stop"))
	serviceTestRequire(t, r.Service("start")) // Manual mode permits explicit actions.
}

func TestRecoveryLockWaitCancellationAndPolicyStop(t *testing.T) {
	r := serviceTestRuntime(t)
	serviceTestRequire(t, r.Service("start"))
	intent, pid, err := r.RecoverySnapshot()
	serviceTestRequire(t, err)
	serviceTestRequire(t, syscall.Kill(pid, syscall.SIGKILL))
	lock, err := base.AcquireServiceLock(r.RunDir(), time.Second)
	serviceTestRequire(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Recover(ctx, intent.Revision) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("lock wait did not cancel")
	}
	serviceTestRequire(t, lock.Close())
	serviceTestRequire(t, r.EnsureStopped())
	stopped, _, err := r.RecoverySnapshot()
	serviceTestRequire(t, err)
	if stopped.Running {
		t.Fatal("policy did not stop crashed intent")
	}
	revision := stopped.Revision
	serviceTestRequire(t, r.EnsureStopped())
	stopped, _, err = r.RecoverySnapshot()
	serviceTestRequire(t, err)
	if revision != stopped.Revision {
		t.Fatal("idle policy rewrote intent")
	}
	if err := r.Recover(context.Background(), intent.Revision); !errors.Is(err, ErrRecoverySuperseded) {
		t.Fatal(err)
	}
}
