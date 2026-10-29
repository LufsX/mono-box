package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutableModuleDir(t *testing.T) {
	for _, parent := range []string{"modules", "lite_modules", "modules_update"} {
		t.Run(parent, func(t *testing.T) {
			module := filepath.Join(t.TempDir(), parent, "mono_box")
			if err := os.MkdirAll(filepath.Join(module, "bin"), 0700); err != nil {
				t.Fatal(err)
			}
			prop := filepath.Join(module, "module.prop")
			if err := os.WriteFile(prop, []byte("name=Mono Box\nid=mono_box\n"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, suffix := range []string{"", " (deleted)"} {
				got, ok := executableModuleDir(filepath.Join(module, "bin", "monobox") + suffix)
				if !ok || got != module {
					t.Fatalf("%q %t", got, ok)
				}
			}
			if err := os.WriteFile(prop, []byte("id=other\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, ok := executableModuleDir(filepath.Join(module, "bin", "monobox")); ok {
				t.Fatal("accepted another module")
			}
		})
	}
	if _, ok := executableModuleDir(filepath.Join(t.TempDir(), "monobox")); ok {
		t.Fatal("accepted standalone location")
	}
}
