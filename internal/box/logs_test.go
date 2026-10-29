//go:build linux || android

package box

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLogsSizeReadAndClear(t *testing.T) {
	r := serviceTestRuntime(t)
	var out bytes.Buffer
	r.Out = &out
	first := filepath.Join(r.CoreDir(), "mihomo_202601010001.log")
	nested := filepath.Join(r.CoreDir(), "nested", "mihomo_202601010002.log")
	serviceTestWrite(t, first, []byte("first\n"), 0600)
	serviceTestWrite(t, nested, []byte("second\n"), 0600)
	serviceTestWrite(t, filepath.Join(r.CoreDir(), "not-a-core.log"), []byte("unrelated"), 0600)
	serviceTestRequire(t, r.LogsSize())
	want := fmt.Sprintf("file\t6\t%s\nfile\t7\t%s\ntotal\t13\t2\n", first, nested)
	if out.String() != want {
		t.Fatalf("logs size:\n%s\nwant:\n%s", out.String(), want)
	}
	out.Reset()
	serviceTestRequire(t, r.ReadLog(nested))
	if out.String() != "second\n" {
		t.Fatalf("read log %q", out.String())
	}
	out.Reset()
	serviceTestRequire(t, r.ClearLogs())
	want = fmt.Sprintf("file\tcleared\t6\t%s\nfile\tcleared\t7\t%s\ncleared\t13\t2\t0\t0\n", first, nested)
	if out.String() != want {
		t.Fatalf("clear logs:\n%s\nwant:\n%s", out.String(), want)
	}
	if _, err := os.Stat(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("log still exists: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(r.CoreDir(), "not-a-core.log")); err != nil || string(data) != "unrelated" {
		t.Fatalf("unrelated file: %q %v", data, err)
	}
	out.Reset()
	serviceTestRequire(t, r.LogsSize())
	if out.String() != "total\t0\t0\n" {
		t.Fatalf("empty size %q", out.String())
	}
	out.Reset()
	serviceTestRequire(t, r.ClearLogs())
	if out.String() != "cleared\t0\t0\t0\t0\n" {
		t.Fatalf("empty clear %q", out.String())
	}
}

func TestLogsKeepActiveInode(t *testing.T) {
	r := serviceTestRuntime(t)
	serviceTestRequire(t, r.Service("start"))
	pid, err := r.RunningPID()
	serviceTestRequire(t, err)
	stdout, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/1", pid))
	serviceTestRequire(t, err)
	// Rename stdout so its path is no longer the newest file in the listing.
	renamed := filepath.Join(r.CoreDir(), "mihomo_000000000000.log")
	serviceTestRequire(t, os.Rename(stdout, renamed))
	old := filepath.Join(r.CoreDir(), "mihomo_199901010000.log")
	newer := filepath.Join(r.CoreDir(), "mihomo_999999999999.log")
	serviceTestWrite(t, old, []byte("old"), 0600)
	serviceTestWrite(t, newer, []byte("newer"), 0600)
	nested := filepath.Join(r.CoreDir(), "archive", "mihomo_archived.log")
	serviceTestWrite(t, nested, []byte("archived"), 0600)
	var out bytes.Buffer
	r.Out = &out
	serviceTestRequire(t, r.ClearLogs())
	if !strings.Contains(out.String(), "file\tskipped\t") || !strings.Contains(out.String(), renamed+"\n") || !strings.Contains(out.String(), "cleared\t16\t3\t0\t1\n") {
		t.Fatalf("clear output %s", out.String())
	}
	for _, path := range []string{old, newer, nested} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("retained inactive log %s: %v", path, err)
		}
	}
	before, err := os.Stat(renamed)
	serviceTestRequire(t, err)
	deadline := time.Now().Add(time.Second)
	for {
		after, err := os.Stat(renamed)
		serviceTestRequire(t, err)
		if after.Size() > before.Size() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("active log stopped receiving data")
		}
		time.Sleep(15 * time.Millisecond)
	}
	serviceTestRequire(t, r.Service("stop"))
	out.Reset()
	serviceTestRequire(t, r.ClearLogs())
	if _, err := os.Stat(renamed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stopped log was not removed: %v", err)
	}
}

func TestLogsPathSafety(t *testing.T) {
	r := serviceTestRuntime(t)
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "mihomo_secret.log")
	serviceTestWrite(t, outside, []byte("secret"), 0600)
	valid := filepath.Join(r.CoreDir(), "mihomo_valid.log")
	serviceTestWrite(t, valid, []byte("valid"), 0600)
	leaf := filepath.Join(r.CoreDir(), "mihomo_link.log")
	serviceTestRequire(t, os.Symlink(outside, leaf))
	parent := filepath.Join(r.CoreDir(), "linked")
	serviceTestRequire(t, os.Symlink(outsideDir, parent))
	fifo := filepath.Join(r.CoreDir(), "mihomo_fifo.log")
	serviceTestRequire(t, syscall.Mkfifo(fifo, 0600))
	hardlink := filepath.Join(r.CoreDir(), "mihomo_hardlink.log")
	serviceTestRequire(t, os.Link(outside, hardlink))
	paths := []string{
		outside, leaf, filepath.Join(parent, "mihomo_secret.log"), fifo, hardlink,
		"mihomo_valid.log", r.CoreDir() + "/../" + filepath.Base(r.CoreDir()) + "/mihomo_valid.log",
		filepath.Join(r.Root, "mihomo-other", "mihomo_valid.log"),
		filepath.Join(r.CoreDir(), "config.yaml"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if err := r.ReadLog(path); err == nil {
				t.Fatalf("read unsafe path %q", path)
			}
			if err := r.OpenLog(path); err == nil {
				t.Fatalf("opened unsafe path %q", path)
			}
		})
	}
	// Hardlinks are refused before reading or deleting, including inventory calls.
	if err := r.ClearLogs(); err == nil {
		t.Fatal("cleanup accepted a hardlink")
	}
	data, err := os.ReadFile(outside)
	serviceTestRequire(t, err)
	if string(data) != "secret" {
		t.Fatalf("outside data changed: %q", data)
	}
	serviceTestRequire(t, os.Remove(hardlink))
	var out bytes.Buffer
	r.Out = &out
	serviceTestRequire(t, r.LogsSize())
	if !strings.Contains(out.String(), "total\t5\t1\n") {
		t.Fatalf("unsafe paths entered inventory: %s", out.String())
	}
	out.Reset()
	serviceTestRequire(t, r.ClearLogs())
	if _, err := os.Lstat(leaf); err != nil {
		t.Fatalf("symlink was deleted: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside log was deleted: %v", err)
	}
}

func TestLogsOpenExportAndIntent(t *testing.T) {
	r := serviceTestRuntime(t)
	path := filepath.Join(r.CoreDir(), "mihomo_$(touch OWNED);#?.log")
	content := []byte("exported snapshot\n")
	serviceTestWrite(t, path, content, 0600)
	share := filepath.Join(t.TempDir(), "Download", "MonoBox", "logs")
	record := filepath.Join(t.TempDir(), "am-args")
	t.Setenv("MONOBOX_AM_ARGS", record)
	am := filepath.Join(t.TempDir(), "am")
	serviceTestWrite(t, am, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$MONOBOX_AM_ARGS\"\ncase \"$*\" in *vnd.android.document/directory*) exit 1;; esac\n"), 0700)
	var out bytes.Buffer
	r.Out = &out
	serviceTestRequire(t, r.logsOpenAt(path, share, am, time.Second))
	export := filepath.Join(share, filepath.Base(path))
	actual, err := os.ReadFile(export)
	serviceTestRequire(t, err)
	if !bytes.Equal(actual, content) {
		t.Fatalf("export %q", actual)
	}
	info, err := os.Stat(export)
	serviceTestRequire(t, err)
	if info.Mode().Perm() != 0644 {
		t.Fatalf("export mode %o", info.Mode().Perm())
	}
	if out.String() != "opened\t"+export+"\n" {
		t.Fatalf("open output %q", out.String())
	}
	args, err := os.ReadFile(record)
	serviceTestRequire(t, err)
	text := string(args)
	if !strings.Contains(text, "vnd.android.document/directory") || !strings.Contains(text, "text/plain") || !strings.Contains(text, "--grant-read-uri-permission") || !strings.Contains(text, "mihomo_%24%28touch%20OWNED%29%3B%23%3F.log") {
		t.Fatalf("intent args: %s", args)
	}
	if _, err := os.Stat(filepath.Join(r.Root, "OWNED")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shell payload ran: %v", err)
	}
	// Existing destination links must not redirect the exported copy.
	outside := filepath.Join(t.TempDir(), "outside")
	serviceTestWrite(t, outside, []byte("keep"), 0600)
	serviceTestRequire(t, os.Remove(export))
	serviceTestRequire(t, os.Symlink(outside, export))
	if err := r.logsOpenAt(path, share, am, time.Second); err == nil {
		t.Fatal("export followed destination symlink")
	}
	actual, err = os.ReadFile(outside)
	serviceTestRequire(t, err)
	if string(actual) != "keep" {
		t.Fatalf("outside export changed: %q", actual)
	}
	linkedShare := filepath.Join(t.TempDir(), "share")
	serviceTestRequire(t, os.Symlink(filepath.Dir(outside), linkedShare))
	if err := r.logsOpenAt(path, linkedShare, am, time.Second); err == nil {
		t.Fatal("export followed share directory symlink")
	}
}

func TestLogsOpenTimeout(t *testing.T) {
	r := serviceTestRuntime(t)
	path := filepath.Join(r.CoreDir(), "mihomo_timeout.log")
	serviceTestWrite(t, path, []byte("log"), 0600)
	am := filepath.Join(t.TempDir(), "am")
	// exec replaces the helper shell so the timeout kills the actual sleeping process.
	serviceTestWrite(t, am, []byte("#!/bin/sh\nexec sleep 30\n"), 0700)
	begin := time.Now()
	if err := r.logsOpenAt(path, t.TempDir(), am, 100*time.Millisecond); err == nil {
		t.Fatal("activity manager timeout ignored")
	}
	if time.Since(begin) > 2*time.Second {
		t.Fatal("activity manager exceeded timeout")
	}
}

func TestLogsMissingCoreDirectory(t *testing.T) {
	r := serviceTestRuntime(t)
	serviceTestRequire(t, os.RemoveAll(r.CoreDir()))
	var out bytes.Buffer
	r.Out = &out
	serviceTestRequire(t, r.LogsSize())
	if out.String() != "total\t0\t0\n" {
		t.Fatalf("missing dir: %q", out.String())
	}
}
