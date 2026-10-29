//go:build linux || android

package box

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Reload only after flock: a Runtime can have been constructed before a config
// patch acquired the same lock. Keep the caller's Config immutable so concurrent
// calls on one Runtime cannot race. The lock's path never depends on bin_name.
func (r *Runtime) serviceLockedRuntime(timeout time.Duration) (*Runtime, serviceLocation, *os.File, error) {
	if r.Root == "" {
		return nil, serviceLocation{}, nil, errors.New("empty runtime root")
	}
	root, err := filepath.Abs(r.Root)
	if err == nil {
		root, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		return nil, serviceLocation{}, nil, err
	}
	base := serviceLocation{root: root, run: filepath.Join(root, "run")}
	lock, err := serviceAcquireLock(base, timeout)
	if err != nil {
		return nil, serviceLocation{}, nil, err
	}
	current := *r
	data, err := serviceReadSmall(filepath.Join(root, "scripts", "box.config"))
	if err == nil {
		current.Config, err = ParseConfig(string(data))
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err != nil {
		lock.Close()
		return nil, serviceLocation{}, nil, err
	}
	loc, err := current.serviceLocation()
	if err != nil {
		lock.Close()
		return nil, serviceLocation{}, nil, err
	}
	return &current, loc, lock, nil
}
