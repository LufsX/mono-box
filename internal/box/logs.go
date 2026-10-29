//go:build linux || android

package box

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type logsEntry struct {
	path   string
	size   int64
	device uint64
	inode  uint64
}

func (r *Runtime) logsWriter() io.Writer {
	if r.Out == nil {
		return io.Discard
	}
	return r.Out
}

func (r *Runtime) logsNameAllowed(name string) bool {
	return strings.HasPrefix(name, r.Config.BinName+"_") && strings.HasSuffix(name, ".log") &&
		!strings.ContainsAny(name, "/\\\x00\r\n\t")
}

func (r *Runtime) logsOpen(path string) (*os.File, error) {
	current, loc, lock, err := r.serviceLockedRuntime(serviceLockTimeout)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	r = current
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("log path must be an absolute, clean path")
	}
	rel, err := filepath.Rel(loc.core, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || !r.logsNameAllowed(filepath.Base(path)) {
		return nil, errors.New("log path is outside the core log directory")
	}
	return serviceOpenRegular(path, os.O_RDONLY, 0)
}

func (r *Runtime) logsCollect(loc serviceLocation) ([]logsEntry, error) {
	var entries []logsEntry
	var visit func(string) error
	visit = func(path string) error {
		dir, err := serviceOpenDirectory(path, false)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		defer dir.Close()
		children, err := dir.ReadDir(-1)
		if err != nil {
			return err
		}
		for _, child := range children {
			if child.Type()&os.ModeSymlink != 0 {
				continue
			}
			path := filepath.Join(path, child.Name())
			if child.IsDir() {
				if err := visit(path); err != nil {
					return err
				}
				continue
			}
			if !r.logsNameAllowed(child.Name()) || !child.Type().IsRegular() {
				continue
			}
			f, err := serviceOpenRegularAt(dir, child.Name(), os.O_RDONLY, 0)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			info, err := f.Stat()
			f.Close()
			if err != nil {
				return err
			}
			stat := info.Sys().(*syscall.Stat_t)
			entries = append(entries, logsEntry{path: path, size: info.Size(), device: uint64(stat.Dev), inode: stat.Ino})
		}
		return nil
	}
	if err := visit(loc.core); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	return entries, nil
}

// LogsSize writes the existing shell protocol: file<TAB>bytes<TAB>path,
// followed by total<TAB>bytes<TAB>count. Only core logs are included.
func (r *Runtime) LogsSize() error {
	current, loc, lock, err := r.serviceLockedRuntime(serviceLockTimeout)
	if err != nil {
		return err
	}
	defer lock.Close()
	r = current
	entries, err := r.logsCollect(loc)
	if err != nil {
		return err
	}
	var total int64
	for _, entry := range entries {
		total += entry.size
		if _, err = fmt.Fprintf(r.logsWriter(), "file\t%d\t%s\n", entry.size, entry.path); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(r.logsWriter(), "total\t%d\t%d\n", total, len(entries))
	return err
}

func logsActiveFiles(loc serviceLocation) (map[[2]uint64]bool, bool, error) {
	active := make(map[[2]uint64]bool)
	id, err := serviceRunning(loc)
	if errors.Is(err, ErrStopped) {
		return active, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	fds, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", id.PID))
	if errors.Is(err, os.ErrNotExist) {
		return active, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("inspect active log descriptors: %w", err)
	}
	for _, fd := range fds {
		info, err := os.Stat(fmt.Sprintf("/proc/%d/fd/%s", id.PID, fd.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, true, err
		}
		if info.Mode().IsRegular() {
			stat := info.Sys().(*syscall.Stat_t)
			active[[2]uint64{uint64(stat.Dev), stat.Ino}] = true
		}
	}
	if err := serviceVerifyProcess(loc, id); err != nil {
		if errors.Is(err, ErrStopped) {
			return make(map[[2]uint64]bool), false, nil
		}
		return nil, true, err
	}
	return active, true, nil
}

// ClearLogs preserves every log inode still open by the verified core.
// Failure to inspect the live process aborts cleanup before deleting any file.
func (r *Runtime) ClearLogs() error {
	current, loc, lock, err := r.serviceLockedRuntime(serviceLockTimeout)
	if err != nil {
		return err
	}
	defer lock.Close()
	r = current
	entries, err := r.logsCollect(loc)
	if err != nil {
		return err
	}
	active, running, err := logsActiveFiles(loc)
	if err != nil {
		return err
	}
	matched := false
	for _, entry := range entries {
		if active[[2]uint64{entry.device, entry.inode}] {
			matched = true
		}
	}
	// Keep the legacy newest-file fallback if stdout cannot be matched.
	if running && !matched && len(entries) > 0 {
		entry := entries[len(entries)-1]
		active[[2]uint64{entry.device, entry.inode}] = true
	}
	var total int64
	count, failed, skipped := 0, 0, 0
	var result error
	for _, entry := range entries {
		state := "cleared"
		if active[[2]uint64{entry.device, entry.inode}] {
			state = "skipped"
			skipped++
		} else {
			err := logsRemoveEntry(entry)
			if err != nil {
				state = "failed"
				failed++
				result = errors.Join(result, err)
			} else {
				total += entry.size
				count++
			}
		}
		if _, err := fmt.Fprintf(r.logsWriter(), "file\t%s\t%d\t%s\n", state, entry.size, entry.path); err != nil {
			return errors.Join(result, err)
		}
	}
	_, err = fmt.Fprintf(r.logsWriter(), "cleared\t%d\t%d\t%d\t%d\n", total, count, failed, skipped)
	return errors.Join(result, err)
}

func logsRemoveEntry(entry logsEntry) error {
	dir, err := serviceOpenDirectory(filepath.Dir(entry.path), false)
	if err != nil {
		return err
	}
	defer dir.Close()
	f, err := serviceOpenRegularAt(dir, filepath.Base(entry.path), os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	stat := info.Sys().(*syscall.Stat_t)
	if uint64(stat.Dev) != entry.device || stat.Ino != entry.inode {
		return errors.New("log changed during cleanup")
	}
	return syscall.Unlinkat(int(dir.Fd()), filepath.Base(entry.path))
}

// ReadLog copies a snapshot of a permitted regular log to Runtime.Out.
func (r *Runtime) ReadLog(path string) error {
	f, err := r.logsOpen(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	_, err = io.Copy(r.logsWriter(), io.LimitReader(f, info.Size()))
	return err
}

// OpenLog exports a snapshot to Android's shared Downloads directory and opens
// that directory, falling back to the exported file's text/plain intent.
func (r *Runtime) OpenLog(path string) error {
	// Validate first, even on a host without Android's activity manager.
	f, err := r.logsOpen(path)
	if err != nil {
		return err
	}
	f.Close()
	const am = "/system/bin/am"
	if info, err := os.Stat(am); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return errors.New("Android activity manager is unavailable")
	}
	shared, err := filepath.EvalSymlinks("/sdcard")
	if err != nil {
		return err
	}
	return r.logsOpenAt(path, filepath.Join(shared, "Download", "MonoBox", "logs"), am, 10*time.Second)
}

func (r *Runtime) logsOpenAt(path, shareDir, am string, timeout time.Duration) error {
	f, err := r.logsOpen(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	dir, err := serviceOpenDirectoryMode(shareDir, true, 0755)
	if err != nil {
		return err
	}
	defer dir.Close()
	name := filepath.Base(path)
	if existing, err := serviceOpenRegularAt(dir, name, os.O_RDONLY, 0); err == nil {
		existing.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Join("/proc/self/fd", strconv.Itoa(int(dir.Fd()))), ".log-export-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, io.LimitReader(f, info.Size()))
	if err == nil {
		err = tmp.Chmod(0644)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = syscall.Renameat(int(dir.Fd()), filepath.Base(tmp.Name()), int(dir.Fd()), name); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	const uri = "content://com.android.externalstorage.documents/document/primary%3ADownload%2FMonoBox%2Flogs"
	open := func(target, mime string, grant bool) error {
		args := []string{"start", "-a", "android.intent.action.VIEW", "-d", target, "-t", mime}
		if grant {
			args = append(args, "--grant-read-uri-permission")
		}
		cmd := exec.CommandContext(ctx, am, args...)
		cmd.WaitDelay = time.Second
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("open exported log: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}
	if err = open(uri, "vnd.android.document/directory", false); err != nil {
		// Encode the entire document ID, including the log name's punctuation.
		target := "content://com.android.externalstorage.documents/document/" + logsURIEncode("primary:Download/MonoBox/logs/"+name)
		if err = open(target, "text/plain", true); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(r.logsWriter(), "opened\t%s\n", filepath.Join(shareDir, name))
	return err
}

func logsURIEncode(value string) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	for _, b := range []byte(value) {
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("-._~", rune(b)) {
			out.WriteByte(b)
		} else {
			out.WriteByte('%')
			out.WriteByte(hex[b>>4])
			out.WriteByte(hex[b&15])
		}
	}
	return out.String()
}
