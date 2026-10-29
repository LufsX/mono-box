package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/LufsX/mono-box/internal/box/base"
)

const (
	watchLogMaxSize  = 1 << 20
	watchLogTailSize = watchLogMaxSize / 2
)

type watchLogWriter struct {
	mu   sync.Mutex
	file *os.File
}

func openWatchLog(root string) (*watchLogWriter, error) {
	dir, err := base.OpenDirectory(filepath.Join(root, "run"), true)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	file, err := base.OpenRegularAt(dir, "watch.log", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	w := &watchLogWriter{file: file}
	err = w.locked(func() error {
		if err := file.Chmod(0600); err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if info.Size() > watchLogMaxSize {
			return w.compact(info.Size(), watchLogTailSize)
		}
		return nil
	})
	if err != nil {
		file.Close()
		return nil, err
	}
	return w, nil
}

func watchLogFlock(file *os.File, operation int) error {
	for {
		err := syscall.Flock(int(file.Fd()), operation)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

// flock serializes separate opens and processes. The mutex also serializes
// goroutines sharing this descriptor, for which flock alone would not suffice.
func (w *watchLogWriter) locked(fn func() error) (err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return os.ErrClosed
	}
	if err := watchLogFlock(w.file, syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, watchLogFlock(w.file, syscall.LOCK_UN))
	}()
	return fn()
}

// Skip the initial partial line when a later line is available. A single line
// longer than the budget still keeps its latest bytes.
func watchLogTail(data []byte, partial bool) []byte {
	if partial {
		if end := bytes.IndexByte(data, '\n'); end >= 0 && end+1 < len(data) {
			return data[end+1:]
		}
	}
	return data
}

// compact rewrites the same inode; other watch processes keep valid descriptors.
func (w *watchLogWriter) compact(size int64, keep int) error {
	if int64(keep) > size {
		keep = int(size)
	}
	offset := size - int64(keep)
	data := make([]byte, keep)
	if _, err := w.file.ReadAt(data, offset); err != nil {
		return err
	}
	if offset > 0 && len(data) > 0 {
		var previous [1]byte
		if _, err := w.file.ReadAt(previous[:], offset-1); err != nil {
			return err
		}
		data = watchLogTail(data, previous[0] != '\n')
	}
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := w.file.Write(data); err != nil {
		return err
	}
	return w.file.Truncate(int64(len(data)))
}

func (w *watchLogWriter) Write(p []byte) (int, error) {
	inputSize := len(p)
	accepted := 0
	err := w.locked(func() error {
		if len(p) == 0 {
			return nil
		}
		info, err := w.file.Stat()
		if err != nil {
			return err
		}
		if len(p) > watchLogMaxSize {
			offset := len(p) - watchLogTailSize
			p = watchLogTail(p[offset:], p[offset-1] != '\n')
			if err := w.compact(info.Size(), 0); err != nil {
				return err
			}
		} else if info.Size() > int64(watchLogMaxSize-len(p)) {
			keep := min(watchLogTailSize, watchLogMaxSize-len(p))
			if err := w.compact(info.Size(), keep); err != nil {
				return err
			}
		}
		// Each descriptor has its own offset, which may be stale after another
		// writer compacted the file. Seek under flock before every append.
		if _, err := w.file.Seek(0, io.SeekEnd); err != nil {
			return err
		}
		n, err := w.file.Write(p)
		accepted = inputSize - len(p) + n
		return err
	})
	if err != nil {
		return accepted, err
	}
	// Discarding older bytes is intentional and must not cause a short write
	// in io.MultiWriter, which also forwards the full input to the terminal.
	return inputSize, nil
}

func (w *watchLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return os.ErrClosed
	}
	err := w.file.Close()
	w.file = nil
	return err
}
