//go:build linux || android

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/LufsX/mono-box/internal/box/base"
)

var errBootDisabled = errors.New("automatic boot start disabled")

// bootModuleEnabled distinguishes an explicit prohibition from unavailable state.
// It also serves as the normal startup guard after validation and before exec.
func (r *Runtime) bootModuleEnabled() error {
	if _, err := os.Lstat(filepath.Join(r.Root, "manual")); err == nil {
		return errBootDisabled
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if r.ModuleDir == "" {
		return nil
	}
	dir, err := base.OpenDirectory(r.ModuleDir, false)
	if err != nil {
		return err
	}
	defer dir.Close()
	for _, name := range []string{"disable", "remove"} {
		if _, err := os.Lstat(filepath.Join("/proc/self/fd", strconv.Itoa(int(dir.Fd())), name)); err == nil {
			return errBootDisabled
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// BootPlan captures a revision once; retries must keep that revision.
func (r *Runtime) BootPlan() (uint64, error) {
	current, _, lock, err := r.serviceLockedRuntime(base.ServiceLockTimeout)
	if err != nil {
		return 0, err
	}
	defer lock.Close()
	intent, err := current.ReadServiceIntent()
	return intent.Revision, err
}

// BootStart claims this boot's one start opportunity under service.lock.
// Once the running intent is durable, even a failed start completes the plan;
// further attempts belong to the optional, bounded recovery worker.
func (r *Runtime) BootStart(expectedRevision uint64) (done bool, err error) {
	current, loc, lock, err := r.serviceLockedRuntime(base.ServiceLockTimeout)
	if err != nil {
		return false, err
	}
	defer lock.Close()
	intent, err := current.ReadServiceIntent()
	if err != nil {
		return false, err
	}
	bootID, err := base.CurrentBootID()
	if err != nil {
		return false, err
	}
	if intent.Revision != expectedRevision || intent.BootID == bootID {
		return true, nil
	}
	if err := current.bootModuleEnabled(); err != nil {
		if errors.Is(err, errBootDisabled) {
			return true, nil
		}
		return false, err
	}
	if _, err := current.WriteServiceIntent(true); err != nil {
		return false, err
	}
	return true, current.serviceStartContext(context.Background(), loc, current.bootModuleEnabled)
}
