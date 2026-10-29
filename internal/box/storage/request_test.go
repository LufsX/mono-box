package storage

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestChunkedLargeImport(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer
	runtime, _ := base.New(root, t.TempDir(), &out, io.Discard)
	r := New(runtime)
	content := "# 中文配置\n" + strings.Repeat("# large YAML\n", 50000)
	name := base64.StdEncoding.EncodeToString([]byte("large.yaml"))
	payload := base64.StdEncoding.EncodeToString([]byte(content))
	args := []string{"profiles", "import", name, payload}
	data, _ := json.Marshal(args)
	encoded := base64.StdEncoding.EncodeToString(data)
	if err := r.Request([]string{"begin"}, nil); err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(out.String())
	if !validRequestToken(token) {
		t.Fatalf("invalid token: %q", token)
	}
	for len(encoded) > 0 {
		n := requestChunkLimit
		if n > len(encoded) {
			n = len(encoded)
		}
		if err := r.Request([]string{"append", token, encoded[:n]}, nil); err != nil {
			t.Fatal(err)
		}
		encoded = encoded[n:]
	}
	out.Reset()
	if err := r.Request([]string{"run", token}, r.Storage); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "mihomo/.mono-box/profiles/large.yaml"))
	if err != nil || string(got) != content {
		t.Fatalf("import mismatch: len=%d %v", len(got), err)
	}
	if _, err := os.Stat(filepath.Join(root, "run/requests", token)); !os.IsNotExist(err) {
		t.Fatal("request retained")
	}
}

func TestRequestValidationAndFailureCleanup(t *testing.T) {
	var out bytes.Buffer
	runtime, _ := base.New(t.TempDir(), t.TempDir(), &out, io.Discard)
	r := New(runtime)
	for _, args := range [][]string{nil, {"append", "../outside", "YWJj"}, {"run", strings.Repeat("A", 32)}, {"run", strings.Repeat("a", 32)}} {
		if err := r.Request(args, nil); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if err := r.Request([]string{"begin"}, nil); err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(out.String())
	data, _ := json.Marshal([]string{"config", "write", "invalid"})
	if err := r.Request([]string{"append", token, base64.StdEncoding.EncodeToString(data)}, nil); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("executor failed")
	if err := r.Request([]string{"run", token}, func([]string) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.RunDir(), "requests", token)); !os.IsNotExist(err) {
		t.Fatal("failed request retained")
	}
	if err := r.Request([]string{"abort", token}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRequestRejectsOversizedChunksAndLimit(t *testing.T) {
	var out bytes.Buffer
	runtime, _ := base.New(t.TempDir(), t.TempDir(), &out, io.Discard)
	r := New(runtime)
	if err := r.Request([]string{"begin"}, nil); err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(out.String())
	if err := r.Request([]string{"append", token, strings.Repeat("a", requestChunkLimit+4)}, nil); err == nil {
		t.Fatal("oversized chunk")
	}
	path := filepath.Join(r.RunDir(), "requests", token)
	f, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(requestLimit); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := r.Request([]string{"append", token, "YQ=="}, nil); err == nil {
		t.Fatal("oversized request")
	}
}
