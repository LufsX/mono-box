package automation

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
	"github.com/LufsX/mono-box/internal/box/service"
	"github.com/LufsX/mono-box/internal/box/storage"
	"github.com/LufsX/mono-box/internal/box/testutil"
)

// This core blocks inside real -t validation until another process creates its
// release marker. Production exec, flock, download and rename paths stay intact.
const guardedCoreSource = `package main
import("os";"path/filepath";"strings";"time")
func main(){
 args:=os.Args[1:];test:=len(args)>0&&args[0]=="-t";if test{args=args[1:]}
 if len(args)!=4||args[0]!="-d"||args[2]!="-f"{os.Exit(2)}
 data,err:=os.ReadFile(args[3]);if err!=nil{os.Exit(2)}
 mode:=strings.TrimSpace(string(data))
 if test {
  if strings.HasPrefix(mode,"block"){
   os.WriteFile(filepath.Join(args[1],mode+".started"),nil,0600)
   for {if _,err:=os.Stat(filepath.Join(args[1],mode+".release"));err==nil{break};time.Sleep(5*time.Millisecond)}
  }
  return
 }
 os.WriteFile(filepath.Join(args[1],"core-started"),nil,0600)
 for{time.Sleep(time.Hour)}
}
`

var guardedCore struct {
	sync.Once
	data []byte
	err  error
}

func guardedFixture(t *testing.T) *Runtime {
	t.Helper()
	guardedCore.Do(func() {
		dir, err := os.MkdirTemp("", "monobox-guard-core-")
		if err != nil {
			guardedCore.err = err
			return
		}
		defer os.RemoveAll(dir)
		source := filepath.Join(dir, "main.go")
		if err := os.WriteFile(source, []byte(guardedCoreSource), 0600); err != nil {
			guardedCore.err = err
			return
		}
		path := filepath.Join(dir, "core")
		cmd := exec.Command("go", "build", "-o", path, source)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if output, err := cmd.CombinedOutput(); err != nil {
			guardedCore.err = errors.New(string(output))
			return
		}
		guardedCore.data, guardedCore.err = os.ReadFile(path)
	})
	if guardedCore.err != nil {
		t.Fatal(guardedCore.err)
	}
	r := New(testutil.NewRuntime(t))
	writeConfigFixture(t, r, "bin_name=mihomo\nCORE_USER_GROUP='"+r.Config.CoreUserGroup+"'\n")
	testutil.Write(t, r.CoreBinary(), guardedCore.data, 0700)
	return r
}

func TestAutomationSettingsSubprocess(t *testing.T) {
	root := os.Getenv("MONO_AUTOMATION_TEST_ROOT")
	if root == "" {
		return
	}
	r := New(&base.Runtime{Root: root, Out: io.Discard})
	if err := r.Command([]string{"set", os.Getenv("MONO_AUTOMATION_TEST_SETTINGS")}); err != nil {
		t.Fatal(err)
	}
}

func subprocessSet(t *testing.T, r *Runtime, settings string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestAutomationSettingsSubprocess$")
	cmd.Env = append(os.Environ(), "MONO_AUTOMATION_TEST_ROOT="+r.Root, "MONO_AUTOMATION_TEST_SETTINGS="+base64.StdEncoding.EncodeToString([]byte(settings)))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("set blocked during validation: %v %s", err, output)
	}
}

func beginAttempt(t *testing.T, r *Runtime, update bool) (record, base.CommitGuard) {
	t.Helper()
	var rec record
	_, err := r.locked(context.Background(), func(current *record) error {
		if update {
			current.UpdateAttempt++
			current.Update.State = "updating"
		} else {
			current.RecoveryAttempt++
			current.Recovery.State = "recovering"
		}
		rec = *current
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if update {
		return rec, r.commitGuard(true, rec.UpdateGeneration, rec.UpdateAttempt)
	}
	return rec, r.commitGuard(false, rec.RecoveryGeneration, rec.RecoveryAttempt)
}

func waitMarker(t *testing.T, r *Runtime, mode string) {
	t.Helper()
	waitFor(t, func() bool { return base.Exists(filepath.Join(r.CoreDir(), mode+".started")) })
}

func releaseValidation(t *testing.T, r *Runtime, mode string) {
	t.Helper()
	testutil.Write(t, filepath.Join(r.CoreDir(), mode+".release"), nil, 0600)
}

func TestRecoveryCommitRejectsDisableAndABAAfterRealValidation(t *testing.T) {
	for _, aba := range []bool{false, true} {
		name := "disable"
		if aba {
			name = "disable-enable"
		}
		t.Run(name, func(t *testing.T) {
			r := guardedFixture(t)
			setJSON(t, r, `{"recoveryEnabled":true}`)
			lock, err := base.AcquireServiceLock(r.RunDir(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			intent, err := r.WriteServiceIntent(true)
			_ = lock.Close()
			if err != nil {
				t.Fatal(err)
			}
			_, guard := beginAttempt(t, r, false)
			testutil.Write(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte("block-recovery"), 0600)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- service.New(r.Runtime).RecoverGuarded(ctx, intent.Revision, guard) }()
			waitMarker(t, r, "block-recovery")
			subprocessSet(t, r, "{}")
			if aba {
				subprocessSet(t, r, `{"recoveryEnabled":true}`)
			}
			releaseValidation(t, r, "block-recovery")
			select {
			case err := <-done:
				if !errors.Is(err, base.ErrAutomationChanged) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("recovery did not finish")
			}
			if base.Exists(filepath.Join(r.CoreDir(), "core-started")) || base.Exists(r.PIDFile()) {
				t.Fatal("old task started core after set succeeded")
			}
		})
	}
}

func TestUpdateCommitRejectsDisableAndABAAfterRealValidation(t *testing.T) {
	for _, aba := range []bool{false, true} {
		name := "disable"
		if aba {
			name = "disable-enable"
		}
		t.Run(name, func(t *testing.T) {
			r := guardedFixture(t)
			setJSON(t, r, `{"updateEnabled":true,"updateWifiOnly":false}`)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("block-update")) }))
			defer server.Close()
			writeSubscription(t, r, "a.yaml", server.URL)
			_, guard := beginAttempt(t, r, true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan taskResult, 1)
			go func() {
				updated, err := storage.New(r.Runtime).ScheduledUpdateGuarded(ctx, guard)
				done <- taskResult{updated: updated, err: err}
			}()
			waitMarker(t, r, "block-update")
			subprocessSet(t, r, "{}")
			if aba {
				subprocessSet(t, r, `{"updateEnabled":true,"updateWifiOnly":false}`)
			}
			releaseValidation(t, r, "block-update")
			select {
			case result := <-done:
				if result.updated != 0 || !errors.Is(result.err, base.ErrAutomationChanged) {
					t.Fatalf("%+v", result)
				}
			case <-time.After(time.Second):
				t.Fatal("update did not finish")
			}
			data, err := os.ReadFile(subscriptionPath(r, "a.yaml"))
			if err != nil || string(data) != "old" {
				t.Fatalf("old task renamed config: %q %v", data, err)
			}
		})
	}
}

func subscriptionPath(r *Runtime, name string) string {
	return filepath.Join(r.CoreDir(), ".mono-box", "profiles", name)
}
func writeSubscription(t *testing.T, r *Runtime, name, source string) {
	t.Helper()
	testutil.Write(t, subscriptionPath(r, name), []byte("old"), 0600)
	testutil.Write(t, filepath.Join(r.CoreDir(), ".mono-box", "sources", name+".url"), []byte(source), 0600)
}
