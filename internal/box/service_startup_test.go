//go:build linux || android

package box

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestServiceArgumentReadiness(t *testing.T) {
	loc := serviceLocation{binary: "/box/bin/mihomo", core: "/box/mihomo"}
	valid := []string{loc.binary, "-d", loc.core, "-f", loc.core + "/config.yaml"}
	for _, tc := range []struct {
		name, data     string
		valid, pending bool
	}{
		{"exec has not populated argv", "", false, true},
		{"single argv0", loc.binary + "\x00", false, false},
		{"empty argv0", "\x00", false, false},
		{"extra NUL", loc.binary + "\x00\x00", false, false},
		{"legacy argv", strings.Join(valid[:3], "\x00") + "\x00", true, false},
		{"full argv", strings.Join(valid, "\x00") + "\x00", true, false},
		{"profile", strings.Join([]string{loc.binary, "-d", loc.core, "-f", loc.core + "/.mono-box/profiles/daily.yaml"}, "\x00") + "\x00", true, false},
		{"wrong root", strings.Join([]string{loc.binary, "-d", "/other/mihomo"}, "\x00") + "\x00", false, false},
		{"wrong flag", strings.Join([]string{loc.binary, "-x", loc.core}, "\x00") + "\x00", false, false},
		{"wrong config", strings.Join([]string{loc.binary, "-d", loc.core, "-f", "/other/config.yaml"}, "\x00") + "\x00", false, false},
		{"extra argument", strings.Join(append(append([]string{}, valid...), "extra"), "\x00") + "\x00", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := serviceVerifyArguments(loc, []byte(tc.data))
			if (err == nil) != tc.valid || errors.Is(err, errServiceArgumentsPending) != tc.pending {
				t.Fatalf("readiness error: %v", err)
			}
			if !tc.valid && !errors.Is(err, ErrStopped) {
				t.Fatalf("lost process validation failure: %v", err)
			}
		})
	}
}

func TestStartupWaitsOnlyForEmptyArguments(t *testing.T) {
	done := make(chan error)
	calls := 0
	err := serviceAwaitStartupArguments(time.Now().Add(time.Second), done, func() error {
		calls++
		if calls <= 2 {
			return serviceVerifyArguments(serviceLocation{}, nil)
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("transient empty argv: %d calls, %v", calls, err)
	}
	for _, failure := range []error{syscall.EACCES, fmt.Errorf("executable changed: %w", ErrStopped), serviceVerifyArguments(serviceLocation{binary: "/core"}, []byte("/core\x00"))} {
		calls = 0
		err = serviceAwaitStartupArguments(time.Now().Add(time.Second), done, func() error { calls++; return failure })
		if calls != 1 || !errors.Is(err, failure) {
			t.Fatalf("retried non-transient failure: %d calls, %v", calls, err)
		}
	}
}

func TestStartupEmptyArgumentsTimeoutAndExit(t *testing.T) {
	pending := func() error { return serviceVerifyArguments(serviceLocation{}, nil) }
	started := time.Now()
	err := serviceAwaitStartupArguments(started.Add(35*time.Millisecond), make(chan error), pending)
	if !errors.Is(err, errServiceArgumentsPending) || !strings.Contains(err.Error(), "timed out") || time.Since(started) > time.Second {
		t.Fatalf("argv timeout: %v", err)
	}
	for _, failure := range []error{nil, errors.New("exit status 23")} {
		done := make(chan error, 1)
		done <- failure
		close(done)
		err := serviceAwaitStartupArguments(time.Now().Add(time.Second), done, pending)
		if err == nil || !strings.Contains(err.Error(), "core exited") {
			t.Fatalf("child exit treated as readiness: %v", err)
		}
		if failure != nil && !errors.Is(err, failure) {
			t.Fatalf("child exit reason lost: %v", err)
		}
	}
}
