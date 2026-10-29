//go:build linux || android

package diagnostic

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
	"github.com/LufsX/mono-box/internal/box/service"
	"github.com/LufsX/mono-box/internal/box/testutil"
)

func TestInspectPIDRejectsForeignAndStaleRecordsWithoutCleanup(t *testing.T) {
	r := &base.Runtime{Root: t.TempDir(), Config: base.DefaultConfig(), Out: io.Discard}
	pidPath := r.PIDFile()
	testutil.Require(t, os.MkdirAll(r.RunDir(), 0700))
	for _, pid := range []string{"1\n", strconv.Itoa(os.Getpid()) + "\n"} {
		testutil.Write(t, pidPath, []byte(pid), 0600)
		got, err := service.New(r).InspectPID()
		if got != 0 || !errors.Is(err, service.ErrStopped) {
			t.Fatalf("foreign/stale PID accepted: pid=%d err=%v", got, err)
		}
		data, err := os.ReadFile(pidPath)
		testutil.Require(t, err)
		if string(data) != pid {
			t.Fatal("inspection changed the stale PID record")
		}
	}
}

func TestInspectPIDDoesNotWaitForServiceLock(t *testing.T) {
	r := &base.Runtime{Root: t.TempDir(), Config: base.DefaultConfig(), Out: io.Discard}
	lock, err := base.AcquireServiceLock(r.RunDir(), 0)
	testutil.Require(t, err)
	defer lock.Close()
	start := time.Now()
	_, err = service.New(r).InspectPID()
	if !errors.Is(err, base.ErrServiceLockTimeout) {
		t.Fatalf("lock contention error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("inspection waited for service lock: %v", elapsed)
	}
}

func TestInspectPIDVerifiesRealCoreAndPreservesMetadata(t *testing.T) {
	r := testutil.NewRuntime(t)
	svc := service.New(r)
	testutil.Require(t, svc.Service("start"))
	t.Cleanup(func() { _ = svc.Service("stop") })
	pidBefore, err := os.ReadFile(r.PIDFile())
	testutil.Require(t, err)
	identityPath := r.PIDFile() + ".identity"
	identityBefore, err := os.ReadFile(identityPath)
	testutil.Require(t, err)
	modulePath := filepath.Join(r.ModuleDir, "module.prop")
	moduleBefore, err := os.ReadFile(modulePath)
	testutil.Require(t, err)
	pid, err := svc.InspectPID()
	testutil.Require(t, err)
	if strconv.Itoa(pid)+"\n" != string(pidBefore) {
		t.Fatalf("inspected wrong PID: %d", pid)
	}
	for path, before := range map[string][]byte{r.PIDFile(): pidBefore, identityPath: identityBefore, modulePath: moduleBefore} {
		after, err := os.ReadFile(path)
		testutil.Require(t, err)
		if string(after) != string(before) {
			t.Fatalf("inspection changed %s", path)
		}
	}
}
