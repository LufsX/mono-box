package box

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryConfig(t *testing.T) {
	c, err := LoadConfig("../../box/scripts/box.config")
	if err != nil {
		t.Fatal(err)
	}
	if c.BinName != "mihomo" || c.APIPort != 9090 || c.ToggleModeCycle != "rule,global,direct" {
		t.Fatalf("%+v", c)
	}
}

func TestLiteralConfig(t *testing.T) {
	c, err := ParseConfig("export bin_name='mihomo'\r\nclash_api_secret='a b#c$()' # comment\nselect_outbound=\"中文 \\\"引号\\\"\"\ntarget_wifi_list='A,true;B,false'\nclash_api_port=1234\nclash_api_port=9091\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.APISecret != "a b#c$()" || c.SelectOutbound != "中文 \"引号\"" || c.TargetWiFiList != "A,true;B,false" || c.APIPort != 9091 {
		t.Fatalf("%+v", c)
	}
}

func TestConfigDoesNotExecute(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "injected")
	_, err := ParseConfig("clash_api_secret='$(touch " + sentinel + ")'\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("executed config: %v", err)
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, line := range []string{"clash_api_port=0", "clash_api_port=65536", "clash_api_port=no", "bin_name='../mihomo'", "bin_name='..'", "clash_api_secret=\"unterminated", "clash_api_secret=value;false", "false", "bin_name=a b"} {
		t.Run(line, func(t *testing.T) {
			if _, err := ParseConfig(line); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}

func TestLiteralQuotes(t *testing.T) {
	for raw, want := range map[string]string{
		"'abc'\\''def'":       "abc'def",
		"\"a\\$b\\\\c\\\"d\"": "a$b\\c\"d",
		"secret#value":        "secret#value",
		"value # comment":     "value",
		" # empty":            "",
	} {
		got, err := parseLiteral(raw)
		if err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", raw, got, err, want)
		}
	}
}

func TestAtomicWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config")
	if err := AtomicWrite(path, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatalf("%s %v", data, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	files, _ := os.ReadDir(filepath.Dir(path))
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".monobox-") {
			t.Fatal("leaked temp file")
		}
	}
}
