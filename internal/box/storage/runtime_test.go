package storage

import (
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestLegacyActiveProfileMarker(t *testing.T) {
	root := t.TempDir()
	runtime, _ := base.New(root, t.TempDir(), io.Discard, io.Discard)
	r := New(runtime)
	profile := filepath.Join(r.CoreDir(), ".mono-box/profiles/chosen.yaml")
	if err := base.AtomicWrite(profile, []byte("mode: rule\n"), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(r.CoreDir(), ".mono-box/active-profile")
	if err := base.AtomicWrite(marker, []byte("chosen.yaml\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := r.ActiveConfig(); got != profile {
		t.Fatal(got)
	}
	if err := r.Storage([]string{"profiles", "delete", base64.StdEncoding.EncodeToString([]byte("chosen.yaml"))}); err == nil {
		t.Fatal("deleted active profile")
	}
	if err := os.WriteFile(marker, []byte("../outside.yaml"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := r.ActiveConfig(); got != filepath.Join(r.CoreDir(), "config.yaml") {
		t.Fatal(got)
	}
	if err := r.Storage([]string{"profiles", "list"}); err != nil {
		t.Fatal(err)
	}
}
