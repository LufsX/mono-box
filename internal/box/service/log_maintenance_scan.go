//go:build linux || android

package service

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

// Automatic maintenance covers the root where the module creates stdout logs.
// Manual inventory/ClearLogs retain their recursive protocol. Batches bound
// directory memory and permit incremental cleanup of a historical backlog.
func (r *Runtime) logsMaintenanceCollect(loc serviceLocation, deadline time.Time) ([]logsEntry, bool, error) {
	var entries []logsEntry
	dir, err := base.OpenDirectory(loc.core, false)
	if errors.Is(err, os.ErrNotExist) {
		return entries, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer dir.Close()
	for {
		if len(entries) >= logScanLimit || time.Now().After(deadline) {
			return entries, false, nil
		}
		children, err := dir.ReadDir(128)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, false, err
		}
		for _, child := range children {
			if len(entries) >= logScanLimit || time.Now().After(deadline) {
				return entries, false, nil
			}
			if !child.Type().IsRegular() || !r.logsNameAllowed(child.Name()) {
				continue
			}
			f, err := base.OpenRegularAt(dir, child.Name(), os.O_RDONLY, 0)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, false, err
			}
			info, err := f.Stat()
			f.Close()
			if err != nil {
				return nil, false, err
			}
			stat := info.Sys().(*syscall.Stat_t)
			entries = append(entries, logsEntry{path: filepath.Join(loc.core, child.Name()), size: info.Size(), device: uint64(stat.Dev), inode: stat.Ino, mtime: info.ModTime()})
		}
		if errors.Is(err, io.EOF) {
			return entries, true, nil
		}
	}
}
