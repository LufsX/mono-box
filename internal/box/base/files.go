//go:build linux || android

package base

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const ServiceLockTimeout = 40 * time.Second

// ErrServiceLockTimeout reports contention after the requested lock wait.
var ErrServiceLockTimeout = errors.New("timed out waiting for service lock")

// Every component below the trusted root is opened without following symlinks.
// Directory descriptors also anchor subsequent operations against rename races.
func OpenDirectory(path string, create bool) (*os.File, error) {
	return OpenDirectoryMode(path, create, 0700)
}

func OpenDirectoryMode(path string, create bool, mode uint32) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("directory is not absolute: %s", path)
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(path), "/"), "/") {
		if part == "" {
			continue
		}
		next, e := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if errors.Is(e, syscall.ENOENT) && create {
			e = syscall.Mkdirat(fd, part, mode)
			if e == nil || errors.Is(e, syscall.EEXIST) {
				next, e = syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
			}
		}
		syscall.Close(fd)
		if e != nil {
			return nil, fmt.Errorf("open directory %s: %w", path, e)
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}

func OpenRegular(path string, flags int, mode os.FileMode) (*os.File, error) {
	dir, err := OpenDirectory(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return OpenRegularAt(dir, filepath.Base(path), flags, mode)
}

func OpenRegularAt(dir *os.File, name string, flags int, mode os.FileMode) (*os.File, error) {
	if filepath.Base(name) != name || name == "." || name == ".." {
		return nil, errors.New("invalid file name")
	}
	fd, err := syscall.Openat(int(dir.Fd()), name,
		(flags&^os.O_TRUNC)|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, uint32(mode.Perm()))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	f := os.NewFile(uintptr(fd), filepath.Join(dir.Name(), name))
	info, err := f.Stat()
	if err == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 {
			err = fmt.Errorf("unsafe non-regular or hard-linked file: %s", f.Name())
		}
	}
	if err == nil && flags&os.O_TRUNC != 0 {
		err = f.Truncate(0)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func AcquireServiceLock(runDir string, timeout time.Duration) (*os.File, error) {
	dir, err := OpenDirectory(runDir, true)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	if os.Geteuid() == 0 {
		if err := dir.Chown(os.Geteuid(), os.Getegid()); err != nil {
			return nil, err
		}
	}
	if err := dir.Chmod(0700); err != nil {
		return nil, err
	}
	lock, err := OpenRegularAt(dir, "service.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			lock.Close()
			return nil, err
		}
		if timeout == 0 && errors.Is(err, syscall.EINTR) {
			lock.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			lock.Close()
			return nil, ErrServiceLockTimeout
		}
		time.Sleep(20 * time.Millisecond)
	}
}
