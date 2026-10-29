//go:build linux || android

package download

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

type pausedCoreDownload struct {
	r        *Runtime
	core     []byte
	entered  chan struct{}
	release  func()
	done     chan error
	finished chan struct{}
}

func pauseCoreDownload(t *testing.T, original []byte, overwrite bool, phase string) *pausedCoreDownload {
	t.Helper()
	arch := downloadTestArch(t, "arm64-v8a")
	p := &pausedCoreDownload{
		core: downloadTestELF(arch), entered: make(chan struct{}), done: make(chan error, 1),
		finished: make(chan struct{}),
	}
	release := make(chan struct{})
	var once sync.Once
	p.release = func() { once.Do(func() { close(release) }) }
	name := "mihomo-android-arm64.gz"
	payload := downloadTestArchive(t, "gz", downloadTestMember{"mihomo", p.core, 0})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		metadata := strings.Contains(req.URL.Path, "/releases/latest")
		if metadata == (phase == "metadata") {
			// Block the response body so downloadGet is waiting on network I/O.
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			close(p.entered)
			select {
			case <-release:
			case <-req.Context().Done():
				return
			}
		}
		if metadata {
			_ = json.NewEncoder(w).Encode(map[string]any{"assets": []downloadAsset{{name, "https://github.com/" + name}}})
		} else {
			_, _ = w.Write(payload)
		}
	}))
	p.r, _ = downloadTestRuntime(t, downloadTestClient(server, nil), original)
	p.r.Out = io.Discard
	t.Cleanup(func() {
		p.release()
		server.Close()
		select {
		case <-p.finished:
		case <-time.After(5 * time.Second):
			t.Error("download goroutine did not stop")
		}
	})
	go func() {
		defer close(p.finished)
		p.done <- p.r.DownloadCore("mihomo", overwrite, false, arch.abi)
	}()
	select {
	case <-p.entered:
	case err := <-p.done:
		t.Fatalf("download ended before pause: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("download did not reach pause")
	}
	return p
}

func (p *pausedCoreDownload) finish(t *testing.T) error {
	t.Helper()
	p.release()
	select {
	case err := <-p.done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("download did not finish")
		return nil
	}
}

func downloadAssertNoTemps(t *testing.T, bin string) {
	t.Helper()
	entries, err := os.ReadDir(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".monobox-download-") {
			t.Fatalf("staging directory leaked: %s", filepath.Join(bin, entry.Name()))
		}
	}
}

func downloadAssertContents(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("unexpected contents at %s: %q, %v", path, got, err)
	}
}

func TestDownloadNetworkAllowsServiceLockAndStart(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep unavailable")
	}
	original, err := os.ReadFile(sleep)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"metadata", "asset"} {
		t.Run(phase, func(t *testing.T) {
			p := pauseCoreDownload(t, original, true, phase)
			bin := filepath.Join(p.r.Root, "bin")
			target := filepath.Join(bin, "mihomo")
			// Model the service start critical section: take the real service
			// lock and execute the installed ELF while the HTTP body is blocked.
			lock, err := base.AcquireServiceLock(filepath.Join(p.r.Root, "run"), 200*time.Millisecond)
			if err != nil {
				t.Fatalf("network download holds service.lock: %v", err)
			}
			cmd := exec.Command(target, "30")
			err = cmd.Start()
			lock.Close()
			if err != nil {
				t.Fatalf("cannot start old core during download (writable handle retained): %v", err)
			}
			defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
			if err := p.finish(t); err == nil || !strings.Contains(err.Error(), "running") {
				t.Fatalf("commit accepted running core: %v", err)
			}
			if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
				t.Fatalf("download stopped the running core: %v", err)
			}
			downloadAssertContents(t, target, original)
			downloadAssertNoTemps(t, bin)
		})
	}
}

func TestDownloadExternalStartThenStopAllowsCommit(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep unavailable")
	}
	original, err := os.ReadFile(sleep)
	if err != nil {
		t.Fatal(err)
	}
	p := pauseCoreDownload(t, original, true, "asset")
	target := filepath.Join(p.r.Root, "bin", "mihomo")
	// No service lock or PID file: an external start must work too.
	cmd := exec.Command(target, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if err := p.finish(t); err != nil {
		t.Fatal(err)
	}
	downloadAssertContents(t, target, p.core)
	downloadAssertNoTemps(t, filepath.Dir(target))
}

func TestDownloadCommitWaitsForServiceLock(t *testing.T) {
	p := pauseCoreDownload(t, []byte("old core"), true, "asset")
	lock, err := base.AcquireServiceLock(filepath.Join(p.r.Root, "run"), 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	p.release()
	select {
	case err := <-p.done:
		t.Fatalf("commit bypassed service.lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	downloadAssertContents(t, filepath.Join(p.r.Root, "bin", "mihomo"), []byte("old core"))
	// Change the target while holding the service lock. Commit must validate
	// after acquiring the lock, rather than trusting checks made before it.
	replacement := []byte("newer core")
	if err := os.WriteFile(filepath.Join(p.r.Root, "bin", "mihomo"), replacement, 0700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := p.finish(t); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("commit failed to revalidate after lock: %v", err)
	}
	downloadAssertContents(t, filepath.Join(p.r.Root, "bin", "mihomo"), replacement)
	downloadAssertNoTemps(t, filepath.Join(p.r.Root, "bin"))
}

func TestDownloadCommitRejectsTargetChanges(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		for _, change := range []string{"replace", "in-place", "remove", "symlink", "hardlink", "directory", "fifo", "absent-created", "absent-ABA"} {
			t.Run(change+"/overwrite="+map[bool]string{false: "false", true: "true"}[overwrite], func(t *testing.T) {
				original := []byte{} // Empty targets may be replaced without overwrite.
				if overwrite {
					original = []byte("old core")
				}
				if strings.HasPrefix(change, "absent-") {
					original = nil
				}
				p := pauseCoreDownload(t, original, overwrite, "asset")
				bin := filepath.Join(p.r.Root, "bin")
				target := filepath.Join(bin, "mihomo")
				replacement := []byte("replacement must survive")
				victim := filepath.Join(t.TempDir(), "victim")
				if err := os.WriteFile(victim, replacement, 0700); err != nil {
					t.Fatal(err)
				}
				switch change {
				case "in-place", "absent-created":
					if err := os.WriteFile(target, replacement, 0700); err != nil {
						t.Fatal(err)
					}
				case "absent-ABA":
					before, err := os.Stat(bin)
					if err != nil {
						t.Fatal(err)
					}
					// Ensure the timestamp changes even on a coarse filesystem.
					deadline := time.Now().Add(2 * time.Second)
					for {
						if time.Now().After(deadline) {
							t.Fatal("directory timestamps did not change")
						}
						if err := os.WriteFile(target, replacement, 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.Remove(target); err != nil {
							t.Fatal(err)
						}
						after, err := os.Stat(bin)
						if err != nil {
							t.Fatal(err)
						}
						a := before.Sys().(*syscall.Stat_t)
						b := after.Sys().(*syscall.Stat_t)
						if a.Mtim != b.Mtim || a.Ctim != b.Ctim {
							break
						}
					}
				default:
					if err := os.Remove(target); err != nil {
						t.Fatal(err)
					}
					switch change {
					case "replace":
						if err := os.Rename(victim, target); err != nil {
							t.Fatal(err)
						}
					case "symlink":
						if err := os.Symlink(victim, target); err != nil {
							t.Fatal(err)
						}
					case "hardlink":
						if err := os.Link(victim, target); err != nil {
							t.Fatal(err)
						}
					case "directory":
						if err := os.Mkdir(target, 0700); err != nil {
							t.Fatal(err)
						}
					case "fifo":
						if err := syscall.Mkfifo(target, 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				before, beforeErr := os.Lstat(target)
				if err := p.finish(t); err == nil || !strings.Contains(err.Error(), "changed") {
					t.Fatalf("accepted target change: %v", err)
				}
				after, afterErr := os.Lstat(target)
				if errors.Is(beforeErr, os.ErrNotExist) {
					if !errors.Is(afterErr, os.ErrNotExist) {
						t.Fatalf("missing target was installed: %v", afterErr)
					}
				} else if beforeErr != nil || afterErr != nil || !os.SameFile(before, after) {
					t.Fatalf("target identity changed on failure: %v, %v", beforeErr, afterErr)
				}
				if change == "replace" || change == "in-place" || change == "absent-created" {
					downloadAssertContents(t, target, replacement)
				}
				if change == "symlink" || change == "hardlink" {
					downloadAssertContents(t, victim, replacement)
				}
				downloadAssertNoTemps(t, bin)
			})
		}
	}
}

func TestDownloadCommitRejectsDirectoryChanges(t *testing.T) {
	for _, component := range []string{"bin", "run", "root"} {
		for _, symlink := range []bool{false, true} {
			t.Run(component+map[bool]string{false: "/replacement", true: "/symlink"}[symlink], func(t *testing.T) {
				original := []byte("old core")
				p := pauseCoreDownload(t, original, true, "asset")
				root := p.r.Root
				path := root
				if component != "root" {
					path = filepath.Join(root, component)
				}
				moved := path + "-moved"
				if err := os.Rename(path, moved); err != nil {
					t.Fatal(err)
				}
				if symlink {
					if err := os.Symlink(moved, path); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
					if component == "root" {
						if err := os.Mkdir(filepath.Join(path, "bin"), 0700); err != nil {
							t.Fatal(err)
						}
					}
					if component == "root" || component == "bin" {
						newBin := path
						if component == "root" {
							newBin = filepath.Join(path, "bin")
						}
						if err := os.WriteFile(filepath.Join(newBin, "mihomo"), []byte("new directory core"), 0700); err != nil {
							t.Fatal(err)
						}
					}
				}
				if err := p.finish(t); err == nil {
					t.Fatal("accepted changed directory")
				}
				oldBin := filepath.Join(root, "bin")
				if component == "root" {
					oldBin = filepath.Join(moved, "bin")
				} else if component == "bin" {
					oldBin = moved
				}
				downloadAssertContents(t, filepath.Join(oldBin, "mihomo"), original)
				downloadAssertNoTemps(t, oldBin)
				if !symlink && component != "run" {
					downloadAssertContents(t, filepath.Join(root, "bin", "mihomo"), []byte("new directory core"))
				}
			})
		}
	}
}

func TestDownloadCommitRejectsLockReplacement(t *testing.T) {
	for _, name := range []string{"service.lock", "download.lock"} {
		t.Run(name, func(t *testing.T) {
			original := []byte("old core")
			p := pauseCoreDownload(t, original, true, "asset")
			path := filepath.Join(p.r.Root, "run", name)
			if err := os.Rename(path, path+".old"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := p.finish(t); err == nil || !strings.Contains(err.Error(), "changed") {
				t.Fatalf("accepted replaced lock: %v", err)
			}
			downloadAssertContents(t, filepath.Join(p.r.Root, "bin", "mihomo"), original)
			downloadAssertNoTemps(t, filepath.Join(p.r.Root, "bin"))
		})
	}
}

func TestDownloadPreparationRejectsSpecialTargets(t *testing.T) {
	for _, kind := range []string{"directory", "fifo", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			client := &http.Client{Transport: downloadTestTransport(func(*http.Request) (*http.Response, error) {
				t.Error("unsafe target reached HTTP")
				return nil, errors.New("unexpected HTTP")
			})}
			r, _ := downloadTestRuntime(t, client, nil)
			target := filepath.Join(r.Root, "bin", "mihomo")
			switch kind {
			case "directory":
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := syscall.Mkfifo(target, 0600); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				source := filepath.Join(t.TempDir(), "core")
				if err := os.WriteFile(source, []byte("preserve"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(source, target); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.DownloadCore("mihomo", true, false, "arm64-v8a"); err == nil {
				t.Fatal("accepted unsafe target")
			}
			downloadAssertNoTemps(t, filepath.Join(r.Root, "bin"))
		})
	}
}

func TestDownloadIndependentLockSerializesNetwork(t *testing.T) {
	arch := downloadTestArch(t, "arm64-v8a")
	core := downloadTestELF(arch)
	entered := make(chan int32, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var hits atomic.Int32
	name := "mihomo-android-arm64"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/releases/") {
			n := hits.Add(1)
			entered <- n
			if n == 1 {
				select {
				case <-release:
				case <-req.Context().Done():
					return
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"assets": []downloadAsset{{name, "https://github.com/" + name}}})
			return
		}
		_, _ = w.Write(core)
	}))
	defer server.Close()
	defer unblock()
	r, _ := downloadTestRuntime(t, downloadTestClient(server, nil), []byte("old core"))
	r.Out = io.Discard
	results := make(chan error, 2)
	go func() { results <- r.DownloadCore("mihomo", true, false, arch.abi) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first download did not start")
	}
	// Separate Runtime and HTTP client, sharing only filesystem locks.
	alias := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(r.Root, alias); err != nil {
		t.Fatal(err)
	}
	other := New(&base.Runtime{Root: alias, HTTP: downloadTestClient(server, nil), Out: io.Discard})
	go func() { results <- other.DownloadCore("mihomo_smart", true, false, arch.abi) }()
	select {
	case <-entered:
		t.Fatal("concurrent download reached HTTP while first download was blocked")
	case err := <-results:
		t.Fatalf("download ended while blocked: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	lock, err := base.AcquireServiceLock(filepath.Join(r.Root, "run"), 200*time.Millisecond)
	if err != nil {
		t.Fatalf("waiting downloader held service.lock: %v", err)
	}
	lock.Close()
	unblock()
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("serialized download did not finish")
		}
	}
	if hits.Load() != 2 {
		t.Fatalf("metadata requests: %d", hits.Load())
	}
	downloadAssertContents(t, filepath.Join(r.Root, "bin", "mihomo"), core)
	downloadAssertNoTemps(t, filepath.Join(r.Root, "bin"))
}
