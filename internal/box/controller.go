package box

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Remove only ErrStopped; preserve any joined PID cleanup or I/O error.
func controllerRunningError(err error) error {
	if err == nil || err == ErrStopped {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var remaining []error
		for _, child := range joined.Unwrap() {
			remaining = append(remaining, controllerRunningError(child))
		}
		return errors.Join(remaining...)
	}
	if errors.Is(err, ErrStopped) {
		if wrapped, ok := err.(interface{ Unwrap() error }); ok {
			return controllerRunningError(wrapped.Unwrap())
		}
	}
	return err
}

type controllerDeps struct {
	netPath  string
	interval time.Duration
	output   func(context.Context, string, ...string) ([]byte, error)
	ready    func(context.Context) (bool, error)
	service  func(*Runtime, string) error
	running  func(*Runtime) (int, error)
}

func defaultControllerDeps() controllerDeps {
	return controllerDeps{
		netPath: "/data/misc/net/rt_tables", interval: time.Second, output: androidOutput,
		ready: func(ctx context.Context) (bool, error) {
			return androidBootReady(ctx, "/sdcard/Android", "/data/system/packages.xml", androidOutput)
		},
		service: func(r *Runtime, action string) error { return r.Service(action) },
		running: func(r *Runtime) (int, error) { return r.RunningPID() },
	}
}

func androidBootReady(ctx context.Context, storage, packages string, output func(context.Context, string, ...string) ([]byte, error)) (bool, error) {
	data, err := output(ctx, "getprop", "sys.boot_completed")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(string(data)) != "1" {
		return false, nil
	}
	info, err := os.Stat(packages)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s is not a regular file", packages)
	}
	probe, err := os.CreateTemp(storage, ".monobox-unlock-")
	if err != nil {
		return false, nil
	} // Credential-encrypted storage may still be locked.
	name := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	return closeErr == nil && removeErr == nil, errors.Join(closeErr, removeErr)
}

func (r *Runtime) Controller(args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	return r.controller(ctx, args, defaultControllerDeps())
}

func (r *Runtime) controller(ctx context.Context, args []string, deps controllerDeps) error {
	if len(args) == 0 {
		return nil
	}
	event := args[0]
	if event == "boot" {
		if len(args) != 1 {
			return errors.New("boot expects no path arguments")
		}
		ticker := time.NewTicker(deps.interval)
		defer ticker.Stop()
		for {
			ready, err := deps.ready(ctx)
			if err != nil {
				return err
			}
			if ready {
				return r.bootController(deps)
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}
	if len(args) < 2 || len(args) > 3 {
		return errors.New("controller expects event, watched path, and optional filename")
	}
	if event == "" || strings.Trim(event, "acew0rmyndDMuox") != "" {
		return fmt.Errorf("invalid controller event %q", event)
	}
	dir := filepath.Clean(args[1])
	name := ""
	if len(args) == 3 {
		name = args[2]
	}
	lock, err := r.controllerLock(false)
	if err != nil {
		return err
	}
	defer unlockController(lock)
	current, err := r.reloadController()
	if err != nil {
		return err
	}
	if name == "disable" && dir == filepath.Clean(r.ModuleDir) && strings.ContainsAny(event, "dn") {
		if exists(filepath.Join(r.Root, "manual")) {
			return nil
		}
		// Combined rename/delete/create notifications are resolved from current state.
		action := "start"
		if exists(filepath.Join(r.ModuleDir, "disable")) {
			action = "stop"
		}
		return deps.service(current, action)
	}
	if strings.Contains(event, "w") && (dir == filepath.Clean(deps.netPath) || (name == "rt_tables" && filepath.Join(dir, name) == filepath.Clean(deps.netPath))) {
		return current.applyNetwork(ctx, deps)
	}
	return nil
}

func (r *Runtime) bootController(deps controllerDeps) error {
	lock, err := r.controllerLock(false)
	if err != nil {
		return err
	}
	defer unlockController(lock)
	current, err := r.reloadController()
	if err != nil {
		return err
	}
	if exists(filepath.Join(r.Root, "manual")) || exists(filepath.Join(r.ModuleDir, "disable")) {
		return nil
	}
	return deps.service(current, "start")
}

func (r *Runtime) controllerLock(nonblock bool) (*os.File, error) {
	return r.namedControllerLock("controller.lock", nonblock)
}
func (r *Runtime) namedControllerLock(name string, nonblock bool) (*os.File, error) {
	if err := os.MkdirAll(r.RunDir(), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(r.RunDir(), name), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	op := syscall.LOCK_EX
	if nonblock {
		op |= syscall.LOCK_NB
	}
	if err = syscall.Flock(int(f.Fd()), op); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func unlockController(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }

type watcherIdentity struct {
	PID        int    `json:"pid"`
	Started    string `json:"started"`
	Executable string `json:"executable"`
}

func processWatcherIdentity(pid int) (watcherIdentity, error) {
	if pid <= 1 {
		return watcherIdentity{}, errors.New("invalid watcher PID")
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return watcherIdentity{}, err
	}
	_, tail, ok := strings.Cut(string(stat), ") ")
	// comm can contain ") "; use its final closing parenthesis.
	if i := strings.LastIndex(string(stat), ") "); i >= 0 {
		tail = string(stat)[i+2:]
		ok = true
	}
	fields := strings.Fields(tail)
	if !ok || len(fields) < 20 {
		return watcherIdentity{}, errors.New("invalid process stat")
	}
	executable, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return watcherIdentity{}, err
	}
	return watcherIdentity{PID: pid, Started: fields[19], Executable: executable}, nil
}

// Watch polls files because Android may replace rt_tables and its parent during
// boot. It needs no inotifyd and keeps monitoring disable while boot is pending.
func (r *Runtime) Watch() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	return r.watch(ctx, defaultControllerDeps())
}

func (r *Runtime) watch(ctx context.Context, deps controllerDeps) (result error) {
	lock, err := r.namedControllerLock("watcher.lock", true)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errors.New("watcher already running")
	}
	if err != nil {
		return err
	}
	defer unlockController(lock)
	identity, err := processWatcherIdentity(os.Getpid())
	if err != nil {
		return err
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	pidPath := filepath.Join(r.RunDir(), "watcher.pid")
	if err = AtomicWrite(pidPath, data, 0600); err != nil {
		return err
	}
	defer func() {
		saved, readErr := os.ReadFile(pidPath)
		if readErr == nil && string(saved) == string(data) {
			result = errors.Join(result, os.Remove(pidPath))
		}
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	boot := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(deps.interval)
		defer ticker.Stop()
		for {
			ready, err := deps.ready(ctx)
			if err != nil || ready {
				select {
				case boot <- err:
				case <-ctx.Done():
					return
				}
				if ready && err == nil {
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	ticker := time.NewTicker(deps.interval)
	defer ticker.Stop()
	disabled := exists(filepath.Join(r.ModuleDir, "disable"))
	var network string
	bootReady, bootDone := false, false
	for {
		select {
		case <-ctx.Done():
			return nil
		case bootErr := <-boot:
			if bootErr != nil {
				r.controllerError(bootErr)
			} else {
				bootReady = true
			}
		case <-ticker.C:
		}
		// A fresh config is loaded for every service/network event.
		nowDisabled := exists(filepath.Join(r.ModuleDir, "disable"))
		if nowDisabled != disabled {
			if nowDisabled || bootReady {
				event := "d"
				if nowDisabled {
					event = "n"
				}
				if err = r.controller(ctx, []string{event, r.ModuleDir, "disable"}, deps); err != nil {
					r.controllerError(err)
				} else {
					disabled = nowDisabled
				}
			} else {
				disabled = nowDisabled
			}
		}
		if bootReady && !bootDone {
			if err = r.bootController(deps); err != nil {
				r.controllerError(err)
			} else {
				bootDone = true
			}
		}
		snapshot, readErr := networkSnapshot(deps.netPath)
		if readErr != nil {
			r.controllerError(readErr)
			continue
		}
		if snapshot == "missing" {
			network = snapshot
			continue
		}
		if snapshot != network && bootReady {
			if err = r.controller(ctx, []string{"w", deps.netPath}, deps); err != nil {
				r.controllerError(err)
			} else {
				network = snapshot
			}
		}
	}
}

// Include metadata and contents to detect rewrites with unchanged file length,
// atomic replacement, deletion and recreation.
func networkSnapshot(path string) (string, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var inode uint64
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		inode = stat.Ino
	}
	return strconv.FormatUint(inode, 10) + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + ":" + string(data), nil
}

func (r *Runtime) controllerError(err error) {
	out := r.Err
	if out == nil {
		out = io.Discard
	}
	fmt.Fprintf(out, "controller: %v\n", err)
}

// StopWatch validates both process start time and executable before signalling.
// The persistent flock file is never unlinked: replacing it would split locks.
func (r *Runtime) StopWatch() error {
	lock, err := r.namedControllerLock("watcher.lock", true)
	if err == nil {
		defer unlockController(lock)
		err = os.Remove(filepath.Join(r.RunDir(), "watcher.pid"))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		return err
	}
	data, err := os.ReadFile(filepath.Join(r.RunDir(), "watcher.pid"))
	if err != nil {
		return err
	}
	var saved watcherIdentity
	if err = json.Unmarshal(data, &saved); err != nil {
		return fmt.Errorf("invalid watcher PID file: %w", err)
	}
	actual, err := processWatcherIdentity(saved.PID)
	if err != nil {
		return fmt.Errorf("watcher identity: %w", err)
	}
	if saved != actual {
		return errors.New("watcher identity mismatch; refusing signal")
	}
	if err = syscall.Kill(saved.PID, syscall.SIGTERM); err != nil {
		return err
	}
	// A service transition may already hold the controller while checking the
	// core. Allow its bounded validation/lock timeouts to finish before giving up.
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		lock, err = r.namedControllerLock("watcher.lock", true)
		if err == nil {
			unlockController(lock)
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return errors.New("watcher did not stop within 60 seconds")
}
