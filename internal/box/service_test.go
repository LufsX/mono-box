//go:build linux || android

package box

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var serviceTestCore struct {
	sync.Once
	data []byte
	err  error
}

// Build a separate, real executable: no mocks of exec, /proc, signals or flock.
const serviceTestCoreSource = `package main
import("encoding/json";"fmt";"os";"os/signal";"path/filepath";"strings";"syscall";"time")
func main(){
	args:=os.Args[1:];test:=len(args)>0&&args[0]=="-t";if test{args=args[1:]}
	if len(args)!=4||args[0]!="-d"||args[2]!="-f"{fmt.Fprintln(os.Stderr,"bad args");os.Exit(2)}
	data,err:=os.ReadFile(args[3]);if err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(2)}
	mode:=strings.TrimSpace(string(data))
	event:=map[string]any{"test":test,"args":os.Args[1:],"pid":os.Getpid(),"uid":os.Getuid(),"gid":os.Getgid()}
	f,err:=os.OpenFile(filepath.Join(args[1],"invocations.jsonl"),os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600)
	if err!=nil{panic(err)};json.NewEncoder(f).Encode(event);f.Close()
	if test {
		if mode=="invalid"{fmt.Fprintln(os.Stderr,"fake configuration rejected");os.Exit(42)}
		if mode=="check-hang"{for{time.Sleep(time.Hour)}}
		fmt.Println("configuration valid");return
	}
	fmt.Println("fake stdout");fmt.Fprintln(os.Stderr,"fake stderr")
	if mode=="exit"{os.Exit(23)}
	if mode=="delayed-exit"{time.Sleep(70*time.Millisecond);os.Exit(23)}
	if mode=="break-metadata"{path:=filepath.Join(filepath.Dir(args[1]),"module","module.prop");if err:=os.Remove(path);err!=nil{panic(err)};if err:=os.Mkdir(path,0700);err!=nil{panic(err)}}
	signals:=make(chan os.Signal,2);signal.Notify(signals,syscall.SIGTERM,syscall.SIGINT)
	tick:=time.NewTicker(30*time.Millisecond);defer tick.Stop()
	for{select{case <-signals:
		if mode=="stop-pid-directory"{path:=filepath.Join(filepath.Dir(args[1]),"run",filepath.Base(args[1])+".pid");if err:=os.Remove(path);err!=nil{panic(err)};if err:=os.Mkdir(path,0700);err!=nil{panic(err)}}
		if mode!="ignore-term"{return};case <-tick.C:fmt.Println("still running")}}
}
`

func serviceTestFake(t *testing.T) []byte {
	t.Helper()
	serviceTestCore.Do(func() {
		dir, err := os.MkdirTemp("", "monobox-fake-build-")
		if err != nil {
			serviceTestCore.err = err
			return
		}
		defer os.RemoveAll(dir)
		source := filepath.Join(dir, "main.go")
		if err = os.WriteFile(source, []byte(serviceTestCoreSource), 0600); err != nil {
			serviceTestCore.err = err
			return
		}
		binary := filepath.Join(dir, "fake-core")
		cmd := exec.Command("go", "build", "-o", binary, source)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if output, err := cmd.CombinedOutput(); err != nil {
			serviceTestCore.err = fmt.Errorf("build fake core: %w: %s", err, output)
			return
		}
		serviceTestCore.data, serviceTestCore.err = os.ReadFile(binary)
	})
	if serviceTestCore.err != nil {
		t.Fatal(serviceTestCore.err)
	}
	return serviceTestCore.data
}

func serviceTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	root := t.TempDir()
	module := filepath.Join(root, "module")
	r := &Runtime{Root: root, ModuleDir: module, Config: DefaultConfig(), Out: io.Discard, Err: io.Discard}
	r.Config.CoreUserGroup = fmt.Sprintf("%d:%d", os.Geteuid(), os.Getegid())
	for _, dir := range []string{filepath.Dir(r.CoreBinary()), r.CoreDir(), r.RunDir(), module} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	serviceTestWrite(t, r.CoreBinary(), serviceTestFake(t), 0700)
	serviceTestWrite(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte("valid"), 0600)
	serviceTestWrite(t, filepath.Join(module, "module.prop"), []byte("id=mono_box\ndescription=old\nversion=1\n"), 0644)
	t.Cleanup(func() {
		if id, err := r.RunningPID(); err == nil {
			_ = syscall.Kill(id, syscall.SIGKILL)
			loc, _ := r.serviceLocation()
			_ = serviceWaitStopped(loc, serviceTestIdentity(t, id), time.Second)
		}
	})
	return r
}

func serviceTestIdentity(t *testing.T, pid int) serviceIdentity {
	t.Helper()
	id, err := serviceProcIdentity(pid)
	if err != nil && !errors.Is(err, ErrStopped) {
		t.Error(err)
	}
	return id
}

func serviceTestWrite(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func serviceTestRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func serviceTestState(t *testing.T, r *Runtime, state string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.ModuleDir, "module.prop"))
	serviceTestRequire(t, err)
	if !strings.Contains(string(data), "description=Use mihomo on Android devices. ["+state+"]\n") {
		t.Fatalf("module status: %s", data)
	}
}

type serviceTestEvent struct {
	Test          bool
	Args          []string
	PID, UID, GID int
}

func serviceTestEvents(t *testing.T, r *Runtime) []serviceTestEvent {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.CoreDir(), "invocations.jsonl"))
	serviceTestRequire(t, err)
	var events []serviceTestEvent
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var e serviceTestEvent
		serviceTestRequire(t, json.Unmarshal(scanner.Bytes(), &e))
		events = append(events, e)
	}
	serviceTestRequire(t, scanner.Err())
	return events
}

func TestServiceLifecycle(t *testing.T) {
	r := serviceTestRuntime(t)
	if err := r.Service("status"); !errors.Is(err, ErrStopped) {
		t.Fatalf("stopped status: %v", err)
	}
	serviceTestState(t, r, "Stopped")
	profile := filepath.Join(r.CoreDir(), ".mono-box", "profiles", "chosen.yaml")
	serviceTestWrite(t, profile, []byte("valid"), 0600)
	serviceTestWrite(t, filepath.Join(r.CoreDir(), ".mono-box", "active-profile"), []byte("chosen.yaml"), 0600)
	serviceTestRequire(t, r.Service("start"))
	pid, err := r.RunningPID()
	serviceTestRequire(t, err)
	if pid <= 1 {
		t.Fatalf("pid %d", pid)
	}
	serviceTestState(t, r, "Running")
	// Changing the preferred binary must not lose the existing saved identity.
	changedPreference, err := r.serviceLocation()
	serviceTestRequire(t, err)
	changedPreference.binary = "/system/bin/new-preferred-core"
	tracked, err := serviceRunning(changedPreference)
	serviceTestRequire(t, err)
	if tracked.PID != pid {
		t.Fatal("binary preference change lost the running core")
	}
	pidData, err := os.ReadFile(r.PIDFile())
	serviceTestRequire(t, err)
	if strings.TrimSpace(string(pidData)) != strconv.Itoa(pid) {
		t.Fatalf("non-numeric pid file: %q", pidData)
	}
	events := serviceTestEvents(t, r)
	if len(events) != 2 || !events[0].Test || events[1].Test {
		t.Fatalf("invocations: %+v", events)
	}
	for _, event := range events {
		want := []string{"-d", r.CoreDir(), "-f", profile}
		if event.Test {
			want = append([]string{"-t"}, want...)
		}
		if fmt.Sprint(event.Args) != fmt.Sprint(want) || event.UID != os.Geteuid() || event.GID != os.Getegid() {
			t.Fatalf("event: %+v", event)
		}
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	serviceTestRequire(t, err)
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	if fields[2] != strconv.Itoa(pid) || fields[3] != strconv.Itoa(pid) {
		t.Fatalf("core did not detach process group/session: %s", stat)
	}
	serviceTestRequire(t, r.Service("start"))
	same, err := r.RunningPID()
	serviceTestRequire(t, err)
	if same != pid || len(serviceTestEvents(t, r)) != 2 {
		t.Fatal("idempotent start created another core")
	}
	stderr, err := os.ReadFile(filepath.Join(r.RunDir(), "error_mihomo.log"))
	serviceTestRequire(t, err)
	if !strings.Contains(string(stderr), "fake stderr") {
		t.Fatalf("stderr %q", stderr)
	}
	check, err := os.ReadFile(filepath.Join(r.RunDir(), "check.log"))
	serviceTestRequire(t, err)
	if !strings.Contains(string(check), "configuration valid") {
		t.Fatalf("check log %q", check)
	}
	for path, want := range map[string]os.FileMode{r.CoreBinary(): 0700, r.CoreDir(): 0750, profile: 0640, r.RunDir(): 0700} {
		info, err := os.Stat(path)
		serviceTestRequire(t, err)
		if info.Mode().Perm() != want {
			t.Fatalf("%s: mode %o, want %o", path, info.Mode().Perm(), want)
		}
	}
	// An atomic core upgrade must not make the running old inode unmanageable.
	replacement := r.CoreBinary() + ".new"
	serviceTestWrite(t, replacement, serviceTestFake(t), 0700)
	serviceTestRequire(t, os.Rename(replacement, r.CoreBinary()))
	afterUpgrade, err := r.RunningPID()
	serviceTestRequire(t, err)
	if afterUpgrade != pid {
		t.Fatalf("upgrade lost pid: %d", afterUpgrade)
	}
	serviceTestRequire(t, r.Service("restart"))
	next, err := r.RunningPID()
	serviceTestRequire(t, err)
	if next == pid {
		t.Fatal("restart retained old process")
	}
	serviceTestRequire(t, r.Service("stop"))
	serviceTestRequire(t, r.Service("stop"))
	serviceTestState(t, r, "Stopped")
	if _, err := os.Stat(r.PIDFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pid not removed: %v", err)
	}
	if err := r.Service("status"); !errors.Is(err, ErrStopped) {
		t.Fatalf("status after stop: %v", err)
	}
	if err := r.Service("bogus"); err == nil {
		t.Fatal("accepted invalid action")
	}
}

func TestServiceStartupFailures(t *testing.T) {
	for _, mode := range []string{"invalid", "exit", "delayed-exit"} {
		t.Run(mode, func(t *testing.T) {
			r := serviceTestRuntime(t)
			serviceTestWrite(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte(mode), 0600)
			err := r.Service("start")
			if err == nil {
				t.Fatal("start succeeded")
			}
			if mode != "invalid" && (!strings.Contains(err.Error(), "startup") || !strings.Contains(err.Error(), filepath.Join(r.RunDir(), "error_mihomo.log"))) {
				t.Fatalf("missing startup diagnostics: %v", err)
			}
			if mode != "invalid" && (!strings.Contains(err.Error(), "fake stderr") || !strings.Contains(err.Error(), "fake stdout")) {
				t.Fatalf("core output missing from startup error: %v", err)
			}
			if mode == "invalid" && !strings.Contains(err.Error(), "fake configuration rejected") {
				t.Fatalf("configuration checker output missing: %v", err)
			}
			if _, err := r.RunningPID(); !errors.Is(err, ErrStopped) {
				t.Fatalf("running after failed start: %v", err)
			}
			serviceTestState(t, r, "Error")
			if mode == "invalid" {
				log, err := os.ReadFile(filepath.Join(r.RunDir(), "check.log"))
				serviceTestRequire(t, err)
				if !strings.Contains(string(log), "configuration rejected") {
					t.Fatalf("check diagnostics %q", log)
				}
				if events := serviceTestEvents(t, r); len(events) != 1 || !events[0].Test {
					t.Fatalf("core started on invalid config: %+v", events)
				}
			}
		})
	}
}

func TestServiceForeignAndStalePID(t *testing.T) {
	r := serviceTestRuntime(t)
	foreign := exec.Command("sleep", "30")
	serviceTestRequire(t, foreign.Start())
	defer func() { _ = foreign.Process.Kill(); _ = foreign.Wait() }()
	serviceTestWrite(t, r.PIDFile(), []byte(strconv.Itoa(foreign.Process.Pid)), 0600)
	serviceTestRequire(t, r.Service("stop"))
	serviceTestRequire(t, foreign.Process.Signal(syscall.Signal(0)))
	serviceTestWrite(t, r.PIDFile(), []byte("not-a-pid\n"), 0600)
	if _, err := r.RunningPID(); !errors.Is(err, ErrStopped) {
		t.Fatalf("malformed pid: %v", err)
	}
	serviceTestWrite(t, r.PIDFile(), []byte("2147483647\n"), 0600)
	serviceTestRequire(t, r.Service("stop"))
	serviceTestRequire(t, r.Service("start"))
	pid, err := r.RunningPID()
	serviceTestRequire(t, err)
	defer syscall.Kill(pid, syscall.SIGKILL)
	loc, err := r.serviceLocation()
	serviceTestRequire(t, err)
	id, err := serviceProcIdentity(pid)
	serviceTestRequire(t, err)
	id.Start = "0"
	serviceTestRequire(t, serviceSavePID(loc, id))
	serviceTestRequire(t, r.Service("stop"))
	serviceTestRequire(t, syscall.Kill(pid, syscall.Signal(0)))
	if _, err := r.RunningPID(); !errors.Is(err, ErrStopped) {
		t.Fatalf("reused identity accepted: %v", err)
	}
}

func TestServiceTimeouts(t *testing.T) {
	r := serviceTestRuntime(t)
	serviceTestWrite(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte("check-hang"), 0600)
	loc, err := r.serviceLocation()
	serviceTestRequire(t, err)
	begin := time.Now()
	err = serviceValidateConfig(loc, filepath.Join(r.CoreDir(), "config.yaml"), nil, 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("check timeout: %v", err)
	}
	if time.Since(begin) > 2*time.Second {
		t.Fatal("check timeout did not bound wait")
	}
	events := serviceTestEvents(t, r)
	if _, err := serviceProcIdentity(events[0].PID); !errors.Is(err, ErrStopped) {
		t.Fatalf("validator survived timeout: %v", err)
	}
	serviceTestWrite(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte("ignore-term"), 0600)
	serviceTestRequire(t, r.Service("start"))
	pid, err := r.RunningPID()
	serviceTestRequire(t, err)
	serviceTestRequire(t, r.Service("stop"))
	if _, err := serviceProcIdentity(pid); !errors.Is(err, ErrStopped) {
		t.Fatalf("term-resistant core survived stop: %v", err)
	}
}

func serviceTestCommand(r *Runtime, action string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestServiceProcess$", "-test.timeout=20s")
	cmd.Env = append(os.Environ(), "MONOBOX_SERVICE_TEST_ROOT="+r.Root, "MONOBOX_SERVICE_TEST_ACTION="+action)
	return cmd
}

// TestServiceProcess is also a short-lived CLI for process-exit and flock tests.
func TestServiceProcess(t *testing.T) {
	root := os.Getenv("MONOBOX_SERVICE_TEST_ROOT")
	if root == "" {
		return
	}
	r := &Runtime{Root: root, ModuleDir: filepath.Join(root, "module"), Config: DefaultConfig(), Out: os.Stdout, Err: os.Stderr}
	r.Config.CoreUserGroup = fmt.Sprintf("%d:%d", os.Geteuid(), os.Getegid())
	action := os.Getenv("MONOBOX_SERVICE_TEST_ACTION")
	if action == "lock" {
		loc, err := r.serviceLocation()
		serviceTestRequire(t, err)
		lock, err := serviceAcquireLock(loc, time.Second)
		serviceTestRequire(t, err)
		defer lock.Close()
		fmt.Println("locked")
		time.Sleep(time.Hour)
		return
	}
	if action == "clear" {
		serviceTestRequire(t, r.ClearLogs())
		return
	}
	serviceTestRequire(t, r.Service(action))
}

func TestServiceConcurrentCLIAndDetach(t *testing.T) {
	r := serviceTestRuntime(t)
	var commands []*exec.Cmd
	var output [4]bytes.Buffer
	for i := range output {
		cmd := serviceTestCommand(r, "start")
		cmd.Stdout = &output[i]
		cmd.Stderr = &output[i]
		serviceTestRequire(t, cmd.Start())
		commands = append(commands, cmd)
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("concurrent start: %v: %s", err, output[i].String())
		}
	}
	pid, err := r.RunningPID()
	serviceTestRequire(t, err)
	events := serviceTestEvents(t, r)
	runs := 0
	for _, event := range events {
		if !event.Test {
			runs++
		}
	}
	if runs != 1 {
		t.Fatalf("created %d cores: %+v", runs, events)
	}
	// All four original controllers exited, but their detached child remains.
	serviceTestRequire(t, syscall.Kill(pid, syscall.Signal(0)))
	serviceTestRequire(t, r.Service("stop"))
	locker := serviceTestCommand(r, "lock")
	stdout, err := locker.StdoutPipe()
	serviceTestRequire(t, err)
	serviceTestRequire(t, locker.Start())
	defer func() { _ = locker.Process.Kill(); _ = locker.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	serviceTestRequire(t, err)
	if line != "locked\n" {
		t.Fatalf("lock helper output %q", line)
	}
	loc, err := r.serviceLocation()
	serviceTestRequire(t, err)
	if lock, err := serviceAcquireLock(loc, 80*time.Millisecond); err == nil {
		lock.Close()
		t.Fatal("parallel process bypassed lock")
	}
	serviceTestRequire(t, locker.Process.Kill())
	_ = locker.Wait()
	lock, err := serviceAcquireLock(loc, 300*time.Millisecond)
	serviceTestRequire(t, err)
	lock.Close()
}

func TestServiceUnsafePaths(t *testing.T) {
	for _, target := range []string{"profile", "marker", "binary", "pid", "lock", "check", "check-hardlink", "stderr", "core-dir", "runtime-config"} {
		t.Run(target, func(t *testing.T) {
			r := serviceTestRuntime(t)
			outside := filepath.Join(t.TempDir(), "outside")
			serviceTestWrite(t, outside, []byte("do not change"), 0600)
			var path string
			switch target {
			case "profile":
				path = filepath.Join(r.CoreDir(), ".mono-box", "profiles", "chosen.yaml")
				serviceTestWrite(t, filepath.Join(r.CoreDir(), ".mono-box", "active-profile"), []byte("chosen.yaml"), 0600)
			case "runtime-config":
				path = filepath.Join(r.Root, "scripts", "box.config")
			case "marker":
				path = filepath.Join(r.CoreDir(), ".mono-box", "active-profile")
			case "binary":
				path = r.CoreBinary()
			case "pid":
				path = r.PIDFile()
			case "lock":
				path = filepath.Join(r.RunDir(), "service.lock")
			case "check", "check-hardlink":
				path = filepath.Join(r.RunDir(), "check.log")
			case "stderr":
				path = filepath.Join(r.RunDir(), "error_mihomo.log")
			case "core-dir":
				serviceTestRequire(t, os.RemoveAll(r.CoreDir()))
				serviceTestRequire(t, os.Symlink(filepath.Dir(outside), r.CoreDir()))
			}
			if path != "" {
				serviceTestRequire(t, os.MkdirAll(filepath.Dir(path), 0700))
				if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
				if target == "check-hardlink" {
					serviceTestRequire(t, os.Link(outside, path))
				} else {
					serviceTestRequire(t, os.Symlink(outside, path))
				}
			}
			if err := r.Service("start"); err == nil {
				t.Fatal("unsafe path accepted")
			}
			data, err := os.ReadFile(outside)
			serviceTestRequire(t, err)
			if string(data) != "do not change" {
				t.Fatalf("outside file changed: %q", data)
			}
		})
	}
}

func TestServiceAndroidCredentials(t *testing.T) {
	cred, err := serviceResolveCredential("root:net_admin")
	if os.Geteuid() == 0 {
		serviceTestRequire(t, err)
		if cred.Uid != 0 || cred.Gid != 3005 || len(cred.Groups) != 1 || cred.Groups[0] != 3005 {
			t.Fatalf("credential: %+v", cred)
		}
	} else if err == nil {
		t.Fatal("non-root was allowed to switch to Android credentials")
	}
	if _, err := serviceResolveCredential("missing:group:extra"); err == nil {
		t.Fatal("invalid credential accepted")
	}
	if _, err := serviceResolveCredential("does-not-exist:3005"); err == nil {
		t.Fatal("unknown user accepted")
	}
	cred, err = serviceResolveCredential(fmt.Sprintf("%d:%d", os.Geteuid(), os.Getegid()))
	serviceTestRequire(t, err)
	if os.Geteuid() != 0 && cred != nil {
		t.Fatal("unprivileged self credential must not call setgroups")
	}
}

func TestServiceRelativeRoot(t *testing.T) {
	r := serviceTestRuntime(t)
	relative, err := filepath.Rel(".", r.Root)
	// filepath.Rel requires absolute paths on both sides.
	if err != nil {
		cwd, e := os.Getwd()
		serviceTestRequire(t, e)
		relative, err = filepath.Rel(cwd, r.Root)
	}
	serviceTestRequire(t, err)
	r.Root = relative
	serviceTestRequire(t, r.Service("start"))
	serviceTestRequire(t, r.Service("stop"))
}
