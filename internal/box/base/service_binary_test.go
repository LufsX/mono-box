//go:build linux || android

package base

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServiceSystemBinaryPreference(t *testing.T) {
	dir := t.TempDir()
	system := filepath.Join(dir, "system", "mihomo")
	local := filepath.Join(dir, "box", "bin", "mihomo")
	if err := os.MkdirAll(filepath.Dir(local), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("local"), 0700); err != nil {
		t.Fatal(err)
	}
	if got := serviceSelectBinary(system, local); got != local {
		t.Fatalf("missing system binary: %s", got)
	}
	if err := os.MkdirAll(filepath.Dir(system), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(system, []byte("system"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := serviceSelectBinary(system, local); got != local {
		t.Fatalf("non-executable system binary: %s", got)
	}
	if err := os.Chmod(system, 0555); err != nil {
		t.Fatal(err)
	}
	if got := serviceSelectBinary(system, local); got != system {
		t.Fatalf("system binary not preferred: %s", got)
	}
	linked := filepath.Join(dir, "system-link")
	if err := os.Symlink(system, linked); err != nil {
		t.Fatal(err)
	}
	if got := serviceSelectBinary(linked, local); got != system {
		t.Fatalf("system link not resolved: %s", got)
	}
}
