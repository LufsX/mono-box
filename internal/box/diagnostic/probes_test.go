package diagnostic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func TestReadConfigMissingInvalidAndUnsafeFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "scripts", "box.config")
	config, missing, err := readConfig(path)
	if err != nil || !missing || config.APIPort != 9090 {
		t.Fatalf("missing config: %+v %v %v", config, missing, err)
	}
	writeConfig(t, root, "clash_api_port=12345\n")
	config, missing, err = readConfig(path)
	if err != nil || missing || config.APIPort != 12345 {
		t.Fatalf("valid config: %+v %v %v", config, missing, err)
	}
	writeConfig(t, root, "clash_api_secret='unterminated\n")
	if _, _, err := readConfig(path); err == nil {
		t.Fatal("malformed config accepted")
	}
	for _, kind := range []string{"symlink", "fifo", "directory"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "scripts", "box.config")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "symlink":
				target := filepath.Join(root, "target")
				if err = os.WriteFile(target, []byte("clash_api_port=9090"), 0600); err == nil {
					err = os.Symlink(target, path)
				}
			case "fifo":
				err = syscall.Mkfifo(path, 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			if _, _, err := readConfig(path); err == nil {
				t.Fatal("unsafe config accepted")
			}
			if time.Since(start) > time.Second {
				t.Fatal("special config file blocked inspection")
			}
		})
	}
}

func TestCommandOutputIsBoundedEvenWithReadFromSource(t *testing.T) {
	out := &boundedOutput{limit: 10}
	if _, err := strings.NewReader(strings.Repeat("x", 100)).WriteTo(out); err == nil {
		t.Fatal("oversized command output accepted")
	}
	if out.buffer.Len() > 10 {
		t.Fatal("output limit bypassed")
	}
}

func TestRoutesShareDeadlineAndDoNotExportOutput(t *testing.T) {
	f := newFixture(t)
	f.deps.timeout = 20 * time.Millisecond
	var firstDeadline time.Time
	calls := 0
	f.deps.routes = func(ctx context.Context, args ...string) ([]byte, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("missing route deadline")
		}
		calls++
		if calls == 1 {
			firstDeadline = deadline
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if deadline != firstDeadline || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatal("route families do not share one time budget")
		}
		return []byte("default via " + privateMarker + " dev tun0\n"), nil
	}
	got := f.run(t)
	statusOf(t, got, "routes", "warning")
	if calls != 2 {
		t.Fatalf("route queries = %d, want 2", calls)
	}
}

func TestQueryAPIResponseBodyTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-req.Context().Done()
	}))
	defer server.Close()
	config := base.DefaultConfig()
	address := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	if _, err := fmt.Sscanf(address, "%d", &config.APIPort); err != nil {
		t.Fatal(err)
	}
	client, closeClient := localClient(nil, 20*time.Millisecond)
	defer closeClient()
	start := time.Now()
	_, _, err := queryAPI(client, config, "/configs", 20*time.Millisecond)
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("response body read not bounded: %v", err)
	}
}

func TestValidDeviceRejectsPathsAndControlCharacters(t *testing.T) {
	for _, name := range []string{"tun0", "Meta", "mihomo-tun", "tun_1.2"} {
		if !validDevice(name) {
			t.Fatalf("valid device rejected: %q", name)
		}
	}
	for _, name := range []string{"", ".", "..", "../tun0", "tun0\n", "tun0;id", "-dev x", strings.Repeat("t", 16)} {
		if validDevice(name) {
			t.Fatalf("unsafe device accepted: %q", name)
		}
	}
}
