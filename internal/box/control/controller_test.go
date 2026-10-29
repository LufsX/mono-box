package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestControllerCombinedFlagsHotReloadAndInvalid(t *testing.T) {
	r := controllerTestRuntime(t, "")
	deps := controllerTestDeps(t)
	var actions []string
	deps.service = func(r *Runtime, action string) error {
		actions = append(actions, action+":"+r.Config.BinName)
		return nil
	}
	disable := filepath.Join(r.ModuleDir, "disable")
	controllerTestWrite(t, disable, "")
	if err := r.controller(context.Background(), []string{"nc", r.ModuleDir, "disable"}, deps); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(disable); err != nil {
		t.Fatal(err)
	}
	controllerTestWrite(t, filepath.Join(r.Root, "scripts", "box.config"), "bin_name=mihomo_smart")
	if err := r.controller(context.Background(), []string{"dn", r.ModuleDir, "disable"}, deps); err != nil {
		t.Fatal(err)
	}
	if strings.Join(actions, ",") != "stop:mihomo,start:mihomo_smart" {
		t.Fatal(actions)
	}
	controllerTestWrite(t, filepath.Join(r.Root, "manual"), "")
	if err := r.controller(context.Background(), []string{"n", r.ModuleDir, "disable"}, deps); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 {
		t.Fatal(actions)
	}
	for _, args := range [][]string{{"boot", "extra"}, {"w"}, {"bad", r.ModuleDir}, {"w", r.ModuleDir, "a", "b"}} {
		if err := r.controller(context.Background(), args, deps); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	sentinel := errors.New("service error")
	deps.service = func(*Runtime, string) error { return sentinel }
	if err := os.Remove(filepath.Join(r.Root, "manual")); err != nil {
		t.Fatal(err)
	}
	if err := r.controller(context.Background(), []string{"n", r.ModuleDir, "disable"}, deps); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
}

func TestAndroidBootReadyUsesOnlySuppliedPaths(t *testing.T) {
	root := t.TempDir()
	storage := filepath.Join(root, "Android")
	packages := filepath.Join(root, "packages.xml")
	boot := "0"
	output := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "getprop" || len(args) != 1 || args[0] != "sys.boot_completed" {
			t.Fatalf("%s %v", name, args)
		}
		return []byte(boot), nil
	}
	ready := func() bool {
		ok, err := androidBootReady(context.Background(), storage, packages, output)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if ready() {
		t.Fatal("boot not complete")
	}
	boot = "1"
	if ready() {
		t.Fatal("packages missing")
	}
	controllerTestWrite(t, packages, "packages")
	if ready() {
		t.Fatal("storage locked")
	}
	if err := os.Mkdir(storage, 0700); err != nil {
		t.Fatal(err)
	}
	if !ready() {
		t.Fatal("not ready")
	}
	entries, err := os.ReadDir(storage)
	if err != nil || len(entries) != 0 {
		t.Fatalf("probe leaked %v %v", entries, err)
	}
}

func controllerTestWait(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}

func TestWatcherBootWaitDoesNotBlockDisableAndDuplicate(t *testing.T) {
	r := controllerTestRuntime(t, "")
	deps := controllerTestDeps(t)
	deps.interval = 10 * time.Millisecond
	deps.ready = func(ctx context.Context) (bool, error) { <-ctx.Done(); return false, ctx.Err() }
	actions := make(chan string, 10)
	deps.service = func(_ *Runtime, action string) error { actions <- action; return nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.watch(ctx, deps) }()
	controllerTestWait(t, func() bool { return base.Exists(filepath.Join(r.RunDir(), "watcher.pid")) })
	duplicateCtx, duplicateCancel := context.WithCancel(context.Background())
	duplicateCancel()
	if err := r.watch(duplicateCtx, deps); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatal(err)
	}
	controllerTestWrite(t, filepath.Join(r.ModuleDir, "disable"), "")
	select {
	case action := <-actions:
		if action != "stop" {
			t.Fatal(action)
		}
	case <-time.After(time.Second):
		t.Fatal("disable blocked by boot")
	}
	if err := os.Remove(filepath.Join(r.ModuleDir, "disable")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	select {
	case action := <-actions:
		t.Fatalf("started before unlock: %s", action)
	default:
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("watcher did not cancel")
	}
	if base.Exists(filepath.Join(r.RunDir(), "watcher.pid")) {
		t.Fatal("PID not removed")
	}
}

func TestWatcherNetworkReplacementAndRetry(t *testing.T) {
	r := controllerTestRuntime(t, "ctr_mode=switch\ntarget_wifi=start\ntarget_cellular=stop")
	deps := controllerTestDeps(t)
	deps.interval = 10 * time.Millisecond
	actions := make(chan string, 20)
	deps.running = func(*Runtime) (int, error) { return 0, nil }
	attempts := 0
	deps.service = func(_ *Runtime, action string) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary environment failure")
		}
		actions <- action
		return nil
	}
	deps.bootStart = func(r *Runtime, _ uint64) (bool, error) {
		err := deps.service(r, "start")
		return err == nil, err // Simulate failure before the intent can be claimed.
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.watch(ctx, deps) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	// The first boot attempt fails. The first successful policy start and
	// subsequent boot retry must both finish before testing a later rewrite.
	for i := 0; i < 2; i++ {
		select {
		case <-actions:
		case <-time.After(time.Second):
			t.Fatal("boot/network retry missing")
		}
	}
	// Rewrite via rename, which detaches a watcher attached to the old inode.
	controllerTestWrite(t, deps.netPath, "101 wlan1")
	select {
	case <-actions:
	case <-time.After(time.Second):
		t.Fatal("replacement not observed")
	}
}

func TestControllerWatchProcess(t *testing.T) {
	root := os.Getenv("MONOBOX_WATCH_TEST_ROOT")
	if root == "" {
		return
	}
	r := &Runtime{Runtime: &base.Runtime{Root: root, ModuleDir: filepath.Join(root, "module"), Config: base.DefaultConfig(), Err: io.Discard}}
	deps := defaultControllerDeps()
	deps.interval = 10 * time.Millisecond
	deps.netPath = filepath.Join(root, "rt_tables")
	deps.ready = func(ctx context.Context) (bool, error) { <-ctx.Done(); return false, ctx.Err() }
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if err := r.watch(ctx, deps); err != nil {
		t.Fatal(err)
	}
}

func TestStopWatchSignalIdentityAndCleanup(t *testing.T) {
	r := controllerTestRuntime(t, "")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestControllerWatchProcess$")
	command.Env = append(os.Environ(), "MONOBOX_WATCH_TEST_ROOT="+r.Root)
	var output strings.Builder
	command.Stdout = &output
	command.Stderr = &output
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	path := filepath.Join(r.RunDir(), "watcher.pid")
	controllerTestWait(t, func() bool { return base.Exists(path) })
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var identity watcherIdentity
	if err = json.Unmarshal(data, &identity); err != nil {
		t.Fatal(err)
	}
	identity.Started = "incorrect start time"
	wrong, _ := json.Marshal(identity)
	if err = base.AtomicWrite(path, wrong, 0600); err != nil {
		t.Fatal(err)
	}
	if err = r.StopWatch(); err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatal(err)
	}
	if err = base.AtomicWrite(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = r.StopWatch(); err != nil {
		t.Fatalf("%v: %s", err, output.String())
	}
	if base.Exists(path) {
		t.Fatal("PID file leaked")
	}
	if err = r.StopWatch(); err != nil {
		t.Fatal("idempotent stop", err)
	}
	controllerTestWrite(t, path, "broken stale pid")
	if err = r.StopWatch(); err != nil || base.Exists(path) {
		t.Fatalf("stale cleanup: %v", err)
	}
}

func TestNetworkSnapshotDetectsSameLengthAndReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rt_tables")
	controllerTestWrite(t, path, "wlan0")
	a, err := networkSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	controllerTestWrite(t, path, "wlan1")
	b, err := networkSnapshot(path)
	if err != nil || a == b {
		t.Fatalf("unchanged snapshot %v", err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	c, err := networkSnapshot(path)
	if err != nil || c != "missing" {
		t.Fatalf("%s %v", c, err)
	}
}
