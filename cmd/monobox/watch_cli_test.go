package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Run watch cases in a child with a deadline, so a regression cannot leave the
// test suite polling forever or install signal handlers in the test process.
func TestCLIWatchProcess(t *testing.T) {
	root := os.Getenv("MONOBOX_CLI_WATCH_TEST_ROOT")
	if root == "" {
		return
	}
	args := []string{"--root", root, "--module-dir", filepath.Join(root, "module"), "watch"}
	if os.Getenv("MONOBOX_CLI_WATCH_TEST_EXTRA") != "" {
		args = append(args, "extra")
	}
	os.Exit(run(args, os.Stdout, os.Stderr))
}

func invokeTestWatch(t *testing.T, root string, extra bool) (int, string, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestCLIWatchProcess$")
	cmd.Env = append(os.Environ(), "MONOBOX_CLI_WATCH_TEST_ROOT="+root)
	if extra {
		cmd.Env = append(cmd.Env, "MONOBOX_CLI_WATCH_TEST_EXTRA=1")
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("watch failed to exit: %v, %s", ctx.Err(), errOut.String())
	}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), out.String(), errOut.String()
		}
		t.Fatal(err)
	}
	return 0, out.String(), errOut.String()
}

func TestCLIWatchLogsInitializationErrors(t *testing.T) {
	for _, kind := range []string{"invalid config", "config read error", "argument error", "duplicate watcher"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			config := filepath.Join(root, "scripts", "box.config")
			if err := os.MkdirAll(filepath.Dir(config), 0700); err != nil {
				t.Fatal(err)
			}
			want := ""
			switch kind {
			case "invalid config":
				if err := os.WriteFile(config, []byte("clash_api_port=invalid\n"), 0600); err != nil {
					t.Fatal(err)
				}
				want = "invalid clash_api_port"
			case "config read error":
				if err := os.Mkdir(config, 0700); err != nil {
					t.Fatal(err)
				}
				want = "is a directory"
			case "argument error":
				want = "watch takes no arguments"
			case "duplicate watcher":
				if err := os.Mkdir(filepath.Join(root, "run"), 0700); err != nil {
					t.Fatal(err)
				}
				lock, err := os.OpenFile(filepath.Join(root, "run", "watcher.lock"), os.O_CREATE|os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
				if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
					t.Fatal(err)
				}
				want = "watcher already running"
			}
			code, out, errOut := invokeTestWatch(t, root, kind == "argument error")
			if code != 1 || out != "" || !strings.Contains(errOut, want) {
				t.Fatalf("exit %d, stdout %q, stderr %q; want %q", code, out, errOut, want)
			}
			if got := string(readWatchLog(t, root)); got != errOut {
				t.Fatalf("log %q differs from visible stderr %q", got, errOut)
			}
		})
	}
}

func TestCLIWatchLogFailureIsVisible(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "run"), 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("untouched"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "run", "watch.log")); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := invokeTestWatch(t, root, false)
	if code != 1 || !strings.Contains(errOut, "Error: watch log:") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "untouched" {
		t.Fatalf("unsafe target modified: %q, %v", got, err)
	}
}

func TestCLIOtherCommandsDoNotCreateWatchLog(t *testing.T) {
	root := t.TempDir()
	var out, errOut bytes.Buffer
	if code := run([]string{"--root", root, "--module-dir", t.TempDir(), "controller"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(root, "run", "watch.log")); !os.IsNotExist(err) {
		t.Fatalf("non-watch command created watch log: %v", err)
	}
}
