//go:build linux || android

package base

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExistingConfigLockNeverCreatesAndSharesWriterLock(t *testing.T) {
	run := filepath.Join(t.TempDir(), "run")
	if lock, err := AcquireExistingConfigLock(context.Background(), run); !errors.Is(err, os.ErrNotExist) || lock != nil {
		t.Fatalf("missing lock: %v %v", lock, err)
	}
	if _, err := os.Stat(run); !os.IsNotExist(err) {
		t.Fatalf("reader created run/: %v", err)
	}
	if err := os.Mkdir(run, 0700); err != nil {
		t.Fatal(err)
	}
	if lock, err := AcquireExistingConfigLock(context.Background(), run); !errors.Is(err, os.ErrNotExist) || lock != nil {
		t.Fatalf("empty run lock: %v %v", lock, err)
	}
	entries, err := os.ReadDir(run)
	if err != nil || len(entries) != 0 {
		t.Fatalf("reader created lock: %v %v", entries, err)
	}
	writer, err := AcquireConfigLock(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	before, err := writer.Stat()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if reader, err := AcquireExistingConfigLock(ctx, run); !errors.Is(err, context.DeadlineExceeded) || reader != nil {
		t.Fatalf("reader escaped writer: %v %v", reader, err)
	}
	writer.Close()
	reader, err := AcquireExistingConfigLock(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	after, err := reader.Stat()
	if err != nil || !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
		t.Fatalf("reader changed lock inode/content: %v", err)
	}
}
