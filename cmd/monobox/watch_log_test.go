package main

import (
	"bufio"
	"bytes"
	"context"
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

func testWatchLog(t *testing.T, root string) *watchLogWriter {
	t.Helper()
	w, err := openWatchLog(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	return w
}

func readWatchLog(t *testing.T, root string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "run", "watch.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > watchLogMaxSize {
		t.Fatalf("log grew to %d bytes", len(data))
	}
	return data
}

func writeWatchLog(t *testing.T, w io.Writer, data []byte) {
	t.Helper()
	if n, err := w.Write(data); err != nil || n != len(data) {
		t.Fatalf("write: %d/%d, %v", n, len(data), err)
	}
}

func TestWatchLogShrinksOnOpen(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "run", "watch.log")
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	recent := bytes.Repeat([]byte("recent record\n"), 4096)
	old := append(bytes.Repeat([]byte("x"), watchLogMaxSize), '\n')
	if err := os.WriteFile(path, append(old, recent...), 0644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// This descriptor must continue to see the log after compaction.
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	testWatchLog(t, root)
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || after.Mode().Perm() != 0600 {
		t.Fatalf("inode or permissions changed incorrectly: %v", after)
	}
	data, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(data, recent) {
		t.Fatalf("old descriptor: got %d bytes, want %d, error %v", len(data), len(recent), err)
	}
	if !bytes.Equal(readWatchLog(t, root), recent) {
		t.Fatal("opening did not retain the recent complete lines")
	}
}

func TestWatchLogLimitAndMultipleDescriptors(t *testing.T) {
	root := t.TempDir()
	first := testWatchLog(t, root)
	second := testWatchLog(t, root)
	line := []byte(strings.Repeat("a", 127) + "\n")
	full := bytes.Repeat(line, watchLogMaxSize/len(line))
	writeWatchLog(t, first, full)
	if !bytes.Equal(readWatchLog(t, root), full) {
		t.Fatal("log compacted before reaching the limit")
	}
	writeWatchLog(t, second, []byte("second writer\n"))
	want := append(bytes.Repeat(line, watchLogTailSize/len(line)), []byte("second writer\n")...)
	if !bytes.Equal(readWatchLog(t, root), want) {
		t.Fatal("overflow did not preserve the last 512 KiB")
	}
	// first's offset still points at the old end until Write seeks under flock.
	writeWatchLog(t, first, []byte("first writer again\n"))
	want = append(want, []byte("first writer again\n")...)
	if !bytes.Equal(readWatchLog(t, root), want) {
		t.Fatal("stale descriptor offset corrupted the log")
	}
}

func TestWatchLogLargeWrites(t *testing.T) {
	for _, size := range []int{watchLogMaxSize - 17, watchLogMaxSize, watchLogMaxSize * 4} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			root := t.TempDir()
			w := testWatchLog(t, root)
			writeWatchLog(t, w, bytes.Repeat([]byte("old\n"), watchLogMaxSize/4))
			payload := append(bytes.Repeat([]byte("x"), size-7), []byte("LATEST\n")...)
			var terminal bytes.Buffer
			writeWatchLog(t, io.MultiWriter(&terminal, w), payload)
			if !bytes.Equal(terminal.Bytes(), payload) {
				t.Fatal("terminal lost part of a large write")
			}
			got := readWatchLog(t, root)
			if !bytes.HasSuffix(got, []byte("LATEST\n")) || len(got) < watchLogTailSize {
				t.Fatalf("latest content lost: %d bytes", len(got))
			}
			if size <= watchLogMaxSize && !bytes.HasSuffix(got, payload) {
				t.Fatal("a write within the limit lost content")
			}
		})
	}
	t.Run("skip partial line", func(t *testing.T) {
		root := t.TempDir()
		w := testWatchLog(t, root)
		writeWatchLog(t, w, []byte("obsolete\n"))
		recent := bytes.Repeat([]byte("complete recent line\n"), 1000)
		payload := append(bytes.Repeat([]byte("x"), 2*watchLogMaxSize), '\n')
		payload = append(payload, recent...)
		writeWatchLog(t, w, payload)
		if !bytes.Equal(readWatchLog(t, root), recent) {
			t.Fatal("oversized write did not retain only the recent complete lines")
		}
	})
	t.Run("single line without newline", func(t *testing.T) {
		root := t.TempDir()
		w := testWatchLog(t, root)
		payload := append(bytes.Repeat([]byte("x"), 2*watchLogMaxSize), []byte("LATEST")...)
		writeWatchLog(t, w, payload)
		if !bytes.Equal(readWatchLog(t, root), payload[len(payload)-watchLogTailSize:]) {
			t.Fatal("oversized line lost its latest bytes")
		}
	})
}

const watchLogTestRecords = 400

func watchLogTestRecord(id, sequence int) []byte {
	return []byte(fmt.Sprintf("%02d:%04d:%s\n", id, sequence, strings.Repeat("x", 2039)))
}

func checkWatchLogRecords(t *testing.T, root string) {
	t.Helper()
	data := readWatchLog(t, root)
	if len(data) < watchLogTailSize {
		t.Fatalf("unexpectedly short log: %d bytes", len(data))
	}
	last := make(map[int]int)
	for _, line := range bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
		fields := strings.SplitN(string(line), ":", 3)
		if len(fields) != 3 {
			t.Fatalf("broken record: %.40q", line)
		}
		id, errID := strconv.Atoi(fields[0])
		sequence, errSequence := strconv.Atoi(fields[1])
		if errID != nil || errSequence != nil || !bytes.Equal(append(append([]byte{}, line...), '\n'), watchLogTestRecord(id, sequence)) {
			t.Fatalf("corrupt record: %.40q", line)
		}
		if previous, ok := last[id]; ok && sequence != previous+1 {
			t.Fatalf("writer %d: record %d followed %d", id, sequence, previous)
		}
		last[id] = sequence
	}
	for id, sequence := range last {
		if sequence != watchLogTestRecords-1 {
			t.Fatalf("writer %d lost its latest record: %d", id, sequence)
		}
	}
}

func TestWatchLogConcurrentWriters(t *testing.T) {
	root := t.TempDir()
	writers := []*watchLogWriter{testWatchLog(t, root), testWatchLog(t, root)}
	var wg sync.WaitGroup
	for id := 0; id < 4; id++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for sequence := 0; sequence < watchLogTestRecords; sequence++ {
				p := watchLogTestRecord(id, sequence)
				if n, err := writers[id%len(writers)].Write(p); err != nil || n != len(p) {
					t.Errorf("write: %d, %v", n, err)
					return
				}
			}
		}(id)
	}
	wg.Wait()
	checkWatchLogRecords(t, root)
}

func TestWatchLogProcess(t *testing.T) {
	root := os.Getenv("MONOBOX_LOG_TEST_ROOT")
	if root == "" {
		return
	}
	id, err := strconv.Atoi(os.Getenv("MONOBOX_LOG_TEST_ID"))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "ready")
	w := testWatchLog(t, root)
	for sequence := 0; sequence < watchLogTestRecords; sequence++ {
		writeWatchLog(t, w, watchLogTestRecord(id, sequence))
	}
}

func TestWatchLogCrossProcessWriters(t *testing.T) {
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	type child struct {
		cmd    *exec.Cmd
		output bytes.Buffer
		done   chan error
	}
	children := make([]child, 2)
	for id := range children {
		c := &children[id]
		c.cmd = exec.CommandContext(ctx, executable, "-test.run=^TestWatchLogProcess$")
		c.cmd.Env = append(os.Environ(), "MONOBOX_LOG_TEST_ROOT="+root, fmt.Sprintf("MONOBOX_LOG_TEST_ID=%d", id))
		c.cmd.Stdout, c.cmd.Stderr = &c.output, &c.output
		if err := c.cmd.Start(); err != nil {
			t.Fatal(err)
		}
		c.done = make(chan error, 1)
		go func() { c.done <- c.cmd.Wait() }()
	}
	for i := range children {
		c := &children[i]
		if err := <-c.done; err != nil {
			t.Fatalf("child: %v, %s", err, c.output.String())
		}
	}
	checkWatchLogRecords(t, root)
}

func TestWatchLogRespectsFlock(t *testing.T) {
	root := t.TempDir()
	w := testWatchLog(t, root)
	if err := syscall.Flock(int(w.file.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(w.file.Fd()), syscall.LOCK_UN)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestWatchLogProcess$")
	cmd.Env = append(os.Environ(), "MONOBOX_LOG_TEST_ROOT="+root, "MONOBOX_LOG_TEST_ID=0")
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, readErr := bufio.NewReader(output).ReadString('\n')
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	if readErr != nil || line != "ready\n" {
		t.Fatalf("child did not reach open: %q, %v", line, readErr)
	}
	select {
	case err := <-done:
		t.Fatalf("child bypassed the held flock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if data := readWatchLog(t, root); len(data) != 0 {
		t.Fatal("child wrote while another process held flock")
	}
	if err := syscall.Flock(int(w.file.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("child did not finish after flock was released")
	}
	checkWatchLogRecords(t, root)
}

func TestWatchLogRejectsUnsafePaths(t *testing.T) {
	for _, kind := range []string{"file symlink", "run symlink", "root symlink", "ancestor symlink", "hard link", "directory", "fifo", "run file"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "root")
			runDir := filepath.Join(root, "run")
			if err := os.MkdirAll(runDir, 0700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(parent, "target")
			sentinel := []byte("do not touch\n")
			if err := os.WriteFile(target, sentinel, 0644); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(runDir, "watch.log")
			var err error
			switch kind {
			case "file symlink":
				err = os.Symlink(target, path)
			case "run symlink":
				if err = os.Remove(runDir); err == nil {
					err = os.Symlink(parent, runDir)
				}
			case "root symlink", "ancestor symlink":
				alias := filepath.Join(parent, "alias")
				err = os.Symlink(root, alias)
				root = alias
				if kind == "ancestor symlink" {
					root = filepath.Join(alias, "child")
				}
			case "hard link":
				err = os.Link(target, path)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "fifo":
				err = syscall.Mkfifo(path, 0600)
			case "run file":
				if err = os.Remove(runDir); err == nil {
					err = os.WriteFile(runDir, sentinel, 0600)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if w, err := openWatchLog(root); err == nil {
				w.Close()
				t.Fatal("accepted unsafe path")
			}
			got, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(got, sentinel) {
				t.Fatalf("target modified: %q, %v", got, err)
			}
			info, err := os.Stat(target)
			if err != nil || info.Mode().Perm() != 0644 {
				t.Fatalf("target permissions changed: %v, %v", info, err)
			}
			if _, err := os.Stat(filepath.Join(parent, "watch.log")); !os.IsNotExist(err) {
				t.Fatalf("created a log outside root: %v", err)
			}
		})
	}
}

func TestWatchLogClose(t *testing.T) {
	w, err := openWatchLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("after close")); err == nil {
		t.Fatal("write after close succeeded")
	}
}
