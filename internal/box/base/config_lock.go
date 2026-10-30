//go:build linux || android

package base

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

// AcquireConfigLock serializes box.config reads and writes that must be atomic.
// Close the returned file to release the lock. Callers needing the service lock
// must acquire it first; the config lock never acquires a service lock.
func AcquireConfigLock(ctx context.Context, runDir string) (*os.File, error) {
	return acquireConfigLock(ctx, runDir, true)
}

// AcquireExistingConfigLock never creates a directory or lock file. An absent
// lock means no cooperating writer has started; readers must check again after
// their snapshot and reread under the lock if a writer created it meanwhile.
func AcquireExistingConfigLock(ctx context.Context, runDir string) (*os.File, error) {
	return acquireConfigLock(ctx, runDir, false)
}

func acquireConfigLock(ctx context.Context, runDir string, create bool) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := OpenDirectory(runDir, create)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	flags := os.O_RDONLY
	if create {
		flags = os.O_CREATE | os.O_RDWR
	}
	lock, err := OpenRegularAt(dir, "config.lock", flags, 0600)
	if err != nil {
		return nil, err
	}
	if create {
		if err := lock.Chmod(0600); err != nil {
			lock.Close()
			return nil, err
		}
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			lock.Close()
			return nil, err
		}
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			if err := ctx.Err(); err != nil {
				lock.Close()
				return nil, err
			}
			return lock, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			lock.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			lock.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
