package service

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestProcessExitErrors(t *testing.T) {
	for _, err := range []error{syscall.ENOENT, syscall.ESRCH, &os.PathError{Op: "read", Path: "/proc/123/stat", Err: syscall.ESRCH}} {
		if !serviceProcessMissing(err) {
			t.Fatalf("not classified as exited: %v", err)
		}
	}
	for _, err := range []error{nil, syscall.EPERM, syscall.EACCES, errors.New("I/O error")} {
		if serviceProcessMissing(err) {
			t.Fatalf("lost real error: %v", err)
		}
	}
}
