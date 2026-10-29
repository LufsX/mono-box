package storage

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func storageB64(value string) string { return base64.StdEncoding.EncodeToString([]byte(value)) }

func storageWriteFixture(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func storageFixture(t *testing.T) (*Runtime, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	out, errOut := new(bytes.Buffer), new(bytes.Buffer)
	r := &Runtime{Runtime: &base.Runtime{Root: root, ModuleDir: filepath.Join(root, "modules", "mono_box_lite"),
		Config: base.DefaultConfig(), Out: out, Err: errOut}}
	storageWriteFixture(t, filepath.Join(root, "scripts", "box.config"), "bin_name='mihomo'\nclash_api_port=9090\n", 0600)
	storageWriteFixture(t, filepath.Join(r.ModuleDir, "module.prop"), "id=mono_box_lite\nname=Mono Box Lite\n", 0644)
	storageWriteFixture(t, filepath.Join(r.CoreDir(), "config.yaml"), "mode: rule\n", 0600)
	// Check the real CLI contract and read the exact descriptor passed by Go.
	storageWriteFixture(t, r.CoreBinary(), "#!/bin/sh\nset -eu\n"+
		"[ \"$#\" = 5 ] && [ \"$1\" = -t ] && [ \"$2\" = -d ] && [ -d \"$3\" ] && [ \"$4\" = -f ]\n"+
		"content=$(cat \"$5\")\ncase \"$content\" in *INVALID*) printf 'subscription-secret' >&2; printf 'config-secret'; exit 1;; esac\n"+
		"[ -n \"$content\" ]\n", 0700)
	return r, out, errOut
}

func storageRun(t *testing.T, r *Runtime, args ...string) string {
	t.Helper()
	out := new(bytes.Buffer)
	r.Out = out
	if err := r.Storage(args); err != nil {
		t.Fatalf("%v: %v", args[:2], err)
	}
	return out.String()
}

func storageRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func storageProfilePath(r *Runtime, name string) string {
	return filepath.Join(r.CoreDir(), ".mono-box", "profiles", name)
}

func storageMetaPath(r *Runtime, name string) string {
	return filepath.Join(r.CoreDir(), ".mono-box", name)
}

func storageNoTemps(t *testing.T, r *Runtime) {
	t.Helper()
	err := filepath.WalkDir(r.Root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(entry.Name(), ".monobox-") {
			t.Errorf("leaked staging file: %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStorageValidProfileNames(t *testing.T) {
	for _, name := range []string{"config.yaml", "中文 专线.yml", "O'Brien $(literal).YAML", ".hidden.yaml"} {
		if !base.ValidProfileName(name) {
			t.Errorf("rejected %q", name)
		}
	}
	for _, name := range []string{"", ".", "..", "../escape.yaml", "/tmp/a.yaml", "a/b.yaml", "a\\b.yml",
		"a..yaml", "x\ty.yaml", "x\ny.yaml", "x\ry.yaml", "x\x00y.yaml", " a.yaml", "a.yaml ", "a.txt",
		"a:*?.yaml", strings.Repeat("a", 241) + ".yaml", string([]byte{0xff}) + ".yaml"} {
		if base.ValidProfileName(name) {
			t.Errorf("accepted %q", name)
		}
	}
}

func TestStorageRawConfigAndModuleInfo(t *testing.T) {
	r, _, _ := storageFixture(t)
	original := "# 原始文本\r\nclash_api_secret='literal $()'\r\n\r\n"
	storageWriteFixture(t, filepath.Join(r.Root, "scripts", "box.config"), original, 0600)
	if got := storageRun(t, r, "config", "read"); got != original {
		t.Fatalf("raw config changed: %q", got)
	}
	want := storageRead(t, filepath.Join(r.ModuleDir, "module.prop"))
	if got := storageRun(t, r, "module", "info"); got != want {
		t.Fatalf("raw module changed: %q", got)
	}
}

func TestStorageConfigAtomicValidation(t *testing.T) {
	r, _, _ := storageFixture(t)
	path := filepath.Join(r.Root, "scripts", "box.config")
	before := storageRead(t, path)
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	for _, content := range []string{"clash_api_port=0\n", "clash_api_secret=secret;false\n", "not-an-assignment"} {
		if err := r.Storage([]string{"config", "write", storageB64(content)}); err == nil {
			t.Fatal("invalid config accepted")
		}
		if got := storageRead(t, path); got != before {
			t.Fatal("invalid write changed config")
		}
	}
	content := "clash_api_port=9091\n# 中文\nclash_api_secret='literal $(touch never)'\n"
	storageRun(t, r, "config", "write", storageB64(content))
	if got := storageRead(t, path); got != content {
		t.Fatalf("write changed text: %q", got)
	}
	oldBytes, _ := io.ReadAll(old)
	if string(oldBytes) != before {
		t.Fatal("write truncated original inode")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("unsafe permissions: %v", info.Mode())
	}
	storageNoTemps(t, r)
}

func TestStorageProfilesLifecycleAndTSV(t *testing.T) {
	r, _, _ := storageFixture(t)
	name := "中文 O'Brien $(literal).yaml"
	content := "mode: global\n# profile\n"
	row := storageRun(t, r, "profiles", "import", storageB64(name), storageB64(content))
	parts := strings.Split(strings.TrimSuffix(row, "\n"), "\t")
	if len(parts) != 7 || parts[0] != "profile" || parts[1] != name || parts[2] != storageProfilePath(r, name) ||
		parts[3] != fmt.Sprint(len(content)) || parts[5] != "" || parts[6] != "false" {
		t.Fatalf("bad TSV: %q", row)
	}
	storageRun(t, r, "profiles", "import", storageB64("a.yaml"), storageB64("mode: direct\n"))
	list := storageRun(t, r, "profiles", "list")
	lines := strings.Split(strings.TrimSuffix(list, "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "current\tconfig.yaml\t") ||
		!strings.HasSuffix(lines[0], "\ttrue") || !strings.HasPrefix(lines[1], "profile\ta.yaml\t") {
		t.Fatalf("bad list: %q", list)
	}
	row = storageRun(t, r, "profiles", "switch", storageB64(name), "profile")
	if !strings.HasSuffix(row, "\ttrue\n") {
		t.Fatal(row)
	}
	if got := storageRead(t, storageMetaPath(r, "active-profile")); got != name {
		t.Fatal(got)
	}
	if r.ActiveConfig() != storageProfilePath(r, name) {
		t.Fatal("runtime did not resolve active profile")
	}
	if err := r.Storage([]string{"profiles", "delete", storageB64(name)}); err == nil {
		t.Fatal("deleted active profile")
	}
	storageRun(t, r, "profiles", "switch", storageB64("config.yaml"), "current")
	if _, err := os.Stat(storageMetaPath(r, "active-profile")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	storageRun(t, r, "profiles", "delete", storageB64(name))
	if _, err := os.Stat(storageProfilePath(r, name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	storageNoTemps(t, r)
}

func TestStorageCoreFailureKeepsOriginalAndMetadata(t *testing.T) {
	r, out, errOut := storageFixture(t)
	name := "safe.yaml"
	path := storageProfilePath(r, name)
	content := "mode: global\n"
	storageRun(t, r, "profiles", "import", storageB64(name), storageB64(content))
	storageRun(t, r, "profiles", "switch", storageB64(name), "profile")
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	r.Out = out
	out.Reset()
	if err := r.Storage([]string{"profiles", "import", storageB64(name), storageB64("INVALID secret")}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("validation error: %v", err)
	}
	if storageRead(t, path) != content || storageRead(t, storageMetaPath(r, "active-profile")) != name {
		t.Fatal("failed validation changed profile or selection")
	}
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Fatal("core output or secrets leaked")
	}
	// Selection of an invalid default must leave the active profile in place.
	storageWriteFixture(t, filepath.Join(r.CoreDir(), "config.yaml"), "INVALID", 0600)
	if err := r.Storage([]string{"profiles", "switch", storageB64("config.yaml"), "current"}); err == nil {
		t.Fatal("switched to invalid default")
	}
	if storageRead(t, storageMetaPath(r, "active-profile")) != name {
		t.Fatal("failed switch lost selection")
	}
	replacement := "mode: direct\n"
	storageRun(t, r, "profiles", "import", storageB64(name), storageB64(replacement))
	data, _ := io.ReadAll(old)
	if string(data) != content || storageRead(t, path) != replacement {
		t.Fatal("profile replacement was not atomic")
	}
	storageNoTemps(t, r)
}

func TestStorageProfilesWithoutInstalledCore(t *testing.T) {
	for _, removeBinDir := range []bool{false, true} {
		t.Run(fmt.Sprint("remove-bin-dir=", removeBinDir), func(t *testing.T) {
			r, _, _ := storageFixture(t)
			if removeBinDir {
				if err := os.RemoveAll(filepath.Dir(r.CoreBinary())); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(r.CoreBinary()); err != nil {
				t.Fatal(err)
			}
			name := "before-core.yaml"
			storageRun(t, r, "profiles", "import", storageB64(name), storageB64("mode: rule\n"))
			storageRun(t, r, "profiles", "switch", storageB64(name), "profile")
			storageRun(t, r, "profiles", "switch", storageB64("config.yaml"), "current")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "mode: direct\n") }))
			defer server.Close()
			storageRun(t, r, "profiles", "download", storageB64(server.URL), storageB64(name))
			storageRun(t, r, "profiles", "update", storageB64(name))
			if storageRead(t, storageProfilePath(r, name)) != "mode: direct\n" {
				t.Fatal("config management requires an installed core")
			}
		})
	}
}

func TestStorageDownloadUpdateAndSourceLayout(t *testing.T) {
	r, _, _ := storageFixture(t)
	var version atomic.Int32
	var invalid atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("token") != "private-secret" {
			t.Error("lost source query")
		}
		if invalid.Load() {
			fmt.Fprint(w, "INVALID subscription-secret")
			return
		}
		fmt.Fprintf(w, "mode: rule\n# version %d\n", version.Load())
	}))
	defer server.Close()
	source := server.URL + "/sub?token=private-secret"
	name := "remote.yaml"
	row := storageRun(t, r, "profiles", "download", storageB64(source), storageB64(name))
	parts := strings.Split(strings.TrimSuffix(row, "\n"), "\t")
	if parts[5] != storageB64(source) {
		t.Fatalf("bad source TSV: %q", row)
	}
	sourcePath := storageMetaPath(r, filepath.Join("sources", name+".url"))
	if storageRead(t, sourcePath) != source {
		t.Fatal("source layout changed")
	}
	storageRun(t, r, "profiles", "switch", storageB64(name), "profile")
	version.Store(2)
	storageRun(t, r, "profiles", "update", storageB64(name))
	before := storageRead(t, storageProfilePath(r, name))
	if !strings.Contains(before, "version 2") || storageRead(t, storageMetaPath(r, "active-profile")) != name {
		t.Fatal("update lost content or selection")
	}
	invalid.Store(true)
	if err := r.Storage([]string{"profiles", "update", storageB64(name)}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("invalid update error: %v", err)
	}
	if storageRead(t, storageProfilePath(r, name)) != before || storageRead(t, sourcePath) != source {
		t.Fatal("failed update changed storage")
	}
	storageRun(t, r, "profiles", "import", storageB64(name), storageB64("mode: direct\n"))
	if _, err := os.Stat(sourcePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("import kept obsolete source")
	}
	if err := r.Storage([]string{"profiles", "update", storageB64(name)}); err == nil {
		t.Fatal("updated profile without source")
	}
	storageRun(t, r, "profiles", "switch", storageB64("config.yaml"), "current")
	invalid.Store(false)
	storageRun(t, r, "profiles", "download", storageB64(server.URL+"/sub?token=private-secret"), storageB64("other.yaml"))
	storageNoTemps(t, r)
}

func TestStorageDownloadErrorsAreBoundedAndPrivate(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		timeout time.Duration
	}{
		{"HTTP", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "body-secret", http.StatusForbidden) }, 0},
		{"empty", func(http.ResponseWriter, *http.Request) {}, 0},
		{"declared-size", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprint(storageContentLimit+1))
		}, 0},
		{"streamed-size", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			chunk := strings.Repeat("a", 1<<16)
			for sent := 0; sent <= storageContentLimit; sent += len(chunk) {
				if _, err := io.WriteString(w, chunk); err != nil {
					return
				}
			}
		}, 0},
		{"deadline", func(w http.ResponseWriter, req *http.Request) { <-req.Context().Done() }, 30 * time.Millisecond},
		{"redirect-loop", func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "/again?token=query-secret", http.StatusFound)
		}, 0},
		{"redirect-scheme", func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "file:///secret", http.StatusFound)
		}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, out, errOut := storageFixture(t)
			name := "kept.yaml"
			storageRun(t, r, "profiles", "import", storageB64(name), storageB64("mode: rule\n"))
			out.Reset()
			r.Out = out
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			r.HTTP = &http.Client{Timeout: tc.timeout}
			originalTimeout := r.HTTP.Timeout
			err := r.Storage([]string{"profiles", "download", storageB64(server.URL + "/sub?token=query-secret"), storageB64(name)})
			if err == nil {
				t.Fatal("accepted failed download")
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), server.URL) || out.Len() != 0 || errOut.Len() != 0 {
				t.Fatalf("download leaked secrets: %v, %q, %q", err, out, errOut)
			}
			if r.HTTP.Timeout != originalTimeout {
				t.Fatal("mutated injected HTTP client")
			}
			if got := storageRead(t, storageProfilePath(r, name)); got != "mode: rule\n" {
				t.Fatal("failed download replaced profile")
			}
			storageNoTemps(t, r)
		})
	}
}

func TestStorageRejectsMalformedCommandsAndEscapes(t *testing.T) {
	r, _, _ := storageFixture(t)
	commands := [][]string{
		nil, {"profiles"}, {"module", "info", "extra"}, {"config", "read", "extra"}, {"profiles", "unknown"},
		{"config", "write", "!"}, {"profiles", "import", storageB64("../escaped.yaml"), storageB64("mode: rule")},
		{"profiles", "import", storageB64("safe.yaml"), "!"}, {"profiles", "import", storageB64("safe.yaml"), ""},
		{"profiles", "download", storageB64("file:///tmp/secret"), storageB64("safe.yaml")},
		{"profiles", "switch", storageB64("safe.yaml"), storageB64("profile")},
		{"profiles", "switch", storageB64("wrong.yaml"), "current"},
		{"profiles", "delete", storageB64("missing.yaml")},
	}
	for i, args := range commands {
		if err := r.Storage(args); err == nil {
			t.Errorf("command %d accepted", i)
		}
	}
	storageNoTemps(t, r)
}

func TestStorageRejectsSymlinksAndSpecialFiles(t *testing.T) {
	tests := []struct {
		name, relative string
		args           []string
		directory      bool
	}{
		{"scripts", "scripts", []string{"config", "write", storageB64("clash_api_port=9091\n")}, true},
		{"config", "scripts/box.config", []string{"config", "read"}, false},
		{"config-write", "scripts/box.config", []string{"config", "write", storageB64("clash_api_port=9091\n")}, false},
		{"config-patch", "scripts/box.config", []string{"config", "patch", storageB64(`{"clash_api_port":"9191"}`)}, false},
		{"config-lock-write", "run/config.lock", []string{"config", "write", storageB64("clash_api_port=9091\n")}, false},
		{"config-lock-patch", "run/config.lock", []string{"config", "patch", storageB64(`{"clash_api_port":"9191"}`)}, false},
		{"core-dir", "mihomo", []string{"profiles", "list"}, true},
		{"metadata-dir", "mihomo/.mono-box", []string{"profiles", "list"}, true},
		{"profiles-dir", "mihomo/.mono-box/profiles", []string{"profiles", "list"}, true},
		{"sources-dir", "mihomo/.mono-box/sources", []string{"profiles", "list"}, true},
		{"active", "mihomo/.mono-box/active-profile", []string{"profiles", "delete", storageB64("safe.yaml")}, false},
		{"profile-read", "mihomo/.mono-box/profiles/safe.yaml", []string{"profiles", "switch", storageB64("safe.yaml"), "profile"}, false},
		{"profile-write", "mihomo/.mono-box/profiles/safe.yaml", []string{"profiles", "import", storageB64("safe.yaml"), storageB64("mode: global")}, false},
		{"profile-delete", "mihomo/.mono-box/profiles/safe.yaml", []string{"profiles", "delete", storageB64("safe.yaml")}, false},
		{"source", "mihomo/.mono-box/sources/safe.yaml.url", []string{"profiles", "update", storageB64("safe.yaml")}, false},
		{"default", "mihomo/config.yaml", []string{"profiles", "switch", storageB64("config.yaml"), "current"}, false},
		{"lock", "run/service.lock", []string{"profiles", "list"}, false},
		{"core-binary", "bin/mihomo", []string{"profiles", "import", storageB64("safe.yaml"), storageB64("mode: rule")}, false},
		{"module", "modules/mono_box_lite/module.prop", []string{"module", "info"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := storageFixture(t)
			storageRun(t, r, "profiles", "import", storageB64("safe.yaml"), storageB64("mode: rule"))
			target := filepath.Join(r.Root, filepath.FromSlash(tc.relative))
			if err := os.RemoveAll(target); err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(t.TempDir(), "outside")
			if tc.directory {
				if err := os.Mkdir(external, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				storageWriteFixture(t, external, "outside-secret", 0600)
			}
			if err := os.Symlink(external, target); err != nil {
				t.Fatal(err)
			}
			if err := r.Storage(tc.args); err == nil {
				t.Fatal("accepted symlink")
			}
			if !tc.directory && storageRead(t, external) != "outside-secret" {
				t.Fatal("modified outside file")
			}
		})
	}
	t.Run("fifo", func(t *testing.T) {
		r, _, _ := storageFixture(t)
		storageRun(t, r, "profiles", "list")
		if err := syscall.Mkfifo(storageProfilePath(r, "pipe.yaml"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := r.Storage([]string{"profiles", "list"}); err == nil {
			t.Fatal("accepted FIFO")
		}
	})
	t.Run("hardlink", func(t *testing.T) {
		r, _, _ := storageFixture(t)
		storageRun(t, r, "profiles", "list")
		if err := os.Link(filepath.Join(r.Root, "scripts", "box.config"), storageProfilePath(r, "link.yaml")); err != nil {
			t.Fatal(err)
		}
		if err := r.Storage([]string{"profiles", "list"}); err == nil {
			t.Fatal("accepted hardlink")
		}
	})
}

func TestStorageMutationsShareServiceFlock(t *testing.T) {
	for _, command := range []string{"import", "download", "update", "switch", "delete", "config-write"} {
		t.Run(command, func(t *testing.T) {
			r, _, _ := storageFixture(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "mode: rule\n") }))
			defer server.Close()
			name := "safe.yaml"
			storageRun(t, r, "profiles", "download", storageB64(server.URL), storageB64(name))
			args := []string{"profiles", command, storageB64(name)}
			switch command {
			case "import":
				args = append(args, storageB64("mode: global\n"))
			case "download":
				args = []string{"profiles", "download", storageB64(server.URL), storageB64(name)}
			case "switch":
				args = append(args, "profile")
			case "config-write":
				args = []string{"config", "write", storageB64("clash_api_port=9091\n")}
			}
			lock, err := os.OpenFile(filepath.Join(r.Root, "run", "service.lock"), os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
				t.Fatal(err)
			}
			defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
			done := make(chan error, 1)
			go func() { done <- r.Storage(args) }()
			select {
			case err := <-done:
				t.Fatalf("mutation escaped service lock: %v", err)
			case <-time.After(30 * time.Millisecond):
			}
			if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("mutation did not acquire released lock")
			}
		})
	}
}
