package storage

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
	"github.com/LufsX/mono-box/internal/box/service"
)

func downloadTestRuntime(r *Runtime) *Runtime {
	copy := New(r.WithConfig(r.Config))
	copy.Out, copy.Err = io.Discard, io.Discard
	return copy
}

func downloadTestResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not finish")
		return nil
	}
}

func downloadTestSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not reach barrier")
	}
}

type downloadTestBarrier struct {
	server  *httptest.Server
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newDownloadTestBarrier(t *testing.T, body string, status int) *downloadTestBarrier {
	t.Helper()
	b := &downloadTestBarrier{started: make(chan struct{}, 4), release: make(chan struct{})}
	b.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b.started <- struct{}{}
		select {
		case <-b.release:
		case <-req.Context().Done():
			return
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(b.server.Close)
	t.Cleanup(b.resume)
	return b
}

func (b *downloadTestBarrier) resume() { b.once.Do(func() { close(b.release) }) }

func startDownloadTest(r *Runtime, command, source, name string) <-chan error {
	done := make(chan error, 1)
	args := []string{"profiles", command, storageB64(name)}
	if command == "download" {
		args = []string{"profiles", command, storageB64(source), storageB64(name)}
	}
	go func() { done <- r.Storage(args) }()
	return done
}

func TestProfileDownloadAllowsServiceAndConfigDuringHTTP(t *testing.T) {
	for _, command := range []string{"download", "update"} {
		t.Run(command, func(t *testing.T) {
			r, _, _ := storageFixture(t)
			r = downloadTestRuntime(r)
			b := newDownloadTestBarrier(t, "mode: global\n", 200)
			name := "remote.yaml"
			if command == "update" {
				storageRun(t, r, "profiles", "import", storageB64(name), storageB64("mode: rule\n"))
				storageWriteFixture(t, storageMetaPath(r, "sources/"+name+".url"), b.server.URL, 0600)
			}
			r = downloadTestRuntime(r)
			done := startDownloadTest(r, command, b.server.URL, name)
			downloadTestSignal(t, b.started)
			for _, action := range []string{"status", "stop"} {
				serviceDone := make(chan error, 1)
				svc := service.New(r.WithConfig(r.Config))
				go func() { serviceDone <- svc.Service(action) }()
				err := downloadTestResult(t, serviceDone)
				if err != nil && !errors.Is(err, service.ErrStopped) {
					t.Fatal(err)
				}
			}
			patchDone := make(chan error, 1)
			go func() {
				patchDone <- downloadTestRuntime(r).PatchConfig(storageB64("{\"clash_api_port\":\"9099\",\"clash_api_secret\":\"latest-secret\"}"))
			}()
			if err := downloadTestResult(t, patchDone); err != nil {
				t.Fatal(err)
			}
			// Check reloading itself, including caller immutability. The commit
			// below must also accept this unrelated configuration change.
			current, err := r.profileDownloadRuntime()
			if err != nil || current.Config.APIPort != 9099 || current.Config.APISecret != "latest-secret" || r.Config.APIPort != 9090 {
				t.Fatalf("config reload: %#v, %v", current, err)
			}
			b.resume()
			if err := downloadTestResult(t, done); err != nil {
				t.Fatal(err)
			}
			if got := storageRead(t, storageProfilePath(r, name)); got != "mode: global\n" {
				t.Fatal(got)
			}
			storageNoTemps(t, r)
		})
	}
}

func TestProfileDownloadRejectsConcurrentChanges(t *testing.T) {
	cases := []struct {
		name   string
		empty  bool
		mutate func(*testing.T, *Runtime, string)
	}{
		{"import", false, func(t *testing.T, r *Runtime, name string) {
			storageRun(t, downloadTestRuntime(r), "profiles", "import", storageB64(name), storageB64("mode: direct\n"))
		}},
		{"delete", false, func(t *testing.T, r *Runtime, name string) {
			storageRun(t, downloadTestRuntime(r), "profiles", "delete", storageB64(name))
		}},
		{"delete-recreate", false, func(t *testing.T, r *Runtime, name string) {
			storageRun(t, downloadTestRuntime(r), "profiles", "delete", storageB64(name))
			storageRun(t, downloadTestRuntime(r), "profiles", "import", storageB64(name), storageB64("mode: rule\n"))
		}},
		{"source-url", false, func(t *testing.T, r *Runtime, name string) {
			storageWriteFixture(t, storageMetaPath(r, "sources/"+name+".url"), "https://example.invalid/?token=other-secret", 0600)
		}},
		{"source-same-url-new-inode", false, func(t *testing.T, r *Runtime, name string) {
			path := storageMetaPath(r, "sources/"+name+".url")
			if err := base.AtomicWrite(path, []byte(storageRead(t, path)), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"profile-same-content-new-inode", false, func(t *testing.T, r *Runtime, name string) {
			path := storageProfilePath(r, name)
			if err := base.AtomicWrite(path, []byte(storageRead(t, path)), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"profile-in-place", false, func(t *testing.T, r *Runtime, name string) {
			storageWriteFixture(t, storageProfilePath(r, name), "mode: direct\n", 0600)
		}},
		{"bin-name", false, func(t *testing.T, r *Runtime, name string) {
			if err := downloadTestRuntime(r).PatchConfig(storageB64("{\"bin_name\":\"other\"}")); err != nil {
				t.Fatal(err)
			}
		}},
		{"invalid-config", false, func(t *testing.T, r *Runtime, name string) {
			storageWriteFixture(t, filepath.Join(r.Root, "scripts", "box.config"), "clash_api_secret=private-secret;false", 0600)
		}},
		{"new-profile", true, func(t *testing.T, r *Runtime, name string) {
			storageRun(t, downloadTestRuntime(r), "profiles", "import", storageB64(name), storageB64("mode: direct\n"))
		}},
		{"missing-import-delete", true, func(t *testing.T, r *Runtime, name string) {
			storageRun(t, downloadTestRuntime(r), "profiles", "import", storageB64(name), storageB64("mode: direct\n"))
			storageRun(t, downloadTestRuntime(r), "profiles", "delete", storageB64(name))
		}},
		{"missing-source-create-delete", true, func(t *testing.T, r *Runtime, name string) {
			path := storageMetaPath(r, "sources/"+name+".url")
			storageWriteFixture(t, path, "https://example.invalid/?token=other-secret", 0600)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{"new-source", true, func(t *testing.T, r *Runtime, name string) {
			storageWriteFixture(t, storageMetaPath(r, "sources/"+name+".url"), "https://example.invalid/?token=other-secret", 0600)
		}},
	}
	for _, relative := range []string{"", ".mono-box", ".mono-box/profiles", ".mono-box/sources"} {
		relative := relative
		cases = append(cases, struct {
			name   string
			empty  bool
			mutate func(*testing.T, *Runtime, string)
		}{"directory-" + relative, false, func(t *testing.T, r *Runtime, name string) {
			path := filepath.Join(r.CoreDir(), relative)
			if err := os.Rename(path, path+".old"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}})
	}
	for _, command := range []string{"download", "update"} {
		for _, tc := range cases {
			if command == "update" && tc.empty {
				continue
			}
			t.Run(command+"/"+tc.name, func(t *testing.T) {
				r, _, _ := storageFixture(t)
				name := "remote.yaml"
				b := newDownloadTestBarrier(t, "mode: global\n# downloaded-secret\n", 200)
				if !tc.empty {
					storageRun(t, r, "profiles", "import", storageB64(name), storageB64("mode: rule\n"))
					storageWriteFixture(t, storageMetaPath(r, "sources/"+name+".url"), b.server.URL, 0600)
				}
				if tc.empty {
					// Make parent mutations observable without depending on the
					// filesystem clock resolution or adding timing sleeps.
					for _, parent := range []string{"profiles", "sources"} {
						if err := os.MkdirAll(storageMetaPath(r, parent), 0700); err != nil {
							t.Fatal(err)
						}
						old := time.Unix(1, 0)
						if err := os.Chtimes(storageMetaPath(r, parent), old, old); err != nil {
							t.Fatal(err)
						}
					}
				}
				r = downloadTestRuntime(r)
				done := startDownloadTest(r, command, b.server.URL, name)
				downloadTestSignal(t, b.started)
				tc.mutate(t, r, name)
				b.resume()
				err := downloadTestResult(t, done)
				if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), b.server.URL) {
					t.Fatalf("conflict result: %v", err)
				}
				// Inspect both the current and renamed trees: stale directory
				// descriptors must never receive the downloaded bytes.
				if err := filepath.WalkDir(r.Root, func(path string, entry os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !entry.IsDir() && entry.Name() == name {
						if strings.Contains(storageRead(t, path), "downloaded-secret") {
							t.Errorf("overwrote concurrent target: %s", path)
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				storageNoTemps(t, r)
			})
		}
	}
}

func TestProfileDownloadConcurrentWinner(t *testing.T) {
	for _, command := range []string{"download", "update"} {
		t.Run(command, func(t *testing.T) {
			r, _, _ := storageFixture(t)
			name := "remote.yaml"
			b := newDownloadTestBarrier(t, "mode: global\n", 200)
			if command == "update" {
				storageRun(t, r, "profiles", "import", storageB64(name), storageB64("mode: rule\n"))
				storageWriteFixture(t, storageMetaPath(r, "sources/"+name+".url"), b.server.URL, 0600)
			}
			r = downloadTestRuntime(r)
			first := startDownloadTest(r, command, b.server.URL, name)
			second := startDownloadTest(r, command, b.server.URL, name)
			downloadTestSignal(t, b.started)
			downloadTestSignal(t, b.started)
			b.resume()
			a, bErr := downloadTestResult(t, first), downloadTestResult(t, second)
			if (a == nil) == (bErr == nil) {
				t.Fatalf("expected one winner: %v, %v", a, bErr)
			}
			if storageRead(t, storageProfilePath(r, name)) != "mode: global\n" {
				t.Fatal("lost winner")
			}
			storageNoTemps(t, r)
		})
	}
}

func TestProfileDownloadFailureKeepsSnapshot(t *testing.T) {
	r, _, _ := storageFixture(t)
	name := "remote.yaml"
	b := newDownloadTestBarrier(t, "private-secret", 503)
	storageRun(t, r, "profiles", "import", storageB64(name), storageB64("mode: rule\n"))
	sourcePath := storageMetaPath(r, "sources/"+name+".url")
	storageWriteFixture(t, sourcePath, b.server.URL, 0600)
	r = downloadTestRuntime(r)
	done := startDownloadTest(r, "update", "", name)
	downloadTestSignal(t, b.started)
	b.resume()
	if err := downloadTestResult(t, done); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
	if storageRead(t, storageProfilePath(r, name)) != "mode: rule\n" || storageRead(t, sourcePath) != b.server.URL {
		t.Fatal("failed download changed profile/source")
	}
	storageNoTemps(t, r)
}

func TestProfileDownloadConfigSemantics(t *testing.T) {
	for _, invalid := range []string{"", "..", "../outside", "bad\tname", "bad\x00name"} {
		t.Run(fmt.Sprintf("invalid-%q", invalid), func(t *testing.T) {
			r, _, _ := storageFixture(t)
			r.Config.BinName = invalid
			if _, err := r.profileDownloadRuntime(); err == nil {
				t.Fatal("disk config masked invalid embedder")
			}
		})
	}
	r, _, _ := storageFixture(t)
	if err := os.RemoveAll(filepath.Join(r.Root, "scripts")); err != nil {
		t.Fatal(err)
	}
	r.Config.BinName, r.Config.APIPort = "embedded", 9098
	current, err := r.profileDownloadRuntime()
	if err != nil || current.Config != r.Config {
		t.Fatalf("lost embedded Config: %v", err)
	}
	b := newDownloadTestBarrier(t, "mode: global\n", 200)
	r = downloadTestRuntime(r)
	done := startDownloadTest(r, "download", b.server.URL, "remote.yaml")
	downloadTestSignal(t, b.started)
	b.resume()
	if err := downloadTestResult(t, done); err != nil {
		t.Fatal(err)
	}
	if storageRead(t, storageProfilePath(r, "remote.yaml")) != "mode: global\n" {
		t.Fatal("wrong embedded directory")
	}
}

func TestProfileDownloadUsesLatestSelectionAndScopesMissingTargetConflicts(t *testing.T) {
	for _, command := range []string{"download", "update"} {
		t.Run(command, func(t *testing.T) {
			r, _, _ := storageFixture(t)
			name := "remote.yaml"
			b := newDownloadTestBarrier(t, "mode: global\n", 200)
			if command == "update" {
				storageRun(t, r, "profiles", "import", storageB64(name), storageB64("mode: rule\n"))
				storageWriteFixture(t, storageMetaPath(r, "sources/"+name+".url"), b.server.URL, 0600)
			}
			r = downloadTestRuntime(r)
			out := new(strings.Builder)
			r.Out = out
			done := startDownloadTest(r, command, b.server.URL, name)
			downloadTestSignal(t, b.started)
			other := downloadTestRuntime(r)
			storageRun(t, other, "profiles", "import", storageB64("other.yaml"), storageB64("mode: direct\n"))
			selected := "other.yaml"
			if command == "update" {
				selected = name
			}
			storageRun(t, other, "profiles", "switch", storageB64(selected), "profile")
			b.resume()
			err := downloadTestResult(t, done)
			if command == "download" {
				// Missing targets conservatively reject unrelated parent changes.
				if err == nil {
					t.Fatal("accepted changed directory for missing target")
				}
				if _, err := os.Stat(storageProfilePath(r, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("created profile after directory conflict")
				}
				storageNoTemps(t, r)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if storageRead(t, storageMetaPath(r, "active-profile")) != selected {
				t.Fatal("lost latest selection")
			}
			wantActive := fmt.Sprintf("\t%t\n", selected == name)
			if !strings.HasSuffix(out.String(), wantActive) {
				t.Fatalf("stale active row: %q", out.String())
			}
			storageNoTemps(t, r)
		})
	}
}

func TestProfileDownloadReloadsInitialConfigAndPreservesCaller(t *testing.T) {
	r, _, _ := storageFixture(t)
	storageRun(t, r, "config", "write", storageB64("bin_name=other\nclash_api_port=9099\n"))
	b := newDownloadTestBarrier(t, "mode: global\n", 200)
	r = downloadTestRuntime(r)
	done := startDownloadTest(r, "download", b.server.URL, "remote.yaml")
	downloadTestSignal(t, b.started)
	b.resume()
	if err := downloadTestResult(t, done); err != nil {
		t.Fatal(err)
	}
	if r.Config.BinName != "mihomo" || r.Config.APIPort != 9090 {
		t.Fatal("mutated caller config")
	}
	if storageRead(t, filepath.Join(r.Root, "other/.mono-box/profiles/remote.yaml")) != "mode: global\n" {
		t.Fatal("did not use config loaded under first lock")
	}
	if _, err := os.Stat(storageProfilePath(r, "remote.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrote stale runtime directory")
	}
}

func downloadTestOpenFiles(t *testing.T, root string) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err == nil && strings.HasPrefix(target, root+"/") {
			count++
		}
	}
	return count
}

func TestProfileDownloadClosesPinnedFiles(t *testing.T) {
	r, _, _ := storageFixture(t)
	r = downloadTestRuntime(r)
	name := "remote.yaml"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "fail") {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, "mode: rule\n")
	}))
	defer server.Close()
	baseline := downloadTestOpenFiles(t, r.Root)
	for i := 0; i < 4; i++ {
		if err := r.Storage([]string{"profiles", "download", storageB64(server.URL), storageB64(name)}); err != nil {
			t.Fatal(err)
		}
		if err := r.Storage([]string{"profiles", "update", storageB64(name)}); err != nil {
			t.Fatal(err)
		}
		if err := r.Storage([]string{"profiles", "download", storageB64(server.URL + "/fail?token=private-secret"), storageB64(name)}); err == nil {
			t.Fatal("expected HTTP failure")
		}
		// Missing and invalid source errors must close even partially prepared snapshots.
		if err := os.Remove(storageMetaPath(r, "sources/"+name+".url")); err != nil {
			t.Fatal(err)
		}
		if err := r.Storage([]string{"profiles", "update", storageB64(name)}); err == nil {
			t.Fatal("accepted missing source")
		}
		storageWriteFixture(t, storageMetaPath(r, "sources/"+name+".url"), "invalid secret", 0600)
		if err := r.Storage([]string{"profiles", "update", storageB64(name)}); err == nil {
			t.Fatal("accepted invalid source")
		}
		if got := downloadTestOpenFiles(t, r.Root); got != baseline {
			t.Fatalf("leaked storage descriptors: before=%d, after=%d", baseline, got)
		}
	}
	storageNoTemps(t, r)
}

// Receiving EOF/Close establishes that HTTP has completed before testing the
// second lock acquisition, without relying on server response timing.
type downloadTestTransport struct {
	base   http.RoundTripper
	closed chan struct{}
}

func (t downloadTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err == nil {
		resp.Body = &downloadTestBody{ReadCloser: resp.Body, closed: t.closed}
	}
	return resp, err
}

type downloadTestBody struct {
	io.ReadCloser
	closed chan struct{}
}

func (b *downloadTestBody) Close() error {
	err := b.ReadCloser.Close()
	close(b.closed)
	return err
}

func TestProfileDownloadReacquiresLockAndValidatesUnderLock(t *testing.T) {
	r, _, _ := storageFixture(t)
	r = downloadTestRuntime(r)
	b := newDownloadTestBarrier(t, "mode: global\n", 200)
	closed := make(chan struct{})
	r.HTTP = &http.Client{Transport: downloadTestTransport{base: http.DefaultTransport, closed: closed}}
	// The fake core blocks on a FIFO so the test controls validation precisely.
	enteredPath, releasePath := filepath.Join(r.Root, "entered"), filepath.Join(r.Root, "release")
	for _, path := range []string{enteredPath, releasePath} {
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	entered, err := os.OpenFile(enteredPath, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer entered.Close()
	release, err := os.OpenFile(releasePath, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer release.Close()
	// Root is generated by TempDir and contains no shell metacharacters.
	storageWriteFixture(t, r.CoreBinary(), "#!/bin/sh\nprintf x > '"+enteredPath+"'\nread answer < '"+releasePath+"'\n", 0700)
	done := startDownloadTest(r, "download", b.server.URL, "remote.yaml")
	downloadTestSignal(t, b.started)
	lock, err := base.AcquireServiceLock(r.RunDir(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	b.resume()
	downloadTestSignal(t, closed)
	select {
	case err := <-done:
		t.Fatalf("commit escaped second lock: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if _, err := os.Stat(storageProfilePath(r, "remote.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("committed before second lock")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	validating := make(chan struct{})
	go func() { var token [1]byte; _, _ = io.ReadFull(entered, token[:]); close(validating) }()
	downloadTestSignal(t, validating)
	// No second holder can enter while validprofile is running.
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatalf("validation does not hold service.lock: %v", err)
	}
	if _, err := release.WriteString("continue\n"); err != nil {
		t.Fatal(err)
	}
	if err := downloadTestResult(t, done); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	storageNoTemps(t, r)
}
