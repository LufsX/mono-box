//go:build linux || android

package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServicePreservesSystemBinaryPermissions(t *testing.T) {
	dir := t.TempDir()
	system := filepath.Join(dir, "system", "mihomo")
	serviceTestWrite(t, system, []byte("system"), 0600)
	serviceTestRequire(t, os.Chmod(system, 0555))
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
