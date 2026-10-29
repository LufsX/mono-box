//go:build linux || android

package service

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
	"github.com/LufsX/mono-box/internal/box/storage"
)

func TestServiceReloadConfigAfterLock(t *testing.T) {
	r := serviceTestRuntime(t)
	configFile := filepath.Join(r.Root, "scripts", "box.config")
	serviceTestWrite(t, configFile, []byte(fmt.Sprintf("bin_name=mihomo\nCORE_USER_GROUP=%s\n", r.Config.CoreUserGroup)), 0600)
	nextDir := filepath.Join(r.Root, "nextcore")
	serviceTestWrite(t, filepath.Join(r.Root, "bin", "nextcore"), serviceTestFake(t), 0700)
	serviceTestWrite(t, filepath.Join(nextDir, "config.yaml"), []byte("valid"), 0600)
	loc, err := r.serviceLocation()
	serviceTestRequire(t, err)
	lock, err := base.AcquireServiceLock(loc.run, time.Second)
	serviceTestRequire(t, err)
	defer lock.Close()
	done := make(chan error, 1)
	go func() { done <- r.Service("start") }()
	select {
	case err := <-done:
		t.Fatalf("start bypassed config lock: %v", err)
	case <-time.After(80 * time.Millisecond):
	}
	// A storage patch commits the new name while the already-constructed
	// Runtime is waiting. Service must resolve both the core and PID afresh.
	serviceTestRequire(t, base.AtomicWrite(configFile, []byte(fmt.Sprintf("bin_name=nextcore\nCORE_USER_GROUP=%s\n", r.Config.CoreUserGroup)), 0600))
	serviceTestRequire(t, lock.Close())
	serviceTestRequire(t, <-done)
	pid, err := r.RunningPID()
	serviceTestRequire(t, err)
	args, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	serviceTestRequire(t, err)
	if !bytes.Contains(args, []byte(nextDir)) {
		t.Fatalf("stale config used: %q", args)
	}
	if _, err := os.Stat(filepath.Join(r.CoreDir(), "invocations.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("old core ran: %v", err)
	}
	if r.Config.BinName != "mihomo" {
		t.Fatal("service mutated the caller's config")
	}
	active, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/1", pid))
	serviceTestRequire(t, err)
	if !strings.HasPrefix(filepath.Base(active), "nextcore_") {
		t.Fatalf("wrong log name: %s", active)
	}
	var out bytes.Buffer
	r.Out = &out
	serviceTestRequire(t, r.LogsSize())
	if !strings.Contains(out.String(), active) {
		t.Fatalf("logs used stale core: %s", out.String())
	}
	out.Reset()
	serviceTestRequire(t, r.ReadLog(active))
	if !strings.Contains(out.String(), "fake stdout") {
		t.Fatalf("read log used stale config: %s", out.String())
	}
	serviceTestRequire(t, r.Service("stop"))
}

func TestServiceLargeStoredConfigLifecycle(t *testing.T) {
	r := serviceTestRuntime(t)
	nextDir := filepath.Join(r.Root, "nextcore")
	nextBinary := filepath.Join(r.Root, "bin", "nextcore")
	serviceTestWrite(t, nextBinary, serviceTestFake(t), 0700)
	serviceTestWrite(t, filepath.Join(nextDir, "config.yaml"), []byte("valid"), 0600)
	content := fmt.Sprintf("bin_name=nextcore\nCORE_USER_GROUP=%s\n", r.Config.CoreUserGroup) +
		strings.Repeat("# padding\n", 7000)
	serviceTestRequire(t, storage.New(r.Runtime).Storage([]string{
		"config", "write", base64.StdEncoding.EncodeToString([]byte(content)),
	}))
	serviceTestRequire(t, r.Service("start"))
	pid, err := r.RunningPID()
	serviceTestRequire(t, err)
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	serviceTestRequire(t, err)
	args, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	serviceTestRequire(t, err)
	if exe != nextBinary || !bytes.Contains(args, []byte(nextDir+"\x00")) {
		t.Fatalf("wrong core started: exe=%s args=%q", exe, args)
	}
	if _, err := os.Stat(filepath.Join(r.CoreDir(), "invocations.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("old core ran: %v", err)
	}
	if r.Config.BinName != "mihomo" {
		t.Fatal("service changed the caller's config")
	}
	serviceTestRequire(t, r.Service("status"))
	serviceTestRequire(t, r.Service("stop"))
	if err := r.Service("status"); !errors.Is(err, ErrStopped) {
		t.Fatalf("status after stop: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.Root, "run", "nextcore.pid")); !os.IsNotExist(err) {
		t.Fatalf("stopped core PID file remains: %v", err)
	}
	if _, err := serviceProcIdentity(pid); !errors.Is(err, ErrStopped) {
		t.Fatalf("core process remains after stop: %v", err)
	}
}

func TestServiceBadConfigReleasesLock(t *testing.T) {
	r := serviceTestRuntime(t)
	configFile := filepath.Join(r.Root, "scripts", "box.config")
	serviceTestWrite(t, configFile, []byte("not an assignment"), 0600)
	if err := r.Service("start"); err == nil {
		t.Fatal("invalid runtime config accepted")
	}
	serviceTestRequire(t, os.Remove(configFile))
	loc, err := r.serviceLocation()
	serviceTestRequire(t, err)
	lock, err := base.AcquireServiceLock(loc.run, 100*time.Millisecond)
	serviceTestRequire(t, err)
	lock.Close()
	serviceTestRequire(t, r.Service("start"))
	serviceTestRequire(t, r.Service("stop"))
}
