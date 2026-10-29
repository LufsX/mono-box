//go:build linux || android

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

// ErrStopped means that no verified core process is running.
var ErrStopped = errors.New("core is stopped")

var errServiceArgumentsPending = fmt.Errorf("core command line is not ready: %w", ErrStopped)

const (
	serviceCheckTimeout = 15 * time.Second
	serviceStartGrace   = 400 * time.Millisecond
	serviceStopTimeout  = 3 * time.Second
	serviceKillTimeout  = 2 * time.Second
)

type serviceLocation struct {
	root, core, binary, run, pid string
}

type serviceIdentity struct {
	PID    int
	Start  string
	Device uint64
	Inode  uint64
	Binary string
}

func (r *Runtime) serviceLocation() (serviceLocation, error) {
	if r.Root == "" {
		return serviceLocation{}, errors.New("empty runtime root")
	}
	name := r.Config.BinName
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00\r\n\t") {
		return serviceLocation{}, errors.New("invalid core name")
	}
	root, err := filepath.Abs(r.Root)
	if err != nil {
		return serviceLocation{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return serviceLocation{}, err
	}
	return serviceLocation{root: root, core: filepath.Join(root, name), binary: r.CoreBinaryAt(root),
		run: filepath.Join(root, "run"), pid: filepath.Join(root, "run", name+".pid")}, nil
}

func serviceReadSmall(path string) ([]byte, error) {
	return serviceReadLimited(path, 64*1024)
}

func serviceReadLimited(path string, limit int64) ([]byte, error) {
	f, err := base.OpenRegular(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file too large: %s", path)
	}
	return data, err
}

func serviceAtomicAt(dir *os.File, name string, data []byte, mode os.FileMode) error {
	// base.AtomicWrite operates through the pinned directory, including its temp file.
	return base.AtomicWrite(filepath.Join("/proc/self/fd", strconv.Itoa(int(dir.Fd())), name), data, mode)
}

// Service serializes lifecycle changes with log cleanup, including across CLI processes.
func (r *Runtime) Service(action string) error {
	switch action {
	case "start", "stop", "restart", "status":
	default:
		return fmt.Errorf("unknown service action %q", action)
	}
	current, loc, lock, err := r.serviceLockedRuntime(base.ServiceLockTimeout)
	if err != nil {
		return err
	}
	r = current
	defer lock.Close() // flock is released on close or process exit; never unlink this file.
	switch action {
	case "start":
		return r.serviceStart(loc)
	case "stop":
		return r.serviceStop(loc)
	case "restart":
		if err := r.serviceStop(loc); err != nil {
			return err
		}
		return r.serviceStart(loc)
	default:
		id, err := serviceRunning(loc)
		if errors.Is(err, ErrStopped) {
			err = errors.Join(err, serviceRemovePID(loc), r.serviceModuleStatus("Stopped"))
			r.serviceMessage("Warn", "%s is stopped.", r.Config.BinName)
			return err
		}
		if err != nil {
			return err
		}
		if err := r.serviceModuleStatus("Running"); err != nil {
			return err
		}
		r.serviceMessage("Info", "%s is running. ( PID: %d )", r.Config.BinName, id.PID)
		return nil
	}
}

// RunningPID returns only a process whose executable, arguments and start identity match.
func (r *Runtime) RunningPID() (int, error) {
	_, loc, lock, err := r.serviceLockedRuntime(base.ServiceLockTimeout)
	if err != nil {
		return 0, err
	}
	defer lock.Close()
	id, err := serviceRunning(loc)
	if errors.Is(err, ErrStopped) {
		return 0, errors.Join(err, serviceRemovePID(loc))
	}
	return id.PID, err
}

func serviceProcessMissing(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

func serviceProcIdentity(pid int) (serviceIdentity, error) {
	if pid <= 1 {
		return serviceIdentity{}, ErrStopped
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if serviceProcessMissing(err) {
		return serviceIdentity{}, ErrStopped
	}
	if err != nil {
		return serviceIdentity{}, err
	}
	// comm can contain spaces and parentheses. Fields after its last ')' start at state.
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return serviceIdentity{}, errors.New("malformed process stat")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return serviceIdentity{}, errors.New("short process stat")
	}
	if fields[0] == "Z" || fields[0] == "X" {
		return serviceIdentity{}, fmt.Errorf("core process %d exited (state %s): %w", pid, fields[0], ErrStopped)
	}
	info, err := os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
	if serviceProcessMissing(err) {
		return serviceIdentity{}, ErrStopped
	}
	if err != nil {
		return serviceIdentity{}, err
	}
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if serviceProcessMissing(err) {
		return serviceIdentity{}, ErrStopped
	}
	if err != nil {
		return serviceIdentity{}, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	return serviceIdentity{PID: pid, Start: fields[19], Device: uint64(stat.Dev), Inode: stat.Ino, Binary: strings.TrimSuffix(exe, " (deleted)")}, nil
}

func serviceVerifyProcess(loc serviceLocation, id serviceIdentity) error {
	current, err := serviceProcIdentity(id.PID)
	if err != nil {
		return err
	}
	if current != id {
		return fmt.Errorf("core process identity changed (PID %d, executable %q -> %q, start %s -> %s): %w", id.PID, id.Binary, current.Binary, id.Start, current.Start, ErrStopped)
	}
	argsData, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", id.PID))
	if serviceProcessMissing(err) {
		return ErrStopped
	}
	if err != nil {
		return err
	}
	argsErr := serviceVerifyArguments(loc, argsData)
	// Catch exit/reuse while the remaining /proc files were read.
	after, err := serviceProcIdentity(id.PID)
	if err != nil {
		return err
	}
	if after != id {
		return fmt.Errorf("core process identity changed while reading arguments (PID %d): %w", id.PID, ErrStopped)
	}
	return argsErr
}

func serviceVerifyArguments(loc serviceLocation, argsData []byte) error {
	if len(argsData) == 0 {
		return fmt.Errorf("empty core argument vector (0 bytes): %w", errServiceArgumentsPending)
	}
	args := strings.Split(strings.TrimSuffix(string(argsData), "\x00"), "\x00")
	if len(args) != 3 && len(args) != 5 {
		return fmt.Errorf("unexpected core argument count %d (expected 3 or 5, %d bytes): %w", len(args), len(argsData), ErrStopped)
	}
	if args[1] != "-d" || args[2] != loc.core {
		return fmt.Errorf("core data directory or arguments do not match %q: %w", loc.core, ErrStopped)
	}
	if len(args) == 5 {
		if args[3] != "-f" {
			return fmt.Errorf("core configuration flag does not match -f: %w", ErrStopped)
		}
		config := args[4]
		defaultConfig := filepath.Join(loc.core, "config.yaml")
		profiles := filepath.Join(loc.core, ".mono-box", "profiles")
		if filepath.Clean(config) != config || (config != defaultConfig && (filepath.Dir(config) != profiles || !base.ValidProfileName(filepath.Base(config)))) {
			return fmt.Errorf("core configuration path does not match the active core directory: %w", ErrStopped)
		}
	}
	return nil
}

func serviceRunning(loc serviceLocation) (serviceIdentity, error) {
	data, err := serviceReadSmall(loc.pid)
	if errors.Is(err, os.ErrNotExist) {
		return serviceIdentity{}, ErrStopped
	}
	if err != nil {
		return serviceIdentity{}, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return serviceIdentity{}, ErrStopped
	}
	id, err := serviceProcIdentity(pid)
	if err != nil {
		return serviceIdentity{}, err
	}
	if err = serviceVerifyProcess(loc, id); err != nil {
		return serviceIdentity{}, err
	}
	record, err := serviceReadSmall(loc.pid + ".identity")
	if err == nil {
		var saved serviceIdentity
		if json.Unmarshal(record, &saved) != nil || saved != id {
			return serviceIdentity{}, ErrStopped
		}
	} else if errors.Is(err, os.ErrNotExist) {
		// Adopt legacy numeric PID files only after verifying the current binary too.
		if id.Binary != loc.binary {
			return serviceIdentity{}, ErrStopped
		}
		f, e := base.OpenRegular(loc.binary, os.O_RDONLY, 0)
		if e != nil {
			return serviceIdentity{}, e
		}
		info, e := f.Stat()
		f.Close()
		if e != nil {
			return serviceIdentity{}, e
		}
		stat := info.Sys().(*syscall.Stat_t)
		if uint64(stat.Dev) != id.Device || stat.Ino != id.Inode {
			return serviceIdentity{}, ErrStopped
		}
	} else {
		return serviceIdentity{}, err
	}
	if err = serviceVerifyProcess(loc, id); err != nil {
		return serviceIdentity{}, err
	}
	return id, nil
}

func serviceSavePID(loc serviceLocation, id serviceIdentity) error {
	dir, err := base.OpenDirectory(loc.run, false)
	if err != nil {
		return err
	}
	defer dir.Close()
	data, err := json.Marshal(id)
	if err != nil {
		return err
	}
	if err = serviceAtomicAt(dir, filepath.Base(loc.pid)+".identity", data, 0600); err != nil {
		return err
	}
	return serviceAtomicAt(dir, filepath.Base(loc.pid), []byte(strconv.Itoa(id.PID)+"\n"), 0600)
}

func serviceRemovePID(loc serviceLocation) error {
	dir, err := base.OpenDirectory(loc.run, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	var result error
	for _, name := range []string{filepath.Base(loc.pid), filepath.Base(loc.pid) + ".identity"} {
		err := syscall.Unlinkat(int(dir.Fd()), name)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("remove %s: %w", filepath.Join(loc.run, name), err))
		}
	}
	return result
}

func serviceResolveCredential(value string) (*syscall.Credential, error) {
	if value == "" {
		value = "root:net_admin"
	}
	u, g, ok := strings.Cut(value, ":")
	if !ok || strings.Contains(g, ":") {
		return nil, errors.New("CORE_USER_GROUP must be user:group")
	}
	if u == "" {
		u = "root"
	}
	if g == "" {
		g = "net_admin"
	}
	resolve := func(name string, group bool) (uint32, error) {
		if name == "root" {
			return 0, nil
		}
		if group && name == "net_admin" {
			return 3005, nil
		}
		id, err := strconv.ParseUint(name, 10, 32)
		if err == nil {
			return uint32(id), nil
		}
		android := map[string]uint32{"system": 1000, "radio": 1001, "bluetooth": 1002, "wifi": 1010, "dhcp": 1014, "vpn": 1016, "shell": 2000, "nobody": 9999}
		if group {
			for k, v := range map[string]uint32{"inet": 3003, "net_raw": 3004, "net_admin": 3005, "net_bw_stats": 3006, "net_bw_acct": 3007, "readproc": 3009} {
				android[k] = v
			}
		}
		if id, ok := android[name]; ok {
			return id, nil
		}
		file := "/etc/passwd"
		if group {
			file = "/etc/group"
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return 0, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Split(line, ":")
			if len(fields) >= 3 && fields[0] == name {
				id, err := strconv.ParseUint(fields[2], 10, 32)
				return uint32(id), err
			}
		}
		return 0, fmt.Errorf("unknown account %q", name)
	}
	uid, err := resolve(u, false)
	if err != nil {
		return nil, fmt.Errorf("resolve core user: %w", err)
	}
	gid, err := resolve(g, true)
	if err != nil {
		return nil, fmt.Errorf("resolve core group: %w", err)
	}
	if os.Geteuid() != 0 {
		if uid != uint32(os.Geteuid()) || gid != uint32(os.Getegid()) {
			return nil, errors.New("root is required to switch core user/group")
		}
		return nil, nil
	}
	return &syscall.Credential{Uid: uid, Gid: gid, Groups: []uint32{gid}}, nil
}

func servicePreparePermissions(loc serviceLocation, cred *syscall.Credential) error {
	core, err := base.OpenDirectory(loc.core, true)
	if err != nil {
		return err
	}
	core.Close()
	binary, err := base.OpenRegular(loc.binary, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("missing or unsafe core: %w", err)
	}
	defer binary.Close()
	uid, gid := os.Geteuid(), os.Getegid()
	if cred != nil {
		uid, gid = int(cred.Uid), int(cred.Gid)
	}
	localBinary := filepath.Join(loc.root, "bin", filepath.Base(loc.binary)) == loc.binary
	if os.Geteuid() == 0 {
		// The controller retains ownership of run/ and its lock/PID records.
		directories := []string{loc.root}
		if localBinary {
			directories = append(directories, filepath.Join(loc.root, "bin"))
		}
		for _, path := range directories {
			dir, e := base.OpenDirectory(path, false)
			if e != nil {
				return e
			}
			e = dir.Chown(os.Geteuid(), os.Getegid())
			if e == nil {
				e = dir.Chmod(0755)
			}
			dir.Close()
			if e != nil {
				return e
			}
		}
		if localBinary {
			if err := binary.Chown(uid, gid); err != nil {
				return err
			}
		}
	}
	if localBinary {
		if err := binary.Chmod(0700); err != nil {
			return err
		}
	}
	return filepath.WalkDir(loc.core, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		var f *os.File
		var err error
		if entry.IsDir() {
			f, err = base.OpenDirectory(path, false)
		} else {
			f, err = base.OpenRegular(path, os.O_RDONLY, 0)
		}
		if err != nil {
			return err
		}
		defer f.Close()
		if os.Geteuid() == 0 {
			if err := f.Chown(uid, gid); err != nil {
				return err
			}
		}
		if entry.IsDir() {
			return f.Chmod(0750)
		}
		return f.Chmod(0640)
	})
}

func (r *Runtime) serviceConfig(loc serviceLocation) (string, error) {
	// ActiveConfig is shared with storage. Recheck containment and every directory
	// component here even if storage has already validated the selected profile.
	path, err := filepath.Abs(r.ActiveConfig())
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(r.Root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, path)
	if err != nil {
		return "", err
	}
	path = filepath.Join(loc.root, rel)
	defaultPath := filepath.Join(loc.core, "config.yaml")
	profiles := filepath.Join(loc.core, ".mono-box", "profiles")
	if path != defaultPath && (filepath.Dir(path) != profiles || !base.ValidProfileName(filepath.Base(path))) {
		return "", errors.New("configuration is outside the core profile directory")
	}
	// Reject an active-profile symlink even when ActiveConfig falls back to default.
	marker := filepath.Join(loc.core, ".mono-box", "active-profile")
	if data, err := serviceReadSmall(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	} else if err == nil && base.ValidProfileName(strings.TrimRight(string(data), "\r\n")) {
		selected := filepath.Join(profiles, strings.TrimRight(string(data), "\r\n"))
		if f, err := base.OpenRegular(selected, os.O_RDONLY, 0); err == nil {
			f.Close()
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	f, err := base.OpenRegular(path, os.O_RDONLY, 0)
	if err != nil {
		return "", fmt.Errorf("missing or unsafe configuration: %w", err)
	}
	f.Close()
	return path, nil
}

func serviceCoreArgs(loc serviceLocation, config string) []string {
	return []string{"-d", loc.core, "-f", config}
}

func serviceValidateConfig(loc serviceLocation, config string, cred *syscall.Credential, timeout time.Duration) error {
	log, err := base.OpenRegular(filepath.Join(loc.run, "check.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, loc.binary, append([]string{"-t"}, serviceCoreArgs(loc, config)...)...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = loc.core, log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Credential: cred}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		reason := "failed"
		if ctx.Err() != nil {
			reason, err = "timed out", ctx.Err()
		}
		return serviceStartupError(fmt.Errorf("configuration check %s; see %s: %w", reason, log.Name(), err), log.Name())
	}
	return nil
}

func (r *Runtime) serviceStart(loc serviceLocation) (result error) {
	r.maintainLogsWarning(loc, "")
	var startupLog string
	defer func() { r.maintainLogsWarning(loc, startupLog) }()
	if id, err := serviceRunning(loc); err == nil {
		if err := r.serviceModuleStatus("Running"); err != nil {
			return err
		}
		r.serviceMessage("Info", "%s is already running. ( PID: %d )", r.Config.BinName, id.PID)
		return nil
	} else if !errors.Is(err, ErrStopped) {
		return err
	}
	if err := serviceRemovePID(loc); err != nil {
		return err
	}
	if err := r.serviceModuleStatus("Starting"); err != nil {
		return err
	}
	defer func() {
		if result != nil {
			result = errors.Join(result, r.serviceModuleStatus("Error"))
		}
	}()
	cred, err := serviceResolveCredential(r.Config.CoreUserGroup)
	if err != nil {
		return err
	}
	if err = servicePreparePermissions(loc, cred); err != nil {
		return err
	}
	config, err := r.serviceConfig(loc)
	if err != nil {
		return err
	}
	if err = serviceValidateConfig(loc, config, cred, serviceCheckTimeout); err != nil {
		return err
	}
	stdout, err := base.OpenRegular(filepath.Join(loc.core, r.Config.BinName+"_"+time.Now().Format("20060102150405.000000000")+".log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return err
	}
	defer stdout.Close()
	startupLog = stdout.Name()
	stderr, err := base.OpenRegular(filepath.Join(loc.run, "error_"+r.Config.BinName+".log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer stderr.Close()
	cmd := exec.Command(loc.binary, serviceCoreArgs(loc, config)...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = loc.core, stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Credential: cred}
	if err = cmd.Start(); err != nil {
		return err
	}
	// Keep PID cleanup and diagnostics together, before log FDs close.
	defer func() {
		if result != nil {
			result = serviceStartupError(errors.Join(result, serviceRemovePID(loc)), stderr.Name(), stdout.Name())
		}
	}()
	startupDeadline := time.Now().Add(serviceStartGrace)
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()
	// Wait reaps children when embedded in a long-lived process; the CLI can exit
	// immediately after this method and the new session keeps its own log FDs.
	abort := func(cause error) error {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(serviceKillTimeout):
		}
		return cause
	}
	id, err := serviceProcIdentity(cmd.Process.Pid)
	if err != nil {
		return abort(fmt.Errorf("core exited during startup; see %s: %w", stderr.Name(), err))
	}
	if id.Binary != loc.binary {
		return abort(errors.New("unexpected core executable"))
	}
	if err = serviceAwaitStartupArguments(startupDeadline, done, func() error { return serviceVerifyProcess(loc, id) }); err != nil {
		return abort(fmt.Errorf("core identity check failed during startup (initial check); see %s: %w", stderr.Name(), err))
	}
	select {
	case err := <-done:
		return fmt.Errorf("core exited during startup (%v); see %s", err, stderr.Name())
	case <-time.After(max(time.Duration(0), time.Until(startupDeadline))):
	}
	if err = serviceVerifyProcess(loc, id); err != nil {
		return abort(fmt.Errorf("core identity check failed during startup (after grace period); see %s: %w", stderr.Name(), err))
	}
	if err = serviceSavePID(loc, id); err != nil {
		return abort(err)
	}
	if err = r.serviceModuleStatus("Running"); err != nil {
		return abort(err)
	}
	r.serviceMessage("Info", "%s started. ( PID: %d )", r.Config.BinName, id.PID)
	return nil
}

func serviceWaitStopped(loc serviceLocation, id serviceIdentity, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := serviceVerifyProcess(loc, id)
		if errors.Is(err, ErrStopped) {
			return nil
		}
		if err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for core to stop")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (r *Runtime) serviceStop(loc serviceLocation) (result error) {
	defer func() {
		if result == nil {
			r.maintainLogsWarning(loc, "")
		}
	}()
	id, err := serviceRunning(loc)
	if errors.Is(err, ErrStopped) {
		return errors.Join(serviceRemovePID(loc), r.serviceModuleStatus("Stopped"))
	}
	if err != nil {
		return err
	}
	// FindProcess pins the process with a pidfd where supported by Go/kernel.
	// Recheck identity after obtaining that handle and before each signal.
	process, err := os.FindProcess(id.PID)
	if err != nil {
		return err
	}
	defer process.Release()
	signal := func(sig syscall.Signal) error {
		if err := serviceVerifyProcess(loc, id); err != nil {
			return err
		}
		return process.Signal(sig)
	}
	if err = signal(syscall.SIGTERM); err != nil && !errors.Is(err, ErrStopped) && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	err = serviceWaitStopped(loc, id, serviceStopTimeout)
	if err != nil {
		if e := signal(syscall.SIGKILL); e != nil && !errors.Is(e, ErrStopped) && !errors.Is(e, os.ErrProcessDone) {
			return e
		}
		if err = serviceWaitStopped(loc, id, serviceKillTimeout); err != nil {
			return errors.Join(err, r.serviceModuleStatus("Running"))
		}
	}
	if err := errors.Join(serviceRemovePID(loc), r.serviceModuleStatus("Stopped")); err != nil {
		return err
	}
	r.serviceMessage("Info", "%s stopped.", r.Config.BinName)
	return nil
}

func (r *Runtime) serviceMessage(level, format string, args ...any) {
	if r.Err != nil {
		fmt.Fprintf(r.Err, "%s [%s]: %s\n", time.Now().Format("2006-01-02 15:04:05"), level, fmt.Sprintf(format, args...))
	}
}

func (r *Runtime) serviceModuleStatus(state string) (result error) {
	defer func() {
		if result != nil {
			result = fmt.Errorf("update module.prop status to %s: %w", state, result)
		}
	}()
	if r.ModuleDir == "" {
		return nil
	}
	module, err := filepath.Abs(r.ModuleDir)
	if err != nil {
		return err
	}
	dir, err := base.OpenDirectory(module, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	f, err := base.OpenRegularAt(dir, "module.prop", os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	f.Close()
	if err != nil {
		return err
	}
	if len(data) > 64*1024 {
		return errors.New("module.prop is too large")
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	description := "description=Use mihomo on Android devices. [" + state + "]"
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, "description=") {
			lines[i] = description
			found = true
		}
	}
	if !found {
		lines = append(lines, description)
	}
	return serviceAtomicAt(dir, "module.prop", []byte(strings.Join(lines, "\n")+"\n"), info.Mode().Perm())
}
