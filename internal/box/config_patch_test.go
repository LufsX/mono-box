package box

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPatchPreservesExternalEditsAndComments(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "scripts/box.config")
	if err := AtomicWrite(path, []byte("clash_api_port=9090\nclash_api_secret=\"old\" # keep\ntarget_wifi='external'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := New(root, t.TempDir(), io.Discard, io.Discard)
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
	c, err := LoadConfig(path)
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
	if err := AtomicWrite(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	r, _ := New(root, t.TempDir(), io.Discard, io.Discard)
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

func TestMergeConfigLastDuplicateAndQuotedComments(t *testing.T) {
	got := mergeConfigPatch("# header\nclash_api_secret='first'\nexport clash_api_secret=\"abc#def\" # keep\n", map[string]string{"clash_api_secret": "'new'", "target_wifi": "'wifi'"})
	want := "# header\nclash_api_secret='first'\nexport clash_api_secret='new' # keep\ntarget_wifi='wifi'\n"
	if got != want {
		t.Fatalf("%q", got)
	}
}
