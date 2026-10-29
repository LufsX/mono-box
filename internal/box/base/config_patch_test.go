package base

import "testing"

func TestMergeConfigPatch(t *testing.T) {
	for _, tc := range []struct {
		name, content, want string
		updates             map[string]string
	}{
		{
			name:    "last duplicate and exported assignment",
			content: "# header\r\nauto_recovery=false\r\nexport auto_recovery=false # keep\r\nclash_api_port=invalid\r\n",
			updates: map[string]string{"auto_recovery": "true", "config_update_wifi_only": "false"},
			want:    "# header\nauto_recovery=false\nexport auto_recovery=true # keep\nclash_api_port=invalid\nconfig_update_wifi_only=false\n",
		},
		{
			name:    "quoted hashes and escaped quote",
			content: "clash_api_secret=\"a\\\" # quoted\" # keep\n",
			updates: map[string]string{"clash_api_secret": "'new#value'"},
			want:    "clash_api_secret='new#value' # keep\n",
		},
		{
			name:    "single quote and escaped hash",
			content: "a='a\\' # one\nb=value\\#literal # two\n",
			updates: map[string]string{"a": "true", "b": "false"},
			want:    "a=true # one\nb=false # two\n",
		},
		{
			name:    "sorted new assignments",
			content: "# auto_recovery=false",
			updates: map[string]string{"config_update_interval_hours": "24", "auto_recovery": "true"},
			want:    "# auto_recovery=false\nauto_recovery=true\nconfig_update_interval_hours=24\n",
		},
		{
			name: "no updates", content: "auto_recovery=true\n\n", want: "auto_recovery=true\n\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := MergeConfigPatch(tc.content, tc.updates); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMergeAutomationConfigWithInvalidOtherFields(t *testing.T) {
	content := "clash_api_port=bad\nauto_recovery=true # preserve\n"
	merged := MergeConfigPatch(content, map[string]string{
		"auto_recovery": "false", "config_update_interval_hours": "168",
	})
	c, err := ParseAutomationConfig(merged)
	if err != nil || c.AutoRecovery || c.ConfigUpdateIntervalHours != 168 {
		t.Fatalf("merged automation: %+v, %v", c, err)
	}
	if _, err := ParseConfig(merged); err == nil {
		t.Fatal("merge changed invalid unrelated field")
	}
}
