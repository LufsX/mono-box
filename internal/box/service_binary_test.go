//go:build linux || android

package box

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServiceSystemBinaryPreference(t *testing.T) {
	dir := t.TempDir()
	system := filepath.Join(dir, "system", "mihomo")
	local := filepath.Join(dir, "box", "bin", "mihomo")
	serviceTestWrite(t, local, []byte("local"), 0700)
	if got := serviceSelectBinary(system, local); got != local {
		t.Fatalf("missing system binary: %s", got)
	}
	serviceTestWrite(t, system, []byte("system"), 0600)
	if got := serviceSelectBinary(system, local); got != local {
		t.Fatalf("non-executable system binary: %s", got)
	}
	serviceTestRequire(t, os.Chmod(system, 0555))
	if got := serviceSelectBinary(system, local); got != system {
		t.Fatalf("system binary not preferred: %s", got)
	}
	linked := filepath.Join(dir, "system-link")
	serviceTestRequire(t, os.Symlink(system, linked))
	if got := serviceSelectBinary(linked, local); got != system {
		t.Fatalf("system link not resolved: %s", got)
	}
	// System binaries may live on a read-only partition and require no local bin/.
	root := filepath.Join(dir, "runtime")
	serviceTestRequire(t, os.MkdirAll(root, 0700))
	loc := serviceLocation{root: root, core: filepath.Join(root, "mihomo"), binary: system, run: filepath.Join(root, "run")}
	serviceTestRequire(t, servicePreparePermissions(loc, nil))
	info, err := os.Stat(system)
	serviceTestRequire(t, err)
	if info.Mode().Perm() != 0555 {
		t.Fatalf("changed system binary permissions: %o", info.Mode().Perm())
	}
}
