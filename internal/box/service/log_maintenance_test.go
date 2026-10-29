//go:build linux || android

package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func maintenanceLogs(t *testing.T, r *Runtime) []logsEntry {
	t.Helper()
	loc, err := r.serviceLocation()
	serviceTestRequire(t, err)
	entries, err := r.logsCollect(loc)
	serviceTestRequire(t, err)
	return entries
}

func maintenanceFill(t *testing.T, path string, size int) {
	t.Helper()
	serviceTestWrite(t, path, bytes.Repeat([]byte("x"), size), 0600)
}

func TestMaintainLogsFailedStartsKeepDiagnostics(t *testing.T) {
	r := serviceTestRuntime(t)
	serviceTestWrite(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte("exit"), 0600)
	// Future mtimes must not evict the log for this failed attempt.
	for i := 0; i < 5; i++ {
		path := filepath.Join(r.CoreDir(), fmt.Sprintf("mihomo_future%d.log", i))
		serviceTestWrite(t, path, []byte("history"), 0600)
		future := time.Now().Add(time.Hour)
		serviceTestRequire(t, os.Chtimes(path, future, future))
	}
	for i := 0; i < 8; i++ {
		err := r.Service("start")
		if err == nil || !strings.Contains(err.Error(), "fake stdout") || !strings.Contains(err.Error(), "fake stderr") {
			t.Fatalf("attempt %d lost startup diagnostics: %v", i, err)
		}
		entries := maintenanceLogs(t, r)
		if len(entries) != logHistoryLimit {
			t.Fatalf("attempt %d retained %d logs", i, len(entries))
		}
		// The startup error names the exact file that must still exist.
		found := false
		for _, e := range entries {
			if strings.Contains(err.Error(), e.path) {
				data, readErr := os.ReadFile(e.path)
				serviceTestRequire(t, readErr)
				found = bytes.Contains(data, []byte("fake stdout"))
			}
		}
		if !found {
			t.Fatalf("attempt %d deleted its stdout: %v", i, err)
		}
		if _, err := os.Stat(r.PIDFile()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed start retained pid: %v", err)
		}
		serviceTestState(t, r, "Error")
	}
}

func TestMaintainLogsHistoryMtimeAndTail(t *testing.T) {
	r := serviceTestRuntime(t)
	now := time.Now().Add(-time.Hour).Truncate(time.Second)
	for i := 0; i < 9; i++ {
		// Reverse lexical order relative to mtime.
		path := filepath.Join(r.CoreDir(), fmt.Sprintf("mihomo_%d.log", 9-i))
		maintenanceFill(t, path, logSizeLimit+1024)
		mtime := now.Add(time.Duration(i) * time.Second)
		serviceTestRequire(t, os.Chtimes(path, mtime, mtime))
	}
	check := filepath.Join(r.RunDir(), "check.log")
	stderr := filepath.Join(r.RunDir(), "error_mihomo.log")
	for _, path := range []string{check, stderr} {
		maintenanceFill(t, path, logErrorLimit+1)
	}
	unrelated := filepath.Join(r.CoreDir(), "cache.db")
	maintenanceFill(t, unrelated, logSizeLimit+1)
	serviceTestRequire(t, r.MaintainLogs())
	entries := maintenanceLogs(t, r)
	if len(entries) != logHistoryLimit {
		t.Fatalf("retained %d logs", len(entries))
	}
	for _, e := range entries {
		if e.size != logTailSize || e.mtime.Before(now.Add(4*time.Second)) {
			t.Fatalf("wrong history retained: %+v", e)
		}
		data, err := os.ReadFile(e.path)
		serviceTestRequire(t, err)
		if !bytes.Equal(data, bytes.Repeat([]byte("x"), logTailSize)) {
			t.Fatal("tail contents changed")
		}
	}
	for _, path := range []string{check, stderr} {
		info, err := os.Stat(path)
		serviceTestRequire(t, err)
		if info.Size() != logErrorTail {
			t.Fatalf("diagnostic size %d", info.Size())
		}
	}
	info, err := os.Stat(unrelated)
	serviceTestRequire(t, err)
	if info.Size() != logSizeLimit+1 {
		t.Fatal("unrelated file changed")
	}
	// Restoring history mtime keeps retention stable on the next tick.
	serviceTestRequire(t, r.MaintainLogs())
	for _, e := range maintenanceLogs(t, r) {
		if e.mtime.Before(now.Add(4 * time.Second)) {
			t.Fatal("copytruncate changed history ordering")
		}
	}
}

func TestMaintainLogsReloadAndPreviousCore(t *testing.T) {
	r := serviceTestRuntime(t)
	for _, name := range []string{"nextcore", "mihomo", "mihomo_smart", "unrelated"} {
		for i := 0; i < 7; i++ {
			serviceTestWrite(t, filepath.Join(r.Root, name, fmt.Sprintf("%s_%d.log", name, i)), []byte("log"), 0600)
		}
	}
	serviceTestWrite(t, filepath.Join(r.Root, "scripts", "box.config"), []byte("bin_name=nextcore\n"), 0600)
	serviceTestRequire(t, r.MaintainLogs())
	for _, name := range []string{"nextcore", "mihomo", "mihomo_smart", "unrelated"} {
		files, err := filepath.Glob(filepath.Join(r.Root, name, name+"_*.log"))
		serviceTestRequire(t, err)
		want := logHistoryLimit
		if name == "unrelated" {
			want = 7
		}
		if len(files) != want {
			t.Fatalf("%s: %d files, want %d", name, len(files), want)
		}
	}
	if r.Config.BinName != "mihomo" {
		t.Fatal("maintenance mutated shared config")
	}
	// Missing alternate core directories must not be created.
	serviceTestRequire(t, os.RemoveAll(filepath.Join(r.Root, "mihomo_smart")))
	serviceTestRequire(t, r.MaintainLogs())
	if _, err := os.Stat(filepath.Join(r.Root, "mihomo_smart")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("maintenance created an alternate directory: %v", err)
	}
}

func TestMaintainLogsActiveAppendAndContinuedWrites(t *testing.T) {
	r := serviceTestRuntime(t)
	serviceTestRequire(t, r.Service("start"))
	pid, err := r.RunningPID()
	serviceTestRequire(t, err)
	paths := make(map[int]string)
	before := make(map[int]os.FileInfo)
	for _, fd := range []int{1, 2} {
		path, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%d", pid, fd))
		serviceTestRequire(t, err)
		flags, err := os.ReadFile(fmt.Sprintf("/proc/%d/fdinfo/%d", pid, fd))
		serviceTestRequire(t, err)
		value, err := logsFDFlags(flags)
		serviceTestRequire(t, err)
		if value&syscall.O_APPEND == 0 {
			t.Fatalf("fd %d lacks O_APPEND: %s", fd, flags)
		}
		// Rename stdout and make it older than every retained history candidate.
		if fd == 1 {
			renamed := filepath.Join(r.CoreDir(), "mihomo_active-old.log")
			serviceTestRequire(t, os.Rename(path, renamed))
			path = renamed
		}
		f, err := base.OpenRegular(path, os.O_WRONLY|os.O_APPEND, 0)
		serviceTestRequire(t, err)
		_, err = f.Write(bytes.Repeat([]byte("tail line\n"), (logSizeLimit/10)+1000))
		f.Close()
		serviceTestRequire(t, err)
		paths[fd] = path
		before[fd], err = os.Stat(path)
		serviceTestRequire(t, err)
	}
	for i := 0; i < 8; i++ {
		path := filepath.Join(r.CoreDir(), fmt.Sprintf("mihomo_new%d.log", i))
		serviceTestWrite(t, path, []byte("new"), 0600)
		future := time.Now().Add(time.Hour)
		serviceTestRequire(t, os.Chtimes(path, future, future))
	}
	serviceTestRequire(t, r.MaintainLogs())
	if n := len(maintenanceLogs(t, r)); n != logHistoryLimit {
		t.Fatalf("active counted incorrectly: %d", n)
	}
	for fd, path := range paths {
		info, err := os.Stat(path)
		serviceTestRequire(t, err)
		limit := int64(logTailSize + 4096)
		if fd == 2 {
			limit = logErrorTail
		}
		if !os.SameFile(before[fd], info) || info.Size() > limit {
			t.Fatalf("fd %d was replaced or not shrunk: %d", fd, info.Size())
		}
	}
	first, err := os.Stat(paths[1])
	serviceTestRequire(t, err)
	deadline := time.Now().Add(time.Second)
	for {
		now, err := os.Stat(paths[1])
		serviceTestRequire(t, err)
		if now.Size() > first.Size() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("core stopped writing after copytruncate")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		serviceTestRequire(t, err)
		if bytes.ContainsRune(data, 0) {
			t.Fatal("copytruncate followed by writing created a hole")
		}
	}
	// Manual ClearLogs keeps its skip-active protocol after automatic trimming.
	var out bytes.Buffer
	r.Out = &out
	serviceTestRequire(t, r.ClearLogs())
	if !strings.Contains(out.String(), "file\tskipped\t") {
		t.Fatalf("manual clear lost active skip: %s", out.String())
	}
	serviceTestRequire(t, r.Service("stop"))
}

func TestMaintainLogsLegacyNonAppendCore(t *testing.T) {
	r := serviceTestRuntime(t)
	loc, err := r.serviceLocation()
	serviceTestRequire(t, err)
	path := filepath.Join(r.CoreDir(), "mihomo_legacy.log")
	f, err := base.OpenRegular(path, os.O_CREATE|os.O_WRONLY, 0600)
	serviceTestRequire(t, err)
	_, err = f.Write(bytes.Repeat([]byte("L"), logSizeLimit+1))
	serviceTestRequire(t, err)
	stderr := filepath.Join(r.RunDir(), "error_mihomo.log")
	ef, err := base.OpenRegular(stderr, os.O_CREATE|os.O_WRONLY, 0600)
	serviceTestRequire(t, err)
	_, err = ef.Write(bytes.Repeat([]byte("E"), logErrorLimit+1))
	serviceTestRequire(t, err)
	cmd := exec.Command(loc.binary, serviceCoreArgs(loc, filepath.Join(loc.core, "config.yaml"))...)
	cmd.Stdout, cmd.Stderr, cmd.Dir = f, ef, loc.core
	serviceTestRequire(t, cmd.Start())
	f.Close()
	ef.Close()
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = serviceRemovePID(loc) }()
	id, err := serviceProcIdentity(cmd.Process.Pid)
	serviceTestRequire(t, err)
	// Retain the same /proc argument readiness tolerance as production startup.
	serviceTestRequire(t, serviceAwaitStartupArguments(time.Now().Add(time.Second), make(chan error), func() error {
		return serviceVerifyProcess(loc, id)
	}))
	serviceTestRequire(t, serviceSavePID(loc, id))
	for i := 0; i < 8; i++ {
		serviceTestWrite(t, filepath.Join(r.CoreDir(), fmt.Sprintf("mihomo_history%d.log", i)), []byte("history"), 0600)
	}
	serviceTestRequire(t, r.MaintainLogs())
	for _, path := range []string{path, stderr} {
		info, err := os.Stat(path)
		serviceTestRequire(t, err)
		limit := int64(logErrorLimit)
		if strings.Contains(filepath.Base(path), "legacy") {
			limit = logSizeLimit
		}
		if info.Size() <= limit {
			t.Fatalf("legacy active log truncated: %s", path)
		}
	}
	serviceTestRequire(t, r.Service("stop"))
	info, err := os.Stat(path)
	serviceTestRequire(t, err)
	if info.Size() > logTailSize {
		t.Fatalf("stopped legacy log not shrunk: %d", info.Size())
	}
}

func TestMaintainLogsBusyIsFastAndErrorsAreVisible(t *testing.T) {
	r := serviceTestRuntime(t)
	var out, warnings bytes.Buffer
	r.Out, r.Err = &out, &warnings
	lock, err := base.AcquireServiceLock(r.RunDir(), time.Second)
	serviceTestRequire(t, err)
	defer lock.Close()
	_, err = base.AcquireServiceLock(r.RunDir(), 0)
	if !errors.Is(err, base.ErrServiceLockTimeout) {
		t.Fatalf("busy lock lacks sentinel: %v", err)
	}
	begin := time.Now()
	serviceTestRequire(t, r.MaintainLogs())
	if elapsed := time.Since(begin); elapsed > 100*time.Millisecond {
		t.Fatalf("busy maintenance blocked: %v", elapsed)
	}
	if out.Len() != 0 || warnings.Len() != 0 {
		t.Fatal("busy maintenance emitted output")
	}
	serviceTestRequire(t, lock.Close())
	serviceTestRequire(t, os.Remove(filepath.Join(r.RunDir(), "service.lock")))
	serviceTestRequire(t, os.Mkdir(filepath.Join(r.RunDir(), "service.lock"), 0700))
	if err := r.MaintainLogs(); err == nil || errors.Is(err, base.ErrServiceLockTimeout) {
		t.Fatalf("unsafe lock swallowed as busy: %v", err)
	}
}

func TestMaintainLogsUnsafePathsAndIdentity(t *testing.T) {
	for _, target := range []string{"log-link", "log-hardlink", "check-link", "core-link", "config-link", "identity", "pid"} {
		t.Run(target, func(t *testing.T) {
			r := serviceTestRuntime(t)
			history := filepath.Join(r.CoreDir(), "mihomo_history.log")
			maintenanceFill(t, history, logSizeLimit+1)
			outside := filepath.Join(t.TempDir(), "outside")
			maintenanceFill(t, outside, logSizeLimit+1)
			switch target {
			case "log-link":
				serviceTestRequire(t, os.Symlink(outside, filepath.Join(r.CoreDir(), "mihomo_link.log")))
			case "log-hardlink":
				serviceTestRequire(t, os.Link(outside, filepath.Join(r.CoreDir(), "mihomo_link.log")))
			case "check-link":
				serviceTestRequire(t, os.Symlink(outside, filepath.Join(r.RunDir(), "check.log")))
			case "core-link":
				serviceTestRequire(t, os.Symlink(filepath.Dir(outside), filepath.Join(r.Root, "mihomo_smart")))
			case "config-link":
				path := filepath.Join(r.Root, "scripts", "box.config")
				serviceTestRequire(t, os.MkdirAll(filepath.Dir(path), 0700))
				serviceTestRequire(t, os.Symlink(outside, path))
			case "identity":
				serviceTestRequire(t, r.Service("start"))
				pid, err := r.RunningPID()
				serviceTestRequire(t, err)
				loc, err := r.serviceLocation()
				serviceTestRequire(t, err)
				id := serviceTestIdentity(t, pid)
				id.Start = "0"
				serviceTestRequire(t, serviceSavePID(loc, id))
				// Refill: startup maintenance has already shrunk history.
				maintenanceFill(t, history, logSizeLimit+1)
			case "pid":
				serviceTestWrite(t, r.PIDFile(), []byte(strconv.Itoa(os.Getpid())), 0600)
			}
			err := r.MaintainLogs()
			if target != "log-link" && err == nil {
				t.Fatal("unsafe path or process information accepted")
			}
			data, readErr := os.ReadFile(outside)
			serviceTestRequire(t, readErr)
			if !bytes.Equal(data, bytes.Repeat([]byte("x"), logSizeLimit+1)) {
				t.Fatal("outside data changed")
			}
			if target == "identity" || target == "pid" {
				info, err := os.Stat(history)
				serviceTestRequire(t, err)
				if info.Size() != logSizeLimit+1 {
					t.Fatal("history modified without safe process information")
				}
			}
		})
	}
}

func TestMaintainLogsRejectsReplacedInode(t *testing.T) {
	r := serviceTestRuntime(t)
	path := filepath.Join(r.CoreDir(), "mihomo_history.log")
	maintenanceFill(t, path, logSizeLimit+1)
	entry, err := logsMaintenanceEntry(path)
	serviceTestRequire(t, err)
	serviceTestRequire(t, os.Rename(path, path+".saved"))
	maintenanceFill(t, path, logSizeLimit+1)
	if err := logsTrimEntry(entry, logSizeLimit, logTailSize, nil); err == nil {
		t.Fatal("truncated replacement inode")
	}
	if err := logsRemoveEntry(entry); err == nil {
		t.Fatal("unlinked replacement inode")
	}
}

func TestMaintainLogsBoundedBacklog(t *testing.T) {
	r := serviceTestRuntime(t)
	for i := 0; i < logDeleteLimit+20; i++ {
		path := filepath.Join(r.CoreDir(), fmt.Sprintf("mihomo_%03d.log", i))
		maintenanceFill(t, path, logSizeLimit+1)
		mtime := time.Unix(int64(i+1), 0)
		serviceTestRequire(t, os.Chtimes(path, mtime, mtime))
	}
	serviceTestRequire(t, r.MaintainLogs())
	entries := maintenanceLogs(t, r)
	if len(entries) < 20 {
		t.Fatalf("single cycle exceeded deletion budget: %d", len(entries))
	}
	for _, e := range entries {
		if e.size != logSizeLimit+1 {
			t.Fatal("read/wrote large retained files before deleting backlog")
		}
	}
	for cycle := 0; cycle < 10 && len(entries) > logHistoryLimit; cycle++ {
		serviceTestRequire(t, r.MaintainLogs())
		entries = maintenanceLogs(t, r)
	}
	if len(entries) != logHistoryLimit {
		t.Fatalf("backlog did not converge: %d", len(entries))
	}
	for _, e := range entries {
		if e.size != logTailSize || e.mtime.Before(time.Unix(logDeleteLimit+16, 0)) {
			t.Fatalf("wrong final retained file: %+v", e)
		}
	}
}

func TestMaintainLogsBacklogDoesNotStarveActiveOrDiagnostics(t *testing.T) {
	r := serviceTestRuntime(t)
	serviceTestRequire(t, r.Service("start"))
	pid, err := r.RunningPID()
	serviceTestRequire(t, err)
	stdout, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/1", pid))
	serviceTestRequire(t, err)
	maintenanceFill(t, stdout, logSizeLimit+1)
	stderr := filepath.Join(r.RunDir(), "error_mihomo.log")
	check := filepath.Join(r.RunDir(), "check.log")
	maintenanceFill(t, stderr, logErrorLimit+1)
	maintenanceFill(t, check, logErrorLimit+1)
	for i := 0; i < logScanLimit+20; i++ {
		serviceTestWrite(t, filepath.Join(r.CoreDir(), fmt.Sprintf("mihomo_backlog_%04d.log", i)), []byte("old"), 0600)
	}
	// Automatic maintenance must not descend into data trees, even if a
	// nested filename happens to look like a core log.
	nested := filepath.Join(r.CoreDir(), ".mono-box", "providers", "mihomo_data.log")
	maintenanceFill(t, nested, logSizeLimit+1)
	serviceTestRequire(t, r.MaintainLogs())
	for path, limit := range map[string]int64{
		stdout: logTailSize + 4096, stderr: logErrorTail, check: logErrorTail,
	} {
		info, err := os.Stat(path)
		serviceTestRequire(t, err)
		if info.Size() > limit {
			t.Fatalf("backlog starved %s: %d", path, info.Size())
		}
	}
	info, err := os.Stat(nested)
	serviceTestRequire(t, err)
	if info.Size() != logSizeLimit+1 {
		t.Fatal("automatic maintenance entered the provider tree")
	}
	entries := maintenanceLogs(t, r)
	if len(entries) <= logHistoryLimit+1 {
		t.Fatal("fixture did not leave a partial history backlog")
	}
	// Verify partial inventories converge and retain globally newest entries.
	for cycle := 0; cycle < 20 && len(entries) > logHistoryLimit+1; cycle++ {
		serviceTestRequire(t, r.MaintainLogs())
		entries = maintenanceLogs(t, r)
	}
	if len(entries) != logHistoryLimit+1 { // five root logs plus provider data
		t.Fatalf("partial inventory did not converge: %d", len(entries))
	}
	serviceTestRequire(t, r.Service("stop"))
}

func TestServiceMaintenanceWarningsDoNotFailLifecycle(t *testing.T) {
	r := serviceTestRuntime(t)
	var warnings bytes.Buffer
	r.Err = &warnings
	outside := filepath.Join(t.TempDir(), "outside")
	serviceTestWrite(t, outside, []byte("keep"), 0600)
	// Unsafe history is a maintenance error, not a startup permission error.
	serviceTestRequire(t, os.Symlink(filepath.Dir(outside), filepath.Join(r.Root, "mihomo_smart")))
	serviceTestRequire(t, r.Service("start"))
	pid, err := r.RunningPID()
	serviceTestRequire(t, err)
	serviceTestRequire(t, syscall.Kill(pid, 0))
	serviceTestState(t, r, "Running")
	if !strings.Contains(warnings.String(), "[Warn]: Log maintenance:") {
		t.Fatalf("maintenance error not reported: %s", warnings.String())
	}
	serviceTestRequire(t, r.Service("stop"))
	serviceTestState(t, r, "Stopped")
	if _, err := os.Stat(r.PIDFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pid after stop: %v", err)
	}
}

func TestLogsFDFlagsRejectMissingOrMalformed(t *testing.T) {
	for _, data := range []string{"", "pos: 4\n", "flags: invalid\n"} {
		if _, err := logsFDFlags([]byte(data)); err == nil {
			t.Fatalf("accepted fdinfo %q", data)
		}
	}
}
