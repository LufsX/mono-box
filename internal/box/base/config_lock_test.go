//go:build linux || android

package base

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestConfigLockTimeoutAndClose(t *testing.T) {
	runDir := filepath.Join(t.TempDir(), "run")
	first, err := AcquireConfigLock(context.Background(), runDir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	info, err := first.Stat()
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("lock permissions: %v, %v", info, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	start := time.Now()
	lock, err := AcquireConfigLock(ctx, runDir)
	if lock != nil {
		lock.Close()
		t.Fatal("acquired a held lock")
	}
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("lock did not honor deadline: %v after %v", err, time.Since(start))
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lock, err = AcquireConfigLock(ctx, runDir)
	if err != nil {
		t.Fatalf("close did not release lock: %v", err)
	}
	lock.Close()
	if _, err := os.Stat(filepath.Join(runDir, "service.lock")); !os.IsNotExist(err) {
		t.Fatalf("config lock used a service lock: %v", err)
	}
}

func TestConfigLockWaitsForRelease(t *testing.T) {
	runDir := t.TempDir()
	first, err := AcquireConfigLock(context.Background(), runDir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		lock, err := AcquireConfigLock(ctx, runDir)
		if lock != nil {
			lock.Close()
		}
		result <- err
	}()
	// Confirm contention while the first descriptor owns the lock.
	select {
	case err := <-result:
		t.Fatalf("waiter returned before release: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	first.Close()
	if err := <-result; err != nil {
		t.Fatalf("waiter failed after release: %v", err)
	}
}

func TestConfigLockCanceledBeforeOpen(t *testing.T) {
	runDir := filepath.Join(t.TempDir(), "run")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lock, err := AcquireConfigLock(ctx, runDir)
	if lock != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lock: %v, %v", lock, err)
	}
	if _, err := os.Stat(runDir); !os.IsNotExist(err) {
		t.Fatalf("canceled call created run directory: %v", err)
	}
}

func TestConfigLockRejectsUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo", "directory", "symlink-parent"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			runDir := filepath.Join(root, "run")
			if err := os.Mkdir(runDir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(runDir, "config.lock")
			target := filepath.Join(root, "target")
			if err := os.WriteFile(target, []byte("untouched"), 0644); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(target, path)
			case "hardlink":
				err = os.Link(target, path)
			case "fifo":
				err = syscall.Mkfifo(path, 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "symlink-parent":
				link := filepath.Join(root, "linked")
				err = os.Symlink(runDir, link)
				runDir = link
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			start := time.Now()
			lock, err := AcquireConfigLock(ctx, runDir)
			if lock != nil {
				lock.Close()
				t.Fatal("accepted unsafe lock path")
			}
			if err == nil || errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
				t.Fatalf("unsafe path was not promptly rejected: %v", err)
			}
			data, err := os.ReadFile(target)
			info, statErr := os.Stat(target)
			if err != nil || statErr != nil || string(data) != "untouched" || info.Mode().Perm() != 0644 {
				t.Fatalf("unsafe target modified: data=%q, read=%v, stat=%v", data, err, statErr)
			}
		})
	}
}

func TestConfigLockRestrictsExistingPermissions(t *testing.T) {
	runDir := t.TempDir()
	path := filepath.Join(runDir, "config.lock")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireConfigLock(context.Background(), runDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	info, err := lock.Stat()
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("existing lock permissions: %v, %v", info, err)
	}
}
