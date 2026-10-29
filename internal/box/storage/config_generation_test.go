package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/LufsX/mono-box/internal/box/base"
)

const generationConfig = "clash_api_port=9090\nauto_recovery=true\nauto_update_config=true\n" +
	"config_update_interval_hours=24\nconfig_update_wifi_only=true\n"

func generationStatus(t *testing.T, r *Runtime) map[string]json.RawMessage {
	t.Helper()
	path := filepath.Join(r.RunDir(), "automation-status.json")
	status := map[string]json.RawMessage{
		"recoveryGeneration": json.RawMessage("7"),
		"updateGeneration":   json.RawMessage("11"),
		"recoveryAttempt":    json.RawMessage("3"),
		"updateAttempt":      json.RawMessage("4"),
		"settings":           json.RawMessage(`{"recoveryEnabled":true,"updateEnabled":true,"updateIntervalHours":24,"updateWifiOnly":true}`),
		"recovery":           json.RawMessage(`{"state":"recovering","attempts":1}`),
		"update":             json.RawMessage(`{"state":"updating","updated":2}`),
		"future":             json.RawMessage(`{"large":9007199254740993,"comment":"keep me"}`),
	}
	if err := base.WritePrivateJSON(path, status, 16<<10); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestStorageConfigInvalidatesOnlyChangedFeatureGenerations(t *testing.T) {
	for _, command := range []string{"patch", "write"} {
		for _, tc := range []struct {
			name, key, value string
			recovery, update string
		}{
			{"recovery", "auto_recovery", "false", "8", "11"},
			{"update", "auto_update_config", "false", "7", "12"},
			{"interval", "config_update_interval_hours", "37", "7", "12"},
			{"wifi-only", "config_update_wifi_only", "false", "7", "12"},
			{"ordinary", "clash_api_port", "9191", "7", "11"},
			{"equivalent-literal", "auto_recovery", "'true'", "7", "11"},
		} {
			t.Run(command+"/"+tc.name, func(t *testing.T) {
				r, _, _ := storageFixture(t)
				configPath := filepath.Join(r.Root, "scripts", "box.config")
				storageWriteFixture(t, configPath, generationConfig, 0600)
				want := generationStatus(t, r)
				updates := map[string]string{tc.key: tc.value}
				content := base.MergeConfigPatch(generationConfig, updates)
				payload := content
				if command == "patch" {
					patch, err := json.Marshal(updates)
					if err != nil {
						t.Fatal(err)
					}
					payload = string(patch)
				}
				storageRun(t, r, "config", command, storageB64(payload))
				want["recoveryGeneration"] = json.RawMessage(tc.recovery)
				want["updateGeneration"] = json.RawMessage(tc.update)
				var got map[string]json.RawMessage
				if err := base.ReadPrivateJSON(filepath.Join(r.RunDir(), "automation-status.json"), &got, 16<<10); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("status fields changed incorrectly: got %s, want %s", mustGenerationJSON(t, got), mustGenerationJSON(t, want))
				}
				if got := storageRead(t, configPath); got != content {
					t.Fatalf("config not saved: %q", got)
				}
			})
		}
	}
}

func mustGenerationJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStorageConfigGenerationRejectsUnsafeOrInvalidStatus(t *testing.T) {
	for _, command := range []string{"patch", "write"} {
		for _, kind := range []string{"symlink", "hardlink", "fifo", "oversized", "invalid-json", "null", "invalid-generation", "overflow"} {
			t.Run(command+"/"+kind, func(t *testing.T) {
				r, _, _ := storageFixture(t)
				path := filepath.Join(r.Root, "scripts", "box.config")
				storageWriteFixture(t, path, generationConfig, 0600)
				statusPath := filepath.Join(r.RunDir(), "automation-status.json")
				if err := os.MkdirAll(r.RunDir(), 0700); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "outside")
				external := `{"recoveryGeneration":7,"updateGeneration":11}`
				storageWriteFixture(t, outside, external, 0600)
				switch kind {
				case "symlink":
					if err := os.Symlink(outside, statusPath); err != nil {
						t.Fatal(err)
					}
				case "hardlink":
					if err := os.Link(outside, statusPath); err != nil {
						t.Fatal(err)
					}
				case "fifo":
					if err := syscall.Mkfifo(statusPath, 0600); err != nil {
						t.Fatal(err)
					}
				default:
					data := map[string]string{
						"oversized":          strings.Repeat(" ", 16<<10) + "{}",
						"invalid-json":       "{",
						"null":               "null",
						"invalid-generation": `{"recoveryGeneration":"invalid"}`,
						"overflow":           `{"recoveryGeneration":18446744073709551615}`,
					}[kind]
					storageWriteFixture(t, statusPath, data, 0600)
				}
				payload := base.MergeConfigPatch(generationConfig, map[string]string{"auto_recovery": "false"})
				if command == "patch" {
					payload = `{"auto_recovery":"false"}`
				}
				if err := r.Storage([]string{"config", command, storageB64(payload)}); err == nil {
					t.Fatal("config write accepted unsafe or invalid generation status")
				}
				if got := storageRead(t, path); got != generationConfig {
					t.Fatal("failed generation invalidation changed config")
				}
				if got := storageRead(t, outside); got != external {
					t.Fatal("generation invalidation changed external target")
				}
				storageNoTemps(t, r)
			})
		}
	}
}

func TestStorageConfigWriteRepairsMalformedOrOversizedConfig(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"malformed", "auto_recovery=invalid\n"},
		{"oversized", strings.Repeat("#", base.ConfigSizeLimit+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := storageFixture(t)
			path := filepath.Join(r.Root, "scripts", "box.config")
			storageWriteFixture(t, path, tc.content, 0600)
			want := generationStatus(t, r)
			storageRun(t, r, "config", "write", storageB64(generationConfig))
			want["recoveryGeneration"] = json.RawMessage("8")
			want["updateGeneration"] = json.RawMessage("12")
			var got map[string]json.RawMessage
			if err := base.ReadPrivateJSON(filepath.Join(r.RunDir(), "automation-status.json"), &got, 16<<10); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) || storageRead(t, path) != generationConfig {
				t.Fatal("replacement did not repair config and invalidate both features")
			}
		})
	}
}
