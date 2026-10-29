package automation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func fixture(t *testing.T) *Runtime {
	t.Helper()
	return New(&base.Runtime{Root: t.TempDir(), Config: base.DefaultConfig()})
}

func setJSON(t *testing.T, r *Runtime, data string) State {
	t.Helper()
	out := new(bytes.Buffer)
	r.Out = out
	if err := r.Command([]string{"set", base64.StdEncoding.EncodeToString([]byte(data))}); err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(out.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestSettingsContractDefaultsValidationAndPrivatePersistence(t *testing.T) {
	r := fixture(t)
	state, err := r.Get()
	if err != nil || state.Settings != defaultSettings() || state.Recovery.State != "disabled" || state.Update.State != "disabled" {
		t.Fatalf("%+v %v", state, err)
	}
	if _, err := os.Stat(filepath.Join(r.Root, "scripts", "box.config")); !os.IsNotExist(err) {
		t.Fatalf("get created config: %v", err)
	}
	setJSON(t, r, "{}")
	for _, name := range []string{"box.config"} {
		info, err := os.Stat(filepath.Join(r.Root, "scripts", name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("%v %v", info, err)
		}
	}
	for _, input := range []string{"null", "[]", `{"updateIntervalHours":0}`, `{"updateIntervalHours":169}`, `{"updateIntervalHours":1.5}`, `{"recoveryEnabled":"true"}`, `{"updateWifiOnly":null}`, `{"unknown":true}`} {
		if err := r.Command([]string{"set", base64.StdEncoding.EncodeToString([]byte(input))}); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	state = setJSON(t, r, `{"recoveryEnabled":true,"updateEnabled":true,"updateIntervalHours":1,"updateWifiOnly":false}`)
	if state.Update.NextRunAt < time.Now().Add(59*time.Minute).UnixMilli() {
		t.Fatalf("first update ran early: %+v", state)
	}
	reopened, err := New(r.Runtime).Get()
	if err != nil || reopened != state {
		t.Fatalf("%+v %v", reopened, err)
	}
	info, err := os.Stat(filepath.Join(r.RunDir(), "automation-status.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("%v %v", info, err)
	}
}

func TestIdlePollingAndGetDoNotReplaceStatus(t *testing.T) {
	r := fixture(t)
	setJSON(t, r, `{"updateEnabled":true}`)
	path := filepath.Join(r.RunDir(), "automation-status.json")
	before, _ := os.Stat(path)
	configPath := filepath.Join(r.Root, "scripts", "box.config")
	configBefore, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := r.Get(); err != nil {
			t.Fatal(err)
		}
		if _, err := r.snapshot(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := os.Stat(path)
	if !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
		t.Fatal("idle polling wrote status")
	}
	configAfter, err := os.Stat(configPath)
	if err != nil || !os.SameFile(configBefore, configAfter) || configBefore.ModTime() != configAfter.ModTime() {
		t.Fatal("idle polling wrote config")
	}
}

func TestUnsafeAutomationFilesAndOversize(t *testing.T) {
	for _, target := range []string{"scripts/box.config", "run/automation-status.json", "run/config.lock"} {
		t.Run(target, func(t *testing.T) {
			r := fixture(t)
			path := filepath.Join(r.Root, target)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Get(); err == nil {
				t.Fatal("followed unsafe target")
			}
			if err := r.Command([]string{"set", base64.StdEncoding.EncodeToString([]byte("{}"))}); err == nil {
				t.Fatal("replaced unsafe target")
			}
			data, _ := os.ReadFile(outside)
			if string(data) != "{}" {
				t.Fatal("changed outside file")
			}
		})
	}
	r := fixture(t)
	if err := os.MkdirAll(filepath.Join(r.Root, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Root, "scripts", "box.config"), []byte(strings.Repeat(" ", base.ConfigSizeLimit+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(); err == nil {
		t.Fatal("accepted oversized settings")
	}
}

func TestConcurrentSettingsAndPartialStatusReconciliation(t *testing.T) {
	r := fixture(t)
	setJSON(t, r, `{"recoveryEnabled":true}`)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := r.set(Settings{RecoveryEnabled: i%2 == 0, UpdateIntervalHours: i + 1, UpdateWifiOnly: true})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	// Simulate settings successfully replaced, then a crash before status rename.
	settings := defaultSettings()
	settings.UpdateEnabled = true
	writeSettingsConfig(t, r, settings)
	rec, err := r.snapshot(context.Background())
	if err != nil || rec.Settings != settings || rec.Update.NextRunAt == 0 {
		t.Fatalf("%+v %v", rec, err)
	}
	// Deleting settings must restore disabled defaults, not status's old settings.
	if err := os.Remove(filepath.Join(r.Root, "scripts", "box.config")); err != nil {
		t.Fatal(err)
	}
	state, err := r.Get()
	if err != nil || state.Settings != defaultSettings() {
		t.Fatalf("%+v %v", state, err)
	}
}
