package base

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutomationConfigDefaultsAndValues(t *testing.T) {
	for name, parse := range map[string]func(string) (Config, error){
		"full": ParseConfig, "automation": ParseAutomationConfig,
	} {
		t.Run(name, func(t *testing.T) {
			c, err := parse("")
			if err != nil || c.AutoRecovery || c.AutoUpdateConfig || c.ConfigUpdateIntervalHours != 24 || !c.ConfigUpdateWiFiOnly {
				t.Fatalf("defaults: %+v, %v", c, err)
			}
			c, err = parse("export auto_recovery='true' # enable\nauto_update_config=\"true\"\nconfig_update_interval_hours=1\nconfig_update_wifi_only=false\n")
			if err != nil || !c.AutoRecovery || !c.AutoUpdateConfig || c.ConfigUpdateIntervalHours != 1 || c.ConfigUpdateWiFiOnly {
				t.Fatalf("explicit values: %+v, %v", c, err)
			}
			c, err = parse("auto_recovery=true\nauto_recovery=false\nconfig_update_interval_hours=168\n")
			if err != nil || c.AutoRecovery || c.ConfigUpdateIntervalHours != 168 {
				t.Fatalf("last assignment and upper boundary: %+v, %v", c, err)
			}
		})
	}
}

func TestAutomationConfigRejectsInvalidValues(t *testing.T) {
	for name, parse := range map[string]func(string) (Config, error){
		"full": ParseConfig, "automation": ParseAutomationConfig,
	} {
		t.Run(name, func(t *testing.T) {
			for _, key := range []string{"auto_recovery", "auto_update_config", "config_update_wifi_only"} {
				for _, value := range []string{"", "''", "# empty", "1", "0", "t", "f", "T", "F", "TRUE", "False", "yes", "no", "' true'", "'false '", "'unterminated", "true;false", "$(false)"} {
					t.Run(key+"="+value, func(t *testing.T) {
						if _, err := parse(key + "=" + value); err == nil {
							t.Fatal("accepted invalid automation boolean")
						}
					})
				}
			}
			for _, value := range []string{"", "''", "0", "-1", "169", "1.5", "no", "' 24'", "99999999999999999999999999", "24;false"} {
				if _, err := parse("config_update_interval_hours=" + value); err == nil {
					t.Errorf("accepted invalid interval %q", value)
				}
			}
			for _, line := range []string{"auto_recovery", "export auto_update_config false", "config_update_interval_hours 24", "config_update_wifi_only = true false"} {
				if _, err := parse(line); err == nil {
					t.Errorf("accepted malformed assignment %q", line)
				}
			}
			if _, err := parse("auto_recovery=invalid\nauto_recovery=false\n"); err == nil {
				t.Error("later assignment concealed an invalid value")
			}
		})
	}
}

func TestParseAutomationConfigIgnoresUnrelatedErrors(t *testing.T) {
	content := "clash_api_port=no\nbin_name='../bad'\nclash_api_secret='unterminated\nunknown=value;false\nnot an assignment\n"
	content += "auto_recovery=false\nauto_update_config=true\nconfig_update_interval_hours=48\nconfig_update_wifi_only=false\n"
	c, err := ParseAutomationConfig(content)
	if err != nil || c.AutoRecovery || !c.AutoUpdateConfig || c.ConfigUpdateIntervalHours != 48 || c.ConfigUpdateWiFiOnly {
		t.Fatalf("automation blocked by unrelated errors: %+v, %v", c, err)
	}
	if c.APIPort != 9090 || c.BinName != "mihomo" || c.APISecret != "" {
		t.Fatalf("unrelated assignments were applied: %+v", c)
	}
	if _, err := ParseConfig(content); err == nil {
		t.Fatal("full parser ignored invalid config")
	}
	if _, err := ParseAutomationConfig(content + "auto_recovery=1\n"); err == nil {
		t.Fatal("unrelated errors concealed an invalid automation value")
	}
	if _, err := ParseAutomationConfig(strings.Repeat("#", ConfigSizeLimit+1)); err == nil {
		t.Fatal("accepted oversized config")
	}
}

func TestAutomationConfigDoesNotExecute(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "injected")
	content := "auto_recovery=$(touch " + sentinel + ")\n"
	for name, parse := range map[string]func(string) (Config, error){
		"full": ParseConfig, "automation": ParseAutomationConfig,
	} {
		if _, err := parse(content); err == nil {
			t.Errorf("%s accepted command as a boolean", name)
		}
	}
	if _, err := ParseAutomationConfig("unknown=$(touch " + sentinel + ")\nauto_recovery=false\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("config executed: %v", err)
	}
}
