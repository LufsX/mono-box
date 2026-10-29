package automation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func writeConfigFixture(t *testing.T, r *Runtime, content string) {
	t.Helper()
	dir := filepath.Join(r.Root, "scripts")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, configName), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeSettingsConfig(t *testing.T, r *Runtime, settings Settings) {
	t.Helper()
	writeConfigFixture(t, r, base.MergeConfigPatch("", settingsPatch(settings)))
}

func readConfigFixture(t *testing.T, r *Runtime) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.Root, "scripts", configName))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSettingsSavePreservesUnrelatedInvalidConfigAndComments(t *testing.T) {
	r := fixture(t)
	original := "# keep this header\n" +
		"bin_name='custom-core'\nCORE_USER_GROUP='custom:group'\n" +
		"clash_api_port=invalid\nclash_api_secret='secret # value' # API comment\n" +
		"custom_field='arbitrary value'\nunrelated broken statement\n"
	writeConfigFixture(t, r, original+
		"export auto_recovery = 'true' # recovery comment\nauto_update_config=true\n"+
		"config_update_interval_hours=168\nconfig_update_wifi_only=false\n")
	state, err := r.Get()
	if err != nil || !state.Settings.RecoveryEnabled || !state.Settings.UpdateEnabled {
		t.Fatalf("%+v %v", state, err)
	}
	if err := os.Chmod(filepath.Join(r.Root, "scripts", configName), 0644); err != nil {
		t.Fatal(err)
	}
	state = setJSON(t, r, "{}")
	if state.Settings != defaultSettings() {
		t.Fatal(state)
	}
	content := readConfigFixture(t, r)
	if !strings.HasPrefix(content, original) || !strings.Contains(content, "export auto_recovery =false # recovery comment\n") {
		t.Fatalf("unrelated config or comments changed: %q", content)
	}
	info, err := os.Stat(filepath.Join(r.Root, "scripts", configName))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("%v %v", info, err)
	}
}

func TestDirectConfigEditsReconcileIndependentGenerationsAndABA(t *testing.T) {
	r := fixture(t)
	settings := Settings{RecoveryEnabled: true, UpdateEnabled: true, UpdateIntervalHours: 2, UpdateWifiOnly: false}
	setJSON(t, r, "{}")
	writeSettingsConfig(t, r, settings)
	initial, err := r.snapshot(context.Background())
	if err != nil || initial.Settings != settings || initial.Update.NextRunAt == 0 {
		t.Fatalf("%+v %v", initial, err)
	}
	_, recoveryGuard := beginAttempt(t, r, false)
	_, updateGuard := beginAttempt(t, r, true)
	settings.RecoveryEnabled = false
	writeSettingsConfig(t, r, settings)
	// Get persists observed changes so disabling and re-enabling between worker
	// polls cannot revive a task from before the change.
	if _, err := r.Get(); err != nil {
		t.Fatal(err)
	}
	settings.RecoveryEnabled = true
	writeSettingsConfig(t, r, settings)
	recoveryChanged, err := r.snapshot(context.Background())
	if err != nil || recoveryChanged.RecoveryGeneration != initial.RecoveryGeneration+2 ||
		recoveryChanged.UpdateGeneration != initial.UpdateGeneration {
		t.Fatalf("%+v %v", recoveryChanged, err)
	}
	called := false
	if err := recoveryGuard(context.Background(), func() error { called = true; return nil }); !errors.Is(err, base.ErrAutomationChanged) || called {
		t.Fatalf("old recovery committed: called=%v err=%v", called, err)
	}
	if err := updateGuard(context.Background(), func() error { return nil }); err != nil {
		t.Fatalf("recovery edit invalidated update: %v", err)
	}
	settings.UpdateIntervalHours = 3
	settings.UpdateWifiOnly = true
	writeSettingsConfig(t, r, settings)
	updateChanged, err := r.snapshot(context.Background())
	if err != nil || updateChanged.Settings != settings ||
		updateChanged.UpdateGeneration != initial.UpdateGeneration+1 ||
		updateChanged.RecoveryGeneration != recoveryChanged.RecoveryGeneration {
		t.Fatalf("%+v %v", updateChanged, err)
	}
	called = false
	if err := updateGuard(context.Background(), func() error { called = true; return nil }); !errors.Is(err, base.ErrAutomationChanged) || called {
		t.Fatalf("old update committed: called=%v err=%v", called, err)
	}
}

func TestUnsafeConfigTypesAndAncestors(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo", "directory"} {
		t.Run(kind, func(t *testing.T) {
			r := fixture(t)
			dir := filepath.Join(r.Root, "scripts")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, configName)
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(outside, path)
			case "hardlink":
				err = os.Link(outside, path)
			case "fifo":
				err = syscall.Mkfifo(path, 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.Get(); err == nil {
				t.Fatal("accepted unsafe config")
			}
			if _, err := r.set(defaultSettings()); err == nil {
				t.Fatal("overwrote unsafe config")
			}
			data, err := os.ReadFile(outside)
			if err != nil || string(data) != "{}" {
				t.Fatalf("outside file changed: %q %v", data, err)
			}
		})
	}
	r := fixture(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(r.Root, "scripts")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(); err == nil {
		t.Fatal("followed scripts symlink")
	}
	if _, err := r.set(defaultSettings()); err == nil {
		t.Fatal("wrote through scripts symlink")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("wrote outside: %v %v", entries, err)
	}
}

func TestConfigOperationsPinParent(t *testing.T) {
	r := fixture(t)
	writeConfigFixture(t, r, "custom=value\n")
	dir, err := base.OpenDirectory(filepath.Join(r.Root, "scripts"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := os.Rename(filepath.Join(r.Root, "scripts"), filepath.Join(r.Root, "original")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideConfig := filepath.Join(outside, configName)
	if err := os.WriteFile(outsideConfig, []byte("outside=value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(r.Root, "scripts")); err != nil {
		t.Fatal(err)
	}
	content, err := readBoxConfig(dir)
	if err != nil || content != "custom=value\n" {
		t.Fatalf("%q %v", content, err)
	}
	if err := writeBoxConfig(dir, base.MergeConfigPatch(content, settingsPatch(defaultSettings()))); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outsideConfig)
	if err != nil || string(data) != "outside=value\n" {
		t.Fatalf("wrote outside pinned parent: %q %v", data, err)
	}
	data, err = os.ReadFile(filepath.Join(r.Root, "original", configName))
	if err != nil || !strings.HasPrefix(string(data), content) || !strings.Contains(string(data), "auto_recovery=false") {
		t.Fatalf("did not write pinned parent: %q %v", data, err)
	}
}

func TestAutomationWaitsForSharedConfigLockAndCancels(t *testing.T) {
	r := fixture(t)
	lock, err := base.AcquireConfigLock(context.Background(), r.RunDir())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := r.snapshot(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("did not wait for shared config.lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.RunDir(), "automation.lock")); !os.IsNotExist(err) {
		t.Fatalf("created separate automation lock: %v", err)
	}
}
