//go:build linux || android

package base

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ServiceIntent is read and changed only while holding service.lock.
// Revision changes on every explicit lifecycle attempt, including failed starts.
type ServiceIntent struct {
	BootID      string `json:"bootId,omitempty"`
	Running     bool   `json:"running"`
	Revision    uint64 `json:"revision"`
	AttemptedAt int64  `json:"attemptedAt"`
}

func ReadPrivateJSON(path string, value any, limit int64) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	f, err := OpenRegular(path, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return errors.New("JSON file exceeds size limit")
	}
	return json.Unmarshal(data, value)
}

// WritePrivateJSON pins the parent and rejects unsafe existing targets.
func WritePrivateJSON(path string, value any, limit int64) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return errors.New("JSON file exceeds size limit")
	}
	dir, err := OpenDirectory(filepath.Dir(path), true)
	if err != nil {
		return err
	}
	defer dir.Close()
	f, err := OpenRegularAt(dir, filepath.Base(path), os.O_RDONLY, 0)
	if err == nil {
		f.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return AtomicWrite(filepath.Join("/proc/self/fd", strconv.Itoa(int(dir.Fd())), filepath.Base(path)), data, 0600)
}

func (r *Runtime) ReadServiceIntent() (ServiceIntent, error) {
	var intent ServiceIntent
	err := ReadPrivateJSON(filepath.Join(r.RunDir(), "service-intent.json"), &intent, 4096)
	if errors.Is(err, os.ErrNotExist) {
		return intent, nil
	}
	if err == nil && (intent.Revision == 0 || intent.AttemptedAt <= 0 || (intent.BootID != "" && !validBootID(intent.BootID))) {
		err = errors.New("invalid service intent")
	}
	return intent, err
}

// CurrentBootID identifies the kernel boot across watcher and CLI restarts.
func CurrentBootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", errors.New("read system boot ID failed")
	}
	id := strings.ToLower(strings.TrimSpace(string(data)))
	if !validBootID(id) {
		return "", errors.New("invalid system boot ID")
	}
	return id, nil
}

func validBootID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if id[i] != '-' {
				return false
			}
		} else if !(id[i] >= '0' && id[i] <= '9' || id[i] >= 'a' && id[i] <= 'f') {
			return false
		}
	}
	return true
}

func (r *Runtime) WriteServiceIntent(running bool) (ServiceIntent, error) {
	intent, err := r.ReadServiceIntent()
	if err != nil {
		return intent, err
	}
	bootID, err := CurrentBootID()
	if err != nil {
		return intent, err
	}
	intent.BootID = bootID
	intent.Running, intent.AttemptedAt = running, time.Now().UnixMilli()
	intent.Revision++
	if intent.Revision == 0 {
		return intent, errors.New("service intent revision overflow")
	}
	return intent, WritePrivateJSON(filepath.Join(r.RunDir(), "service-intent.json"), intent, 4096)
}
