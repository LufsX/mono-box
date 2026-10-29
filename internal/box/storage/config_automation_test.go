package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestStoragePatchAutomationFields(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enabled  string
		interval string
		want     int
	}{
		{"minimum", "true", "1", 1},
		{"maximum", "false", "168", 168},
		{"default-interval", "true", "24", 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := storageFixture(t)
			updates := map[string]string{
				"auto_recovery": tc.enabled, "auto_update_config": tc.enabled,
				"config_update_interval_hours": tc.interval, "config_update_wifi_only": tc.enabled,
			}
			patch, err := json.Marshal(updates)
			if err != nil {
				t.Fatal(err)
			}
			storageRun(t, r, "config", "patch", storageB64(string(patch)))
			if _, err := os.Lstat(filepath.Join(r.RunDir(), "automation-status.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("patch created status without an active task: %v", err)
			}
			config, err := base.LoadConfig(filepath.Join(r.Root, "scripts", "box.config"))
			if err != nil {
				t.Fatal(err)
			}
			enabled := tc.enabled == "true"
			if config.AutoRecovery != enabled || config.AutoUpdateConfig != enabled ||
				config.ConfigUpdateWiFiOnly != enabled || config.ConfigUpdateIntervalHours != tc.want {
				t.Fatalf("automation patch not applied: %+v", config)
			}
		})
	}
}

func TestStorageRejectsInvalidAutomationConfig(t *testing.T) {
	for key, values := range map[string][]string{
		"auto_recovery":                {"1", "0", "TRUE", "yes", "''"},
		"auto_update_config":           {"1", "0", "FALSE", "no", "''"},
		"config_update_wifi_only":      {"1", "0", "True", "yes", "''"},
		"config_update_interval_hours": {"0", "169", "-1", "24.5", "abc", "''"},
	} {
		for _, value := range values {
			t.Run(key+"="+value, func(t *testing.T) {
				r, _, _ := storageFixture(t)
				path := filepath.Join(r.Root, "scripts", "box.config")
				before, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				original := storageRead(t, path)
				patch, err := json.Marshal(map[string]string{key: value})
				if err != nil {
					t.Fatal(err)
				}
				for _, args := range [][]string{
					{"config", "patch", storageB64(string(patch))},
					{"config", "write", storageB64(original + key + "=" + value + "\n")},
				} {
					if err := r.Storage(args); err == nil {
						t.Fatalf("%s accepted invalid automation field", args[1])
					}
					after, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					if !os.SameFile(before, after) || storageRead(t, path) != original {
						t.Fatal("invalid automation config replaced or modified original")
					}
				}
			})
		}
	}
}

func TestStorageOrdinaryPatchPreservesAutomationAndUnknownFields(t *testing.T) {
	r, _, _ := storageFixture(t)
	path := filepath.Join(r.Root, "scripts", "box.config")
	original := "# settings\nclash_api_port=9090 # API\n" +
		"auto_recovery=true # recover\nauto_update_config=true\n" +
		"config_update_interval_hours=37\nconfig_update_wifi_only=false\n" +
		"export future_option='literal # $()' # unknown setting\n"
	storageWriteFixture(t, path, original, 0600)
	storageRun(t, r, "config", "patch", storageB64(`{"clash_api_port":"9191"}`))
	want := strings.Replace(original, "9090", "9191", 1)
	if got := storageRead(t, path); got != want {
		t.Fatalf("ordinary patch changed unrelated text: got %q, want %q", got, want)
	}
}

func TestStoragePatchValidatesWholeConfig(t *testing.T) {
	for _, original := range []string{
		"auto_recovery=invalid\n",
		"config_update_interval_hours=169\n",
		"clash_api_port=0\n",
		"not-an-assignment\n",
	} {
		t.Run(strings.TrimSpace(original), func(t *testing.T) {
			r, _, _ := storageFixture(t)
			path := filepath.Join(r.Root, "scripts", "box.config")
			storageWriteFixture(t, path, original, 0600)
			if err := r.PatchConfig(storageB64(`{"target_wifi":"'new'"}`)); err == nil {
				t.Fatal("patch accepted invalid on-disk config")
			}
			if storageRead(t, path) != original {
				t.Fatal("rejected patch modified invalid on-disk config")
			}
		})
	}
}

func TestStorageConfigWritesWaitForConfigLock(t *testing.T) {
	for _, command := range []string{"write", "patch"} {
		t.Run(command, func(t *testing.T) {
			r, _, _ := storageFixture(t)
			path := filepath.Join(r.Root, "scripts", "box.config")
			runDir := filepath.Join(r.Root, "run")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			lock, err := base.AcquireConfigLock(ctx, runDir)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			service, err := os.OpenFile(filepath.Join(runDir, "service.lock"), os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close()

			replacement := "# replacement\r\nclash_api_port=9292\r\n"
			payload := replacement
			if command == "patch" {
				payload = `{"clash_api_port":"9191"}`
			}
			done := make(chan error, 1)
			go func() { done <- r.Storage([]string{"config", command, storageB64(payload)}) }()

			// The writer must hold service.lock while it waits for config.lock.
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				err = syscall.Flock(int(service.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
				if errors.Is(err, syscall.EWOULDBLOCK) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := syscall.Flock(int(service.Fd()), syscall.LOCK_UN); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					t.Fatalf("writer finished before acquiring service then config lock: %v", err)
				case <-ctx.Done():
					t.Fatal("writer did not acquire service lock")
				case <-ticker.C:
				}
			}
			select {
			case err := <-done:
				t.Fatalf("writer bypassed held config lock: %v", err)
			case <-time.After(50 * time.Millisecond):
			}

			// Simulate automation set completing while storage is waiting.
			latest := "# latest automation settings\nclash_api_port=9090 # API\n" +
				"auto_recovery=true\nauto_update_config=true\n" +
				"config_update_interval_hours=37\nconfig_update_wifi_only=false\n" +
				"future_option='keep me' # unknown\n"
			if err := base.AtomicWrite(path, []byte(latest), 0600); err != nil {
				t.Fatal(err)
			}
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("writer did not finish after config lock release")
			}
			want := replacement
			if command == "patch" {
				want = strings.Replace(latest, "9090", "9191", 1)
			}
			if got := storageRead(t, path); got != want {
				t.Fatalf("incorrect config after lock release: got %q, want %q", got, want)
			}
			config, err := base.LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if command == "write" && (config.AutoRecovery || config.AutoUpdateConfig ||
				config.ConfigUpdateIntervalHours != 24 || !config.ConfigUpdateWiFiOnly) {
				t.Fatalf("full replacement did not restore omitted automation defaults: %+v", config)
			}
			storageNoTemps(t, r)
		})
	}
}

func TestStorageRawConfigReadPreservesTextWithoutConfigLock(t *testing.T) {
	r, _, _ := storageFixture(t)
	original := "# 原始配置\r\n\r\nexport auto_recovery=true # recover\r\n" +
		"auto_update_config=false\r\nconfig_update_interval_hours=37\r\n" +
		"config_update_wifi_only=false\r\nfuture_option='literal # $()' # keep\r\n"
	storageRun(t, r, "config", "write", storageB64(original))
	var out strings.Builder
	r.Out = &out
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	lock, err := base.AcquireConfigLock(ctx, filepath.Join(r.Root, "run"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	done := make(chan error, 1)
	go func() { done <- r.Storage([]string{"config", "read"}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("read waited for config lock")
	}
	if got := out.String(); got != original {
		t.Fatalf("raw config changed: got %q, want %q", got, original)
	}
}
