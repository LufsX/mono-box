//go:build linux || android

package service

import (
	"errors"

	"github.com/LufsX/mono-box/internal/box/base"
)

// EnsureStopped records policy even after a crash. It rechecks the process
// under service.lock so an intervening recovery is stopped as well.
func (r *Runtime) EnsureStopped() error {
	current, loc, lock, err := r.serviceLockedRuntime(base.ServiceLockTimeout)
	if err != nil {
		return err
	}
	defer lock.Close()
	intent, err := current.ReadServiceIntent()
	if err != nil {
		return err
	}
	bootID, err := base.CurrentBootID()
	if err != nil {
		return err
	}
	if intent.Revision == 0 || intent.Running || intent.BootID != bootID {
		if _, err := current.WriteServiceIntent(false); err != nil {
			return err
		}
	}
	_, err = serviceRunning(loc)
	if errors.Is(err, ErrStopped) {
		return nil
	}
	if err != nil {
		return err
	}
	return current.serviceStop(loc)
}
