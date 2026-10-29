package storage

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestPatchPreservesExternalEditsAndComments(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "scripts/box.config")
	if err := base.AtomicWrite(path, []byte("clash_api_port=9090\nclash_api_secret=\"old\" # keep\ntarget_wifi='external'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime, err := base.New(root, t.TempDir(), io.Discard, io.Discard)
	r := New(runtime)
	if err != nil {
		t.Fatal(err)
	}
	latest := "clash_api_port=9191\nclash_api_secret=\"old\" # keep\ntarget_wifi='external edit'\n"
	if err := os.WriteFile(path, []byte(latest), 0600); err != nil {
		t.Fatal(err)
	}
	updates := map[string]string{"clash_api_secret": "\"new \\\"quote\\\" \\$dollar\""}
	data, _ := json.Marshal(updates)
	if err := r.PatchConfig(base64.StdEncoding.EncodeToString(data)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "clash_api_port=9191\nclash_api_secret=\"new \\\"quote\\\" \\$dollar\" # keep\ntarget_wifi='external edit'\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	c, err := base.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.APISecret != "new \"quote\" $dollar" || c.TargetWiFi != "external edit" {
		t.Fatalf("%+v", c)
	}
}

func TestInvalidPatchDoesNotModifyFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "scripts/box.config")
	original := "clash_api_port=9090\n"
	if err := base.AtomicWrite(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	runtime, _ := base.New(root, t.TempDir(), io.Discard, io.Discard)
	r := New(runtime)
	for _, patch := range []map[string]string{
		{"clash_api_port": "99999"}, {"clash_api_secret": "\"unterminated"}, {"target_wifi": "foo\nclash_api_port=1"}, {"unknown": "value"},
	} {
		data, _ := json.Marshal(patch)
		if err := r.PatchConfig(base64.StdEncoding.EncodeToString(data)); err == nil {
			t.Fatalf("accepted %+v", patch)
		}
		got, _ := os.ReadFile(path)
		if string(got) != original {
			t.Fatalf("changed: %s", got)
		}
	}
}

func TestConfigWriteAndPatchSizeLimit(t *testing.T) {
	r, _, _ := storageFixture(t)
	path := filepath.Join(r.Root, "scripts", "box.config")
	const limit = 16 << 20
	header := "clash_api_port=9090\n"
	padding := limit - len(header)
	content := header + strings.Repeat("# padding\n", padding/10) + strings.Repeat("\n", padding%10)
	storageRun(t, r, "config", "write", storageB64(content))
	if got := storageRun(t, r, "config", "read"); got != content {
		t.Fatal("config at size limit did not round trip")
	}
	// Replacing a value without growing the file is valid at exactly 16 MiB.
	storageRun(t, r, "config", "patch", storageB64(`{"clash_api_port":"9191"}`))
	want := strings.Replace(content, "9090", "9191", 1)
	if got := storageRead(t, path); got != want {
		t.Fatal("patch at size limit produced incorrect config")
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, command, data string
	}{
		{"write", "write", content + "\n"},
		{"merged-patch", "patch", `{"target_wifi":"'new'"}`},
		{"encoded-patch", "patch", strings.Repeat(" ", limit) + "{}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := r.Storage([]string{"config", tc.command, storageB64(tc.data)}); err == nil {
				t.Fatal("oversized config operation accepted")
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || storageRead(t, path) != want {
				t.Fatal("rejected operation replaced or modified the original file")
			}
		})
	}
	// An oversized on-disk file must also be rejected by read and patch.
	storageWriteFixture(t, path, content+"\n", 0600)
	if err := r.Storage([]string{"config", "read"}); err == nil {
		t.Fatal("oversized on-disk config read accepted")
	}
	if err := r.PatchConfig(storageB64(`{"clash_api_port":"9292"}`)); err == nil {
		t.Fatal("oversized on-disk config patch accepted")
	}
	if storageRead(t, path) != content+"\n" {
		t.Fatal("patch modified oversized on-disk config")
	}
	storageNoTemps(t, r)
}

func TestMergeConfigLastDuplicateAndQuotedComments(t *testing.T) {
	got := mergeConfigPatch("# header\nclash_api_secret='first'\nexport clash_api_secret=\"abc#def\" # keep\n", map[string]string{"clash_api_secret": "'new'", "target_wifi": "'wifi'"})
	want := "# header\nclash_api_secret='first'\nexport clash_api_secret='new' # keep\ntarget_wifi='wifi'\n"
	if got != want {
		t.Fatalf("%q", got)
	}
}
