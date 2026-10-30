package control

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestControllerDirectoryLockProcess(t *testing.T) {
	root := os.Getenv("MONOBOX_CONTROL_LOCK_ROOT")
	if root == "" {
		return
	}
	r := New(&base.Runtime{Root: root})
	lock, err := r.controllerLock(true)
	if os.Getenv("MONOBOX_CONTROL_LOCK_HELD") == "true" {
		if lock != nil {
			unlockController(lock)
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			t.Fatalf("cross-process controller lock escaped: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	unlockController(lock)
}

func TestControllerDirectoryLockIndependentAndPersistent(t *testing.T) {
	r := controllerTestRuntime(t, "")
	lock, err := r.controllerLock(true)
	if err != nil {
		t.Fatal(err)
	}
	defer unlockController(lock)
	before, err := lock.Stat()
	if err != nil {
		t.Fatal(err)
	}
	// A separate open description in this process must contend too.
	if other, err := r.controllerLock(true); !errors.Is(err, syscall.EWOULDBLOCK) {
		if other != nil {
			unlockController(other)
		}
		t.Fatalf("separate open escaped lock: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	check := func(held string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestControllerDirectoryLockProcess$")
		cmd.Env = append(os.Environ(), "MONOBOX_CONTROL_LOCK_ROOT="+r.Root, "MONOBOX_CONTROL_LOCK_HELD="+held)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("controller subprocess: %v\n%s", err, out)
		}
	}
	check("true")
	for _, name := range []string{"service.lock", "config.lock", "download.lock", "watcher.lock"} {
		f, err := r.namedControllerLock(name, true)
		if err != nil {
			t.Fatalf("controller blocked independent %s: %v", name, err)
		}
		unlockController(f)
	}
	unlockController(lock)
	check("false")
	after, err := os.Stat(r.RunDir())
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("controller replaced run inode: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.RunDir(), "controller.lock")); !os.IsNotExist(err) {
		t.Fatalf("redundant controller.lock: %v", err)
	}
}

func TestPolicyNetworkLeavesServiceLockAvailable(t *testing.T) {
	r := controllerTestRuntime(t, "")
	deps := controllerTestDeps(t)
	entered, release := make(chan struct{}), make(chan struct{})
	server := controllerTestServer(t, r, func(w http.ResponseWriter, req *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	// Ensure fatal paths release the handler before httptest cleanup waits.
	releasePolicy := sync.OnceFunc(func() { close(release) })
	defer releasePolicy()
	controllerTestWrite(t, filepath.Join(r.Root, "scripts", "box.config"),
		"ctr_mode=tun\ntarget_wifi=true\nclash_api_port="+controllerTestPort(server))
	done := make(chan error, 1)
	go func() { done <- r.controller(context.Background(), []string{"w", deps.netPath}, deps) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("policy did not reach API")
	}
	lock, err := base.AcquireServiceLock(r.RunDir(), 0)
	if err != nil {
		t.Fatalf("network request held service.lock: %v", err)
	}
	lock.Close()
	releasePolicy()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("policy failed to complete")
	}
}

func TestPolicyRecordIsOnlyArtifactAndCellularFailureRetries(t *testing.T) {
	r := controllerTestRuntime(t, "")
	deps := controllerTestDeps(t)
	current, fail := "direct", false
	server := controllerTestServer(t, r, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			_, _ = w.Write([]byte("{\"mode\":\"" + current + "\"}"))
			return
		}
		if fail {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if current == "direct" {
			current = "global"
		} else {
			current = "direct"
		}
		w.WriteHeader(http.StatusNoContent)
	})
	controllerTestWrite(t, filepath.Join(r.Root, "scripts", "box.config"),
		"ctr_mode=mode\ntarget_wifi=global\ntarget_cellular=last\nclash_api_port="+controllerTestPort(server))
	event := []string{"w", deps.netPath}
	if err := r.controller(context.Background(), event, deps); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(r.RunDir())
	if err != nil || len(entries) != 1 || entries[0].Name() != ".policy-mode.json" {
		t.Fatalf("policy runtime artifacts: %v %v", entries, err)
	}
	path := filepath.Join(r.RunDir(), ".policy-mode.json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("policy permissions: %v %v", info, err)
	}
	controllerTestWrite(t, deps.netPath, "100 rmnet0")
	fail = true
	if err := r.controller(context.Background(), event, deps); err == nil {
		t.Fatal("failed restore reported success")
	}
	var saved savedPolicy
	if err := base.ReadPrivateJSON(path, &saved, 16<<10); err != nil || !saved.WiFi || saved.Baseline != "direct" {
		t.Fatalf("failure discarded pending baseline: %+v %v", saved, err)
	}
	fail = false
	if err := r.controller(context.Background(), event, deps); err != nil || current != "direct" {
		t.Fatalf("cellular retry: %s %v", current, err)
	}
	// Repeated cellular observations retain the committed baseline.
	if err := base.ReadPrivateJSON(path, &saved, 16<<10); err != nil || saved.WiFi || saved.Baseline != "direct" {
		t.Fatalf("cellular completion: %+v %v", saved, err)
	}
}
