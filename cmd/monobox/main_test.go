package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIValidation(t *testing.T) {
	for _, args := range [][]string{{}, {"unknown"}, {"service"}, {"service", "start", "extra"}, {"logs", "read", "invalid-base64"}, {"logs", "remove", "YWJj"}, {"download-core", "mihomo", "no", "false", "arm64-v8a"}, {"proxy-delay"}, {"action", "switch_tun", "1"}, {"action", "start", "extra"}, {"choose-volume"}, {"choose-volume", "0"}, {"choose-volume", "61"}, {"choose-volume", "invalid"}} {
		var out, errOut bytes.Buffer
		args = append([]string{"--root", t.TempDir(), "--module-dir", t.TempDir()}, args...)
		if code := run(args, &out, &errOut); code == 0 {
			t.Fatalf("accepted %v, out %s", args, out.String())
		}
	}
	for _, args := range [][]string{{"--help"}, {"help"}, {"action", "--help"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 0 {
			t.Fatalf("%v: %s", args, errOut.String())
		}
	}
}

func TestCLIConfigRecoveryAndModuleDiscoveryOverride(t *testing.T) {
	root := t.TempDir()
	module := t.TempDir()
	path := filepath.Join(root, "scripts/box.config")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("clash_api_port=invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "module.prop"), []byte("id=mono_box\n"), 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func(command ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := run(append([]string{"--root", root, "--module-dir", module}, command...), &out, &errOut)
		return code, out.String(), errOut.String()
	}
	if code, out, err := invoke("config", "read"); code != 0 || !strings.Contains(out, "invalid") {
		t.Fatalf("%d %s %s", code, out, err)
	}
	if code, out, err := invoke("module", "info"); code != 0 || out != "id=mono_box\n" {
		t.Fatalf("%d %s %s", code, out, err)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("clash_api_port=9090\n"))
	if code, out, err := invoke("config", "write", encoded); code != 0 {
		t.Fatalf("%d %s %s", code, out, err)
	}
	if code, out, err := invoke("config", "read"); code != 0 || out != "clash_api_port=9090\n" {
		t.Fatalf("%d %s %s", code, out, err)
	}
}

func TestCLILogAliases(t *testing.T) {
	root := t.TempDir()
	core := filepath.Join(root, "mihomo")
	if err := os.MkdirAll(core, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(core, "mihomo_202601010000.log")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"service", "logs_size"}, {"action", "log_size"}} {
		var out, errOut bytes.Buffer
		args := append([]string{"--root", root, "--module-dir", t.TempDir()}, command...)
		if code := run(args, &out, &errOut); code != 0 || !strings.Contains(out.String(), "total\t5\t1") {
			t.Fatalf("%d %s %s", code, out.String(), errOut.String())
		}
	}
}
