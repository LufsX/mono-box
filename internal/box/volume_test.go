package box

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func volumeTestEvent(wordBytes int, kind, key uint16, value uint32) []byte {
	event := make([]byte, wordBytes*2+8)
	// Nonzero timeval fields catch decoders that start at the wrong offset.
	for i := 0; i < wordBytes*2; i++ {
		event[i] = 0xa5
	}
	binary.NativeEndian.PutUint16(event[wordBytes*2:], kind)
	binary.NativeEndian.PutUint16(event[wordBytes*2+2:], key)
	binary.NativeEndian.PutUint32(event[wordBytes*2+4:], value)
	return event
}

func volumeTestFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "event0")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func volumeTestPipe(t *testing.T) (string, *os.File) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close(); writer.Close() })
	return fmt.Sprintf("/proc/self/fd/%d", reader.Fd()), writer
}

func volumeTestWrite(t *testing.T, writer *os.File, data []byte) {
	t.Helper()
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
}

type volumeTestClock struct {
	elapsed time.Duration
	tick    func()
}

func (c *volumeTestClock) now() time.Time { return time.Unix(100, 0).Add(c.elapsed) }
func (c *volumeTestClock) sleep(d time.Duration) {
	c.elapsed += d
	if c.tick != nil {
		c.tick()
	}
}

type volumeTestFailedWriter struct{}

func (volumeTestFailedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestVolumeOutput(t *testing.T) {
	for _, key := range []uint16{114, 115} {
		data := append(volumeTestEvent(8, 1, key, 1), volumeTestEvent(8, 1, key, 0)...)
		source := volumeTestFile(t, data)
		var output bytes.Buffer
		r := &Runtime{Out: &output}
		clock := &volumeTestClock{}
		err := r.chooseVolume([]string{source}, time.Second, 8, clock.now, clock.sleep)
		want := "down\n"
		if key == 115 {
			want = "up\n"
		}
		if err != nil || output.String() != want {
			t.Fatalf("output %q, %v; want %q", output.String(), err, want)
		}
		r.Out = volumeTestFailedWriter{}
		if err := r.chooseVolume([]string{source}, time.Second, 8, clock.now, clock.sleep); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("output error: %v", err)
		}
	}
	source, _ := volumeTestPipe(t)
	var output bytes.Buffer
	r := &Runtime{Out: &output}
	clock := &volumeTestClock{}
	err := r.chooseVolume([]string{source}, time.Millisecond, 8, clock.now, clock.sleep)
	if !errors.Is(err, ErrVolumeTimeout) || output.Len() != 0 {
		t.Fatalf("timeout output %q, %v", output.String(), err)
	}
}

func TestVolumeEvents(t *testing.T) {
	for _, wordBytes := range []int{4, 8} {
		for _, key := range []uint16{114, 115} {
			t.Run(fmt.Sprintf("%d-bit/key%d", wordBytes*8, key), func(t *testing.T) {
				var data []byte
				for _, event := range [][]byte{
					volumeTestEvent(wordBytes, 0, key, 1),   // SYN
					volumeTestEvent(wordBytes, 2, key, 1),   // wrong type
					volumeTestEvent(wordBytes, 1, 30, 1),    // unrelated key
					volumeTestEvent(wordBytes, 1, key, 0),   // release
					volumeTestEvent(wordBytes, 1, key, 2),   // repeat
					volumeTestEvent(wordBytes, 1, key, 255), // invalid value
					volumeTestEvent(wordBytes, 1, key, 1),
					volumeTestEvent(wordBytes, 1, key, 0),
				} {
					data = append(data, event...)
				}
				clock := &volumeTestClock{}
				got, err := chooseVolume([]string{volumeTestFile(t, data)}, time.Second, wordBytes, clock.now, clock.sleep)
				want := "down"
				if key == 115 {
					want = "up"
				}
				if err != nil || got != want || clock.elapsed != 0 {
					t.Fatalf("got %q, %v after %s; want %q immediately", got, err, clock.elapsed, want)
				}
			})
		}
	}
}

func TestVolumeSplitFramesAndMultipleDevices(t *testing.T) {
	for _, wordBytes := range []int{4, 8} {
		t.Run(fmt.Sprint(wordBytes), func(t *testing.T) {
			idle, idleWriter := volumeTestPipe(t)
			source, writer := volumeTestPipe(t)
			// A partial frame on one device must not contaminate another.
			volumeTestWrite(t, idleWriter, volumeTestEvent(wordBytes, 1, 115, 1)[:7])
			press := volumeTestEvent(wordBytes, 1, 114, 1)
			chunks := [][]byte{press[:1], press[1:9], press[9:], volumeTestEvent(wordBytes, 1, 114, 0)}
			clock := &volumeTestClock{}
			clock.tick = func() {
				if len(chunks) > 0 {
					volumeTestWrite(t, writer, chunks[0])
					chunks = chunks[1:]
				}
			}
			got, err := chooseVolume([]string{idle, source}, time.Second, wordBytes, clock.now, clock.sleep)
			if err != nil || got != "down" || clock.elapsed != 80*time.Millisecond {
				t.Fatalf("got %q, %v after %s", got, err, clock.elapsed)
			}
		})
	}
}

func TestVolumeReleaseBound(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		wait    time.Duration
	}{
		{"held", time.Second, 500 * time.Millisecond},
		{"total-deadline", 65 * time.Millisecond, 65 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, writer := volumeTestPipe(t)
			volumeTestWrite(t, writer, volumeTestEvent(8, 1, 115, 1))
			clock := &volumeTestClock{}
			got, err := chooseVolume([]string{source}, tc.timeout, 8, clock.now, clock.sleep)
			if err != nil || got != "up" || clock.elapsed != tc.wait {
				t.Fatalf("got %q, %v after %s; want wait %s", got, err, clock.elapsed, tc.wait)
			}
		})
	}
}

func TestVolumeReleaseMustMatchDeviceAndKey(t *testing.T) {
	first, writer := volumeTestPipe(t)
	second, otherWriter := volumeTestPipe(t)
	volumeTestWrite(t, writer, volumeTestEvent(8, 1, 115, 1))
	clock := &volumeTestClock{}
	clock.tick = func() {
		switch clock.elapsed {
		case 20 * time.Millisecond:
			volumeTestWrite(t, otherWriter, volumeTestEvent(8, 1, 115, 0))
			volumeTestWrite(t, writer, volumeTestEvent(8, 1, 114, 0))
		case 40 * time.Millisecond:
			volumeTestWrite(t, writer, volumeTestEvent(8, 1, 115, 2))
		case 60 * time.Millisecond:
			volumeTestWrite(t, writer, volumeTestEvent(8, 1, 115, 0))
		}
	}
	got, err := chooseVolume([]string{first, second}, time.Second, 8, clock.now, clock.sleep)
	if err != nil || got != "up" || clock.elapsed != 60*time.Millisecond {
		t.Fatalf("got %q, %v after %s", got, err, clock.elapsed)
	}
}

func TestVolumeTimeout(t *testing.T) {
	source, writer := volumeTestPipe(t)
	volumeTestWrite(t, writer, bytes.Join([][]byte{
		volumeTestEvent(8, 0, 0, 0),
		volumeTestEvent(8, 1, 115, 0),
		volumeTestEvent(8, 1, 114, 2),
	}, nil))
	clock := &volumeTestClock{}
	got, err := chooseVolume([]string{source}, 45*time.Millisecond, 8, clock.now, clock.sleep)
	if got != "" || !errors.Is(err, ErrVolumeTimeout) || clock.elapsed != 45*time.Millisecond {
		t.Fatalf("got %q, %v after %s", got, err, clock.elapsed)
	}
	// Also exercise a real idle pipe: a blocking read would hang this test.
	start := time.Now()
	_, err = chooseVolume([]string{source}, 45*time.Millisecond, 8, time.Now, time.Sleep)
	if !errors.Is(err, ErrVolumeTimeout) || time.Since(start) > time.Second {
		t.Fatalf("real timeout: %v after %s", err, time.Since(start))
	}
}

func TestVolumeDeviceErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		path func(*testing.T) string
		want error
	}{
		{"open", func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") }, os.ErrNotExist},
		{"read", func(t *testing.T) string { return t.TempDir() }, syscall.EISDIR},
		{"eof", func(t *testing.T) string { return volumeTestFile(t, nil) }, io.EOF},
		{"partial-eof", func(t *testing.T) string { return volumeTestFile(t, []byte{1, 2}) }, io.ErrUnexpectedEOF},
		{"unplug", func(t *testing.T) string {
			path, writer := volumeTestPipe(t)
			writer.Close()
			return path
		}, io.EOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := &volumeTestClock{}
			path := tc.path(t)
			_, err := chooseVolume([]string{path}, time.Second, 8, clock.now, clock.sleep)
			if !errors.Is(err, tc.want) || errors.Is(err, ErrVolumeTimeout) {
				t.Fatalf("got %v; want %v", err, tc.want)
			}
			// One failed device must not prevent selection on another.
			data := append(volumeTestEvent(8, 1, 114, 1), volumeTestEvent(8, 1, 114, 0)...)
			got, err := chooseVolume([]string{path, volumeTestFile(t, data)}, time.Second, 8, clock.now, clock.sleep)
			if err != nil || got != "down" {
				t.Fatalf("surviving device: %q, %v", got, err)
			}
		})
	}
}

func TestVolumeClosesDescriptors(t *testing.T) {
	source, _ := volumeTestPipe(t)
	count := func() int {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}
	data := append(volumeTestEvent(8, 1, 114, 1), volumeTestEvent(8, 1, 114, 0)...)
	success := volumeTestFile(t, data)
	empty, directory := volumeTestFile(t, nil), t.TempDir()
	before := count()
	for i := 0; i < 20; i++ {
		for _, paths := range [][]string{{source, directory}, {success, source}, {empty, directory}} {
			clock := &volumeTestClock{}
			chooseVolume(paths, time.Millisecond, 8, clock.now, clock.sleep)
		}
	}
	if after := count(); after != before {
		t.Fatalf("fd count: before %d, after %d", before, after)
	}
}

func TestVolumeCapability(t *testing.T) {
	for _, tc := range []struct {
		bitmap string
		has    bool
		valid  bool
	}{
		{"c000000000000 0", true, true}, // 64-bit kernel, keys 114 and 115
		{"4000000000000 0", true, true},
		{"8000000000000 0", true, true},
		{"c0000 0 0 0", true, true}, // 32-bit kernel
		{"80000 0 0 0", true, true},
		{"0", false, true},
		{"3 0", false, true},
		{"", false, false},
		{"not-hex", false, false},
		{"10000000000000000", false, false},
	} {
		has, valid := volumeCapability(tc.bitmap)
		if has != tc.has || valid != tc.valid {
			t.Errorf("%q: got %v/%v, want %v/%v", tc.bitmap, has, valid, tc.has, tc.valid)
		}
	}
}

func TestVolumeSources(t *testing.T) {
	root := t.TempDir()
	dev, sys := filepath.Join(root, "dev"), filepath.Join(root, "sys")
	if err := os.MkdirAll(dev, 0700); err != nil {
		t.Fatal(err)
	}
	for i, bits := range []string{"0", "4000000000000 0", "8000000000000 0", "unknown"} {
		name := fmt.Sprintf("event%d", i)
		if err := os.WriteFile(filepath.Join(dev, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if i == 3 {
			continue // unreadable sysfs device remains eligible
		}
		path := filepath.Join(sys, name, "device/capabilities/key")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(bits), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := volumeSources(dev, sys)
	want := []string{filepath.Join(dev, "event1"), filepath.Join(dev, "event2"), filepath.Join(dev, "event3")}
	if err != nil || fmt.Sprint(sources) != fmt.Sprint(want) {
		t.Fatalf("preferred devices: %v, %v", sources, err)
	}
	sources, err = volumeSources(dev, filepath.Join(root, "missing"))
	if err != nil || len(sources) != 4 {
		t.Fatalf("fallback devices: %v, %v", sources, err)
	}
	for _, name := range []string{"event1", "event2"} {
		if err := os.WriteFile(filepath.Join(sys, name, "device/capabilities/key"), []byte("0"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sources, err = volumeSources(dev, sys)
	if err != nil || len(sources) != 4 {
		t.Fatalf("no volume capability fallback: %v, %v", sources, err)
	}
	if _, err := volumeSources(t.TempDir(), sys); err == nil {
		t.Fatal("expected no-devices error")
	}
}
