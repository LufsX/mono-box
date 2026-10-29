//go:build linux || android

package testutil

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/LufsX/mono-box/internal/box/base"
)

var core struct {
	sync.Once
	data []byte
	err  error
}

// Build a separate, real executable: no mocks of exec, /proc, signals or flock.
const coreSource = `package main
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

func FakeCore(t *testing.T) []byte {
	t.Helper()
	core.Do(func() {
		dir, err := os.MkdirTemp("", "monobox-fake-build-")
		if err != nil {
			core.err = err
			return
		}
		defer os.RemoveAll(dir)
		source := filepath.Join(dir, "main.go")
		if err = os.WriteFile(source, []byte(coreSource), 0600); err != nil {
			core.err = err
			return
		}
		binary := filepath.Join(dir, "fake-core")
		cmd := exec.Command("go", "build", "-o", binary, source)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if output, err := cmd.CombinedOutput(); err != nil {
			core.err = fmt.Errorf("build fake core: %w: %s", err, output)
			return
		}
		core.data, core.err = os.ReadFile(binary)
	})
	if core.err != nil {
		t.Fatal(core.err)
	}
	return core.data
}

func NewRuntime(t *testing.T) *base.Runtime {
	t.Helper()
	root := t.TempDir()
	module := filepath.Join(root, "module")
	r := &base.Runtime{Root: root, ModuleDir: module, Config: base.DefaultConfig(), Out: io.Discard, Err: io.Discard}
	r.Config.CoreUserGroup = fmt.Sprintf("%d:%d", os.Geteuid(), os.Getegid())
	for _, dir := range []string{filepath.Dir(r.CoreBinary()), r.CoreDir(), r.RunDir(), module} {
		Require(t, os.MkdirAll(dir, 0700))
	}
	Write(t, r.CoreBinary(), FakeCore(t), 0700)
	Write(t, filepath.Join(r.CoreDir(), "config.yaml"), []byte("valid"), 0600)
	Write(t, filepath.Join(module, "module.prop"), []byte("id=mono_box\ndescription=old\nversion=1\n"), 0644)
	executable, err := os.Stat(r.CoreBinary())
	Require(t, err)
	t.Cleanup(func() { cleanupCore(r, executable) })
	return r
}

// Reclaim fixture processes even if a failed test damaged config or PID files.
func cleanupCore(r *base.Runtime, executable os.FileInfo) {
	data, _ := os.ReadFile(filepath.Join(r.CoreDir(), "invocations.jsonl"))
	seen := make(map[int]bool)
	for _, line := range bytes.Split(data, []byte("\n")) {
		var event Event
		if json.Unmarshal(line, &event) != nil || event.Test || event.PID <= 1 || seen[event.PID] {
			continue
		}
		seen[event.PID] = true
		process, err := os.FindProcess(event.PID)
		if err != nil {
			continue
		}
		info, err := os.Stat(filepath.Join("/proc", strconv.Itoa(event.PID), "exe"))
		if err == nil && os.SameFile(executable, info) {
			if process.Kill() == nil {
				_, _ = process.Wait()
			}
		}
		_ = process.Release()
	}
}

func Write(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	Require(t, os.MkdirAll(filepath.Dir(path), 0700))
	Require(t, os.WriteFile(path, data, mode))
}

func Require(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func State(t *testing.T, r *base.Runtime, state string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.ModuleDir, "module.prop"))
	Require(t, err)
	if !strings.Contains(string(data), "description=Use mihomo on Android devices. ["+state+"]\n") {
		t.Fatalf("module status: %s", data)
	}
}

type Event struct {
	Test          bool
	Args          []string
	PID, UID, GID int
}

func Events(t *testing.T, r *base.Runtime) []Event {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.CoreDir(), "invocations.jsonl"))
	Require(t, err)
	var events []Event
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var event Event
		Require(t, json.Unmarshal(scanner.Bytes(), &event))
		events = append(events, event)
	}
	Require(t, scanner.Err())
	return events
}

func ProcessStopped(t *testing.T, pid int) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if os.IsNotExist(err) {
		return
	}
	Require(t, err)
	_, tail, ok := strings.Cut(string(data), ") ")
	if !ok || (!strings.HasPrefix(tail, "Z ") && !strings.HasPrefix(tail, "X ")) {
		t.Fatalf("process %d survived: %s", pid, data)
	}
}
