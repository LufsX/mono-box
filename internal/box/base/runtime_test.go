package base

import (
	"bytes"
	"net/http"
	"testing"
)

func TestRuntimeWithConfig(t *testing.T) {
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	client := &http.Client{}
	original := &Runtime{
		Root: "/box", ModuleDir: "/module", Config: DefaultConfig(),
		Out: out, Err: errOut, HTTP: client,
	}
	config := original.Config
	config.BinName = "mihomo-smart"
	current := original.WithConfig(config)
	if current == original || current.Config != config {
		t.Fatal("configuration snapshot was not created")
	}
	if current.Root != original.Root || current.ModuleDir != original.ModuleDir ||
		current.Out != out || current.Err != errOut || current.HTTP != client {
		t.Fatal("runtime paths or I/O changed")
	}
	current.Config.APISecret = "new secret"
	if original.Config != DefaultConfig() {
		t.Fatal("snapshot changed the original configuration")
	}
}
