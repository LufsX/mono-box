package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestSingleFileReadsDoNotCreateOrWaitForServiceLock(t *testing.T) {
	r, _, _ := storageFixture(t)
	for _, args := range [][]string{{"config", "read"}, {"module", "info"}} {
		storageRun(t, r, args...)
	}
	if _, err := os.Stat(r.RunDir()); !os.IsNotExist(err) {
		t.Fatalf("single-file read created run/: %v", err)
	}
	lock, err := base.AcquireServiceLock(r.RunDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	for _, args := range [][]string{{"config", "read"}, {"module", "info"}} {
		done := make(chan error, 1)
		go func() { done <- r.Storage(args) }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("single-file read waited for lifecycle lock")
		}
	}
}

func TestRequestInvalidOrMissingDoesNotCreateState(t *testing.T) {
	r, _, _ := storageFixture(t)
	token := strings.Repeat("a", 32)
	for _, args := range [][]string{
		{"unknown"}, {"begin", "extra"}, {"append", token, "!"},
		{"run", token, "extra"}, {"append", token, "YQ=="}, {"run", token},
	} {
		if err := r.Request(args, nil); err == nil {
			t.Fatalf("accepted invalid or missing request: %v", args)
		}
	}
	if err := r.Request([]string{"abort", token}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.RunDir()); !os.IsNotExist(err) {
		t.Fatalf("invalid/missing request created runtime state: %v", err)
	}
}

func TestRequestDirectoryLockReleasedBeforeExecute(t *testing.T) {
	r, out, _ := storageFixture(t)
	if err := r.Request([]string{"begin"}, nil); err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(out.String())
	data, err := json.Marshal([]string{"config", "read"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Request([]string{"append", token, base64.StdEncoding.EncodeToString(data)}, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.RunDir(), "requests")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err := r.Request([]string{"run", token}, func(args []string) error {
		called = true
		dir, err := base.OpenDirectory(path, false)
		if err != nil {
			return err
		}
		defer dir.Close()
		// A nested executor must be able to stage another request immediately.
		if err := syscall.Flock(int(dir.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Errorf("execute still owns staging lock: %v", err)
			return err
		}
		syscall.Flock(int(dir.Fd()), syscall.LOCK_UN)
		if _, err := os.Stat(filepath.Join(path, token)); !os.IsNotExist(err) {
			t.Fatalf("execute observed unconsumed payload: %v", err)
		}
		lock, err := base.AcquireServiceLock(r.RunDir(), 0)
		if err != nil {
			return err
		}
		defer lock.Close()
		configLock, err := base.AcquireConfigLock(context.Background(), r.RunDir())
		if err != nil {
			return err
		}
		configLock.Close()
		return r.Storage(args)
	}); err != nil || !called {
		t.Fatalf("request execution: called=%v err=%v", called, err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("request replaced directory inode: %v", err)
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("request retained lock/payload: %v %v", entries, err)
	}
}

func TestRequestDirectoryLockSerializesChunks(t *testing.T) {
	r, out, _ := storageFixture(t)
	if err := r.Request([]string{"begin"}, nil); err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(out.String())
	dir, err := base.OpenDirectory(filepath.Join(r.RunDir(), "requests"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := syscall.Flock(int(dir.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(dir.Fd()), syscall.LOCK_UN)
	done := make(chan error, 1)
	go func() { done <- r.Request([]string{"append", token, "YQ=="}, nil) }()
	select {
	case err := <-done:
		t.Fatalf("chunk escaped directory lock: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	syscall.Flock(int(dir.Fd()), syscall.LOCK_UN)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("chunk did not resume")
	}
	if got := storageRead(t, filepath.Join(r.RunDir(), "requests", token)); got != "a" {
		t.Fatalf("chunk corrupted: %q", got)
	}
}

func TestProfileCancellationAndRejectedCommitCleanStaging(t *testing.T) {
	for _, failure := range []string{"cancelled", "invalid", "guard"} {
		t.Run(failure, func(t *testing.T) {
			r, _, _ := storageFixture(t)
			s, err := r.openProfileStorage()
			if err != nil {
				t.Fatal(err)
			}
			defer s.close()
			name := "kept.yaml"
			storageWriteFixture(t, storageProfilePath(r, name), "mode: rule\n", 0600)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			content := []byte("mode: direct\n")
			var guard base.CommitGuard
			if failure == "cancelled" {
				cancel()
			} else if failure == "invalid" {
				content = []byte("INVALID secret")
			} else {
				guard = func(context.Context, func() error) error { return base.ErrAutomationChanged }
			}
			if err := r.installStoredProfileGuarded(ctx, s, name, content, true, guard); err == nil {
				t.Fatal("failed profile committed")
			}
			if got := storageRead(t, storageProfilePath(r, name)); got != "mode: rule\n" {
				t.Fatalf("failure replaced profile: %q", got)
			}
			storageNoTemps(t, r)
		})
	}
}

type failedRequestOutput struct{}

func (failedRequestOutput) Write([]byte) (int, error) { return 0, errors.New("output failed") }

func TestRequestBeginOutputFailureLeavesNoPayload(t *testing.T) {
	r, _, _ := storageFixture(t)
	r.Out = failedRequestOutput{}
	if err := r.Request([]string{"begin"}, nil); err == nil {
		t.Fatal("missing output error")
	}
	entries, err := os.ReadDir(filepath.Join(r.RunDir(), "requests"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("begin failure leaked staging: %v %v", entries, err)
	}
	// The persistent inode remains usable after the failed begin.
	r.Out = &bytes.Buffer{}
	if err := r.Request([]string{"begin"}, nil); err != nil {
		t.Fatal(err)
	}
}
