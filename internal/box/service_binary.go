//go:build linux || android

package box

import (
	"os"
	"path/filepath"
)

// Keep system-installed cores ahead of the module's downloaded binary.
func (r *Runtime) serviceBinary(root string) string {
	return serviceSelectBinary(filepath.Join("/system/bin", r.Config.BinName), filepath.Join(root, "bin", r.Config.BinName))
}

func serviceSelectBinary(system, local string) string {
	if info, err := os.Stat(system); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
		if resolved, err := filepath.EvalSymlinks(system); err == nil {
			return resolved
		}
	}
	return local
}
