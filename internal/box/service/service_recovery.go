//go:build linux || android

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

var ErrRecoverySuperseded = errors.New("recovery intent changed")

// RecoverySnapshot never waits behind a lifecycle transition. A legacy install
// is adopted only after verifying a running core, under the same lock.
func (r *Runtime) RecoverySnapshot() (base.ServiceIntent, int, error) {
	current, loc, lock, err := r.serviceLockedRuntime(0)
	if err != nil {
		return base.ServiceIntent{}, 0, err
	}
	defer lock.Close()
	intent, err := current.ReadServiceIntent()
	if err != nil {
		return intent, 0, err
	}
	id, err := serviceRunning(loc)
	if errors.Is(err, ErrStopped) {
		return intent, 0, nil
	}
	if err != nil {
		return intent, 0, err
	}
	if intent.Revision == 0 {
		intent, err = current.WriteServiceIntent(true)
	}
	return intent, id.PID, err
}

// Recover checks intent and module state while holding service.lock all the way
// through the normal start permission preparation and configuration validation.
func (r *Runtime) Recover(ctx context.Context, revision uint64) error {
	return r.RecoverGuarded(ctx, revision, nil)
}

// RecoverGuarded runs commit around cmd.Start after validation, under service.lock.
func (r *Runtime) RecoverGuarded(ctx context.Context, revision uint64, commit base.CommitGuard) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, loc, lock, err := r.serviceLockedRuntime(0)
		if errors.Is(err, base.ErrServiceLockTimeout) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(20 * time.Millisecond):
				continue
			}
		}
		if err != nil {
			return err
		}
		defer lock.Close()
		intent, err := current.ReadServiceIntent()
		if err != nil {
			return err
		}
		if !intent.Running || revision == 0 || intent.Revision != revision {
			return ErrRecoverySuperseded
		}
		if err := current.recoveryModuleEnabled(); err != nil {
			return err
		}
		return current.serviceStartGuarded(ctx, loc, current.recoveryModuleEnabled, commit)
	}
}

func (r *Runtime) recoveryModuleEnabled() error {
	if _, err := os.Lstat(filepath.Join(r.Root, "manual")); !errors.Is(err, os.ErrNotExist) {
		return errors.New("automatic start disabled by manual mode")
	}
	if r.ModuleDir == "" {
		return nil
	}
	dir, err := base.OpenDirectory(r.ModuleDir, false)
	if err != nil {
		return errors.New("module unavailable")
	}
	defer dir.Close()
	for _, name := range []string{"disable", "remove"} {
		// Lstat also treats unsafe marker symlinks as disabled.
		if _, err := os.Lstat(filepath.Join("/proc/self/fd", strconv.Itoa(int(dir.Fd())), name)); err == nil {
			return errors.New("module disabled or pending removal")
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.New("module state unavailable")
		}
	}
	return nil
}
