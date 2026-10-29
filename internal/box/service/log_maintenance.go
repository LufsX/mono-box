//go:build linux || android

package service

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

const (
	logHistoryLimit = 5
	logSizeLimit    = 4 << 20
	logTailSize     = 2 << 20
	logErrorLimit   = 1 << 20
	logErrorTail    = 512 << 10
	logScanLimit    = 512
	logDeleteLimit  = 64
	logWorkTime     = 250 * time.Millisecond
)

// MaintainLogs attempts maintenance once without waiting for lifecycle work.
// Each core directory retains five root-level logs, including open inodes.
// Active output and run diagnostics precede bounded, incremental history work.
// Contention is a successful no-op; inspection, filesystem and lock errors are
// returned to the caller for rate-limited reporting. Config is reloaded under
// the lock, without mutating the shared Runtime.
func (r *Runtime) MaintainLogs() error {
	current, loc, lock, err := r.serviceLockedRuntime(0)
	if errors.Is(err, base.ErrServiceLockTimeout) {
		return nil
	}
	if err != nil {
		return err
	}
	defer lock.Close()
	return current.maintainLogsLocked(loc)
}

type logProcess struct {
	loc serviceLocation
	id  serviceIdentity
}

// A live but unverified PID is not evidence that its logs are inactive.
func logsMaintenanceProcess(loc serviceLocation) (*logProcess, error) {
	id, err := serviceRunning(loc)
	if err == nil {
		return &logProcess{loc: loc, id: id}, nil
	}
	if !errors.Is(err, ErrStopped) {
		return nil, err
	}
	data, err := serviceReadSmall(loc.pid)
	if errors.Is(err, os.ErrNotExist) {
		if _, err = serviceReadSmall(loc.pid + ".identity"); !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("cannot inspect logs with orphaned process identity: %s", loc.pid)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return nil, fmt.Errorf("cannot inspect logs with invalid PID: %s", loc.pid)
	}
	if _, err = serviceProcIdentity(pid); errors.Is(err, ErrStopped) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("cannot inspect logs of unverified process %d", pid)
}

// Values distinguish append-safe open inodes from legacy open inodes. Every
// open regular inode is protected against unlink, regardless of access mode.
func logsMaintenanceActive(processes []logProcess) (map[[2]uint64]bool, error) {
	active := make(map[[2]uint64]bool)
	for _, p := range processes {
		if err := serviceVerifyProcess(p.loc, p.id); err != nil {
			return nil, err
		}
		dir := fmt.Sprintf("/proc/%d", p.id.PID)
		fds, err := os.ReadDir(dir + "/fd")
		if err != nil {
			return nil, err
		}
		for _, fd := range fds {
			path := dir + "/fd/" + fd.Name()
			info, err := os.Stat(path)
			if errors.Is(err, os.ErrNotExist) {
				continue // A descriptor was closed, before any mutation.
			}
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				continue
			}
			data, err := os.ReadFile(dir + "/fdinfo/" + fd.Name())
			if err != nil {
				return nil, fmt.Errorf("read core fdinfo: %w", err)
			}
			flags, err := logsFDFlags(data)
			if err != nil {
				return nil, err
			}
			// Check that fdinfo still describes the inode we inspected.
			after, err := os.Stat(path)
			if err != nil || !os.SameFile(info, after) {
				return nil, errors.New("core log descriptor changed during inspection")
			}
			stat := info.Sys().(*syscall.Stat_t)
			key := [2]uint64{uint64(stat.Dev), stat.Ino}
			appendSafe := flags&syscall.O_APPEND != 0
			if previous, exists := active[key]; exists {
				appendSafe = appendSafe && previous
			}
			active[key] = appendSafe
		}
		if err := serviceVerifyProcess(p.loc, p.id); err != nil {
			return nil, err
		}
	}
	return active, nil
}

func logsFDFlags(data []byte) (uint64, error) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "flags:" {
			flags, err := strconv.ParseUint(fields[1], 8, 64)
			if err == nil {
				return flags, nil
			}
			return 0, fmt.Errorf("invalid core fdinfo flags: %w", err)
		}
	}
	return 0, errors.New("core fdinfo has no flags")
}

// maintainLogsLocked is also used by lifecycle operations that own service.lock.
func (r *Runtime) maintainLogsLocked(loc serviceLocation) error {
	return r.maintainLogsKeepingLocked(loc, "")
}

func (r *Runtime) maintainLogsWarning(loc serviceLocation, keep string) {
	if err := r.maintainLogsKeepingLocked(loc, keep); err != nil {
		r.serviceMessage("Warn", "Log maintenance: %v", err)
	}
}

// The just-created stdout is pinned even if an older file has a future mtime.
func (r *Runtime) maintainLogsKeepingLocked(loc serviceLocation, keep string) error {
	type directory struct {
		r   *Runtime
		loc serviceLocation
	}
	deadline := time.Now().Add(logWorkTime)
	var dirs []directory
	var processes []logProcess
	seen := make(map[string]bool)
	for _, name := range []string{filepath.Base(loc.core), "mihomo", "mihomo_smart"} {
		if seen[name] {
			continue
		}
		seen[name] = true
		current := New(r.Runtime.WithConfig(r.Config))
		current.Config.BinName = name
		other, err := current.serviceLocation()
		if err != nil {
			return err
		}
		// Inspect process records even if its directory has disappeared.
		p, err := logsMaintenanceProcess(other)
		if err != nil {
			return err
		}
		if p != nil {
			processes = append(processes, *p)
		}
		dirs = append(dirs, directory{current, other})
	}
	active, err := logsMaintenanceActive(processes)
	if err != nil {
		return err
	}
	// Active output and run diagnostics have a fixed, small workload and take
	// priority over history, even when scanning a backlog exhausts its budget.
	result := logsMaintainActiveOutput(processes)
	paths := []string{filepath.Join(loc.run, "check.log")}
	for _, dir := range dirs {
		paths = append(paths, filepath.Join(loc.run, "error_"+dir.r.Config.BinName+".log"))
	}
	for _, path := range paths {
		e, err := logsMaintenanceEntry(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err == nil {
			err = logsTrimEntry(e, logErrorLimit, logErrorTail, processes)
		}
		result = errors.Join(result, err)
	}
	deleted := 0
	for _, dir := range dirs {
		entries, complete, err := dir.r.logsMaintenanceCollect(dir.loc, deadline)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].mtime.Equal(entries[j].mtime) {
				return entries[i].path > entries[j].path
			}
			return entries[i].mtime.After(entries[j].mtime)
		})
		retained := make(map[[2]uint64]bool)
		for _, e := range entries {
			key := [2]uint64{e.device, e.inode}
			if _, open := active[key]; open || e.path == keep {
				retained[key] = true
			}
		}
		for _, e := range entries {
			if len(retained) >= logHistoryLimit {
				break
			}
			retained[[2]uint64{e.device, e.inode}] = true
		}
		pending := !complete
		// Delete excess history before reading any large retained history.
		for _, e := range entries {
			key := [2]uint64{e.device, e.inode}
			if retained[key] {
				continue
			}
			if deleted >= logDeleteLimit || time.Now().After(deadline) {
				pending = true
				continue
			}
			// A core can reopen an old path between timer ticks.
			current, err := logsMaintenanceActive(processes)
			if err != nil {
				return errors.Join(result, err)
			}
			if _, open := current[key]; !open {
				err = logsRemoveEntry(e)
				result = errors.Join(result, err)
				deleted++
				pending = pending || err != nil
			}
		}
		// Entries below a partial inventory's top five cannot be in the
		// global top five. Defer history copytruncate until inventory is full.
		if !pending {
			for _, e := range entries {
				if time.Now().After(deadline) {
					break
				}
				key := [2]uint64{e.device, e.inode}
				if _, open := active[key]; retained[key] && !open {
					result = errors.Join(result, logsTrimEntry(e, logSizeLimit, logTailSize, processes))
				}
			}
		}
	}
	return result
}

// Locate stdout/stderr directly instead of depending on directory inventory.
// Only module-generated paths are eligible; /proc links are used for identity,
// never as writable paths.
func logsMaintainActiveOutput(processes []logProcess) error {
	var result error
	for _, p := range processes {
		for _, fd := range []int{1, 2} {
			if err := serviceVerifyProcess(p.loc, p.id); err != nil {
				return errors.Join(result, err)
			}
			procPath := fmt.Sprintf("/proc/%d/fd/%d", p.id.PID, fd)
			info, err := os.Stat(procPath)
			if err != nil {
				return errors.Join(result, err)
			}
			if !info.Mode().IsRegular() {
				continue
			}
			path, err := os.Readlink(procPath)
			if err != nil {
				return errors.Join(result, err)
			}
			limit, tail := int64(logSizeLimit), int64(logTailSize)
			if path == filepath.Join(p.loc.run, "error_"+filepath.Base(p.loc.core)+".log") {
				limit, tail = logErrorLimit, logErrorTail
			} else if filepath.Dir(path) != p.loc.core ||
				!strings.HasPrefix(filepath.Base(path), filepath.Base(p.loc.core)+"_") ||
				!strings.HasSuffix(path, ".log") {
				continue
			}
			e, err := logsMaintenanceEntry(path)
			if err == nil {
				stat := info.Sys().(*syscall.Stat_t)
				if e.device != uint64(stat.Dev) || e.inode != stat.Ino {
					err = errors.New("active log pathname no longer matches core descriptor")
				} else {
					err = logsTrimEntry(e, limit, tail, processes)
				}
			}
			result = errors.Join(result, err)
		}
	}
	return result
}

func logsMaintenanceEntry(path string) (logsEntry, error) {
	f, err := base.OpenRegular(path, os.O_RDONLY, 0)
	if err != nil {
		return logsEntry{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return logsEntry{}, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	return logsEntry{path: path, size: info.Size(), device: uint64(stat.Dev), inode: stat.Ino, mtime: info.ModTime()}, nil
}

// Periodic copytruncate keeps the inode and bounds memory use. Writes racing
// the tail snapshot and truncate can lose a small window of output.
func logsTrimEntry(entry logsEntry, limit, tail int64, processes []logProcess) error {
	f, err := base.OpenRegular(entry.path, os.O_RDWR, 0)
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
		return errors.New("log changed during maintenance")
	}
	if info.Size() <= limit {
		return nil
	}
	active, err := logsMaintenanceActive(processes)
	if err != nil {
		return err
	}
	appendSafe, open := active[[2]uint64{entry.device, entry.inode}]
	if open && !appendSafe {
		return nil
	}
	data := make([]byte, tail)
	if _, err = f.ReadAt(data, info.Size()-tail); err != nil {
		return err
	}
	// Prefer complete lines, but retain an unterminated tail too.
	if i := bytes.IndexByte(data, '\n'); i >= 0 && i+1 < len(data) {
		data = data[i+1:]
	}
	active, err = logsMaintenanceActive(processes)
	if err != nil {
		return err
	}
	appendSafe, open = active[[2]uint64{entry.device, entry.inode}]
	if open && !appendSafe {
		return nil
	}
	// Reopen safely to check the pathname and link count just before writing.
	check, err := logsMaintenanceEntry(entry.path)
	if err != nil {
		return err
	}
	if check.device != entry.device || check.inode != entry.inode {
		return errors.New("log replaced during maintenance")
	}
	if n, err := f.WriteAt(data, 0); err != nil {
		return err
	} else if n != len(data) {
		return io.ErrShortWrite
	}
	if err = f.Truncate(int64(len(data))); err != nil {
		return err
	}
	if !open {
		// Shrinking history must not make it newer than the startup diagnostic.
		times := []syscall.Timeval{syscall.NsecToTimeval(stat.Atim.Nano()), syscall.NsecToTimeval(info.ModTime().UnixNano())}
		return syscall.Futimes(int(f.Fd()), times)
	}
	return nil
}
