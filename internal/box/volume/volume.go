package volume

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ErrVolumeTimeout means no volume key was pressed before the deadline.
var ErrVolumeTimeout = errors.New("volume key timeout")

// ChooseVolume writes "up" or "down" when a volume key is pressed.
func (r *Runtime) ChooseVolume(timeout time.Duration) error {
	sources, err := volumeSources("/dev/input", "/sys/class/input")
	if err != nil {
		return err
	}
	return r.chooseVolume(sources, timeout, strconv.IntSize/8, time.Now, time.Sleep)
}

func (r *Runtime) chooseVolume(sources []string, timeout time.Duration, wordBytes int, now func() time.Time, sleep func(time.Duration)) error {
	choice, err := chooseVolume(sources, timeout, wordBytes, now, sleep)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(r.Out, choice)
	return err
}

func volumeSources(devDir, sysDir string) ([]string, error) {
	all, err := filepath.Glob(filepath.Join(devDir, "event*"))
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no input devices in %s", devDir)
	}
	var preferred, unknown []string
	for _, path := range all {
		bits, err := os.ReadFile(filepath.Join(sysDir, filepath.Base(path), "device/capabilities/key"))
		if err != nil {
			unknown = append(unknown, path)
			continue
		}
		hasVolume, valid := volumeCapability(string(bits))
		if !valid {
			unknown = append(unknown, path)
		} else if hasVolume {
			preferred = append(preferred, path)
		}
	}
	if len(preferred) != 0 {
		return append(preferred, unknown...), nil
	}
	return all, nil
}

// Sysfs words use the kernel's long size, which can differ from userspace.
// Accept either layout so a 32-bit executable also finds keys on a 64-bit kernel.
func volumeCapability(bitmap string) (hasVolume, valid bool) {
	fields := strings.Fields(bitmap)
	if len(fields) == 0 {
		return false, false
	}
	words := make([]uint64, len(fields))
	for i, field := range fields {
		word, err := strconv.ParseUint(field, 16, 64)
		if err != nil {
			return false, false
		}
		words[len(fields)-1-i] = word
	}
	for _, width := range []uint{32, 64} {
		if width == 32 {
			fits := true
			for _, word := range words {
				fits = fits && word <= 0xffffffff
			}
			if !fits {
				continue
			}
		}
		for _, key := range []uint{114, 115} {
			index := int(key / width)
			if index < len(words) && words[index]&(uint64(1)<<(key%width)) != 0 {
				hasVolume = true
			}
		}
	}
	return hasVolume, true
}

type volumeSource struct {
	fd      int
	path    string
	pending []byte
}

// chooseVolume owns every fd it opens. Raw nonblocking reads avoid Go's poller
// waiting past the deadline; each device gets one bounded read per polling pass.
func chooseVolume(paths []string, timeout time.Duration, wordBytes int, now func() time.Time, sleep func(time.Duration)) (string, error) {
	if wordBytes != 4 && wordBytes != 8 {
		return "", fmt.Errorf("unsupported input_event word size: %d", wordBytes)
	}
	deadline := now().Add(timeout)
	size := 2*wordBytes + 8
	var sources []volumeSource
	var failures []error
	for _, path := range paths {
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if err != nil {
			failures = append(failures, fmt.Errorf("open %s: %w", path, err))
			continue
		}
		sources = append(sources, volumeSource{fd: fd, path: path})
	}
	defer func() {
		for _, source := range sources {
			if source.fd >= 0 {
				syscall.Close(source.fd)
			}
		}
	}()
	if len(sources) == 0 {
		return "", fmt.Errorf("no readable input devices: %w", errors.Join(append(failures, os.ErrNotExist)...))
	}

	selected, selectedKey, choice := -1, uint16(0), ""
	buffer := make([]byte, size*32)
	for {
		if !now().Before(deadline) {
			if choice != "" {
				return choice, nil
			}
			return "", ErrVolumeTimeout
		}
		active := 0
		for i := range sources {
			source := &sources[i]
			if source.fd < 0 || (selected >= 0 && selected != i) {
				continue
			}
			active++
			n, readErr := syscall.Read(source.fd, buffer)
			if n > 0 {
				source.pending = append(source.pending, buffer[:n]...)
			}
			for len(source.pending) >= size {
				event := source.pending[:size]
				source.pending = source.pending[size:]
				offset := 2 * wordBytes
				kind := binary.NativeEndian.Uint16(event[offset:])
				key := binary.NativeEndian.Uint16(event[offset+2:])
				value := binary.NativeEndian.Uint32(event[offset+4:])
				if kind != 1 || (key != 114 && key != 115) {
					continue
				}
				if selected >= 0 {
					if key == selectedKey && value == 0 {
						return choice, nil
					}
					continue
				}
				if value != 1 {
					continue
				}
				selected, selectedKey, choice = i, key, "down"
				if key == 115 {
					choice = "up"
				}
				if releaseDeadline := now().Add(500 * time.Millisecond); releaseDeadline.Before(deadline) {
					deadline = releaseDeadline
				}
			}
			if readErr == syscall.EAGAIN || readErr == syscall.EWOULDBLOCK || readErr == syscall.EINTR {
				continue
			}
			if readErr == nil && n > 0 {
				continue
			}
			if choice != "" && selected == i {
				return choice, nil
			}
			if readErr == nil {
				readErr = io.EOF
				if len(source.pending) != 0 {
					readErr = io.ErrUnexpectedEOF
				}
			}
			failures = append(failures, fmt.Errorf("read %s: %w", source.path, readErr))
			syscall.Close(source.fd)
			source.fd = -1
			active--
		}
		if active == 0 {
			return "", fmt.Errorf("input devices unavailable: %w", errors.Join(failures...))
		}
		if remaining := deadline.Sub(now()); remaining > 0 {
			sleep(min(20*time.Millisecond, remaining))
		}
	}
}
