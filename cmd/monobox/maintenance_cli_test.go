package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaintenanceCommandsRemainAvailableWithInvalidConfig(t *testing.T) {
	root, module := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	secret := "subscription-secret-do-not-export"
	if err := os.WriteFile(filepath.Join(root, "scripts", "box.config"),
		[]byte("clash_api_secret='"+secret+"'\nclash_api_port=invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func(args ...string) []byte {
		t.Helper()
		var out, errOut bytes.Buffer
		code := run(append([]string{"--root", root, "--module-dir", module}, args...), &out, &errOut)
		if code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, errOut.String())
		}
		if strings.Contains(out.String(), secret) || strings.Contains(errOut.String(), secret) {
			t.Fatalf("%v exposed configuration secret", args)
		}
		return out.Bytes()
	}
	var report struct {
		Checks []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"checks"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(invoke("diagnostics"), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Checks) == 0 || report.Text == "" {
		t.Fatalf("missing diagnostic results: %+v", report)
	}
	hasError := false
	for _, check := range report.Checks {
		hasError = hasError || check.Status == "error"
	}
	if !hasError {
		t.Fatal("invalid configuration was not reported")
	}
	var state struct {
		Settings struct {
			RecoveryEnabled     bool `json:"recoveryEnabled"`
			UpdateEnabled       bool `json:"updateEnabled"`
			UpdateIntervalHours int  `json:"updateIntervalHours"`
			UpdateWifiOnly      bool `json:"updateWifiOnly"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(invoke("automation", "get"), &state); err != nil {
		t.Fatal(err)
	}
	if state.Settings.RecoveryEnabled || state.Settings.UpdateEnabled ||
		state.Settings.UpdateIntervalHours != 24 || !state.Settings.UpdateWifiOnly {
		t.Fatalf("unexpected defaults: %+v", state.Settings)
	}
	payload := base64.StdEncoding.EncodeToString([]byte(
		`{"recoveryEnabled":true,"updateEnabled":true,"updateIntervalHours":6,"updateWifiOnly":false}`))
	invoke("automation", "set", payload)
	if err := json.Unmarshal(invoke("automation", "get"), &state); err != nil {
		t.Fatal(err)
	}
	if !state.Settings.RecoveryEnabled || !state.Settings.UpdateEnabled ||
		state.Settings.UpdateIntervalHours != 6 || state.Settings.UpdateWifiOnly {
		t.Fatalf("settings did not persist: %+v", state.Settings)
	}
	content, err := os.ReadFile(filepath.Join(root, "scripts", "box.config"))
	if err != nil {
		t.Fatal(err)
	}
	for _, assignment := range []string{
		"clash_api_secret='" + secret + "'", "clash_api_port=invalid",
		"auto_recovery=true", "auto_update_config=true",
		"config_update_interval_hours=6", "config_update_wifi_only=false",
	} {
		if !strings.Contains(string(content), assignment) {
			t.Fatalf("box.config lost assignment %q", strings.SplitN(assignment, "=", 2)[0])
		}
	}
}

func TestMaintenanceCLIRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{"diagnostics", "extra"},
		{"automation"},
		{"automation", "get", "extra"},
		{"automation", "set", "invalid-base64"},
		{"automation", "remove"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(append([]string{"--root", t.TempDir(), "--module-dir", t.TempDir()}, args...), &out, &errOut)
			if code == 0 {
				t.Fatalf("accepted %v: %s", args, out.String())
			}
		})
	}
}
