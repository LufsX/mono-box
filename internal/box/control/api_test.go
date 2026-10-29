package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func controllerTestRuntime(t *testing.T, config string) *Runtime {
	t.Helper()
	root := t.TempDir()
	r := &Runtime{Runtime: &base.Runtime{Root: root, ModuleDir: filepath.Join(root, "module"), Out: io.Discard, Err: io.Discard, Config: base.DefaultConfig()}}
	if err := os.MkdirAll(r.ModuleDir, 0700); err != nil {
		t.Fatal(err)
	}
	controllerTestWrite(t, filepath.Join(root, "scripts", "box.config"), config)
	return r
}
func controllerTestWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := base.AtomicWrite(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func controllerTestServer(t *testing.T, r *Runtime, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	r.Config.APIPort = port
	r.HTTP = server.Client()
	return server
}
func controllerTestPort(server *httptest.Server) string {
	u, _ := url.Parse(server.URL)
	return u.Port()
}

func TestAPIJSONEscapingAuthAndStatus(t *testing.T) {
	r := controllerTestRuntime(t, "")
	group := "分组 /?#% &"
	name := "node \"quoted\"\n测试"
	count := 0
	controllerTestServer(t, r, func(w http.ResponseWriter, req *http.Request) {
		count++
		if req.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing auth")
		}
		if req.URL.EscapedPath() != "/proxies/"+url.PathEscape(group) {
			t.Errorf("path = %s", req.URL.EscapedPath())
		}
		if req.Method == "GET" {
			io.WriteString(w, `{"now":"当前"}`)
			return
		}
		var payload map[string]string
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["name"] != name {
			t.Errorf("name = %q", payload["name"])
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Config.APISecret = "secret"
	r.Config.SelectOutbound = group
	if err := r.SetOutbound(group, name); err != nil {
		t.Fatal(err)
	}
	if got, err := r.GetNowOutbound(); err != nil || got != "当前" {
		t.Fatalf("%q %v", got, err)
	}
	if count != 2 {
		t.Fatal(count)
	}
}

func TestAPIErrorsAndNoEnvironmentProxy(t *testing.T) {
	r := controllerTestRuntime(t, "")
	proxyCalls := 0
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(*http.Request) (*url.URL, error) { proxyCalls++; return nil, errors.New("must not use proxy") }
	r.HTTP = &http.Client{Transport: transport, Timeout: time.Second}
	status := http.StatusUnauthorized
	body := "unauthorized"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(status); io.WriteString(w, body) }))
	defer server.Close()
	r.Config.APIPort, _ = strconv.Atoi(controllerTestPort(server))
	if err := r.SetMode("rule"); err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatal(err)
	}
	if proxyCalls != 0 {
		t.Fatal("used proxy")
	}
	status = 200
	for _, invalid := range []string{"null", "[]", "{", `{"mode":3}`} {
		body = invalid
		if _, err := r.GetNowMode(); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
	status = 302
	if _, err := r.GetConfigs(); err == nil {
		t.Fatal("accepted redirect")
	}
	if err := r.SetMode("invalid"); err == nil {
		t.Fatal("accepted invalid mode")
	}
	if err := r.SetOutbound("", "x"); err == nil {
		t.Fatal("accepted empty selector")
	}
}

type controllerRoundTrip func(*http.Request) (*http.Response, error)

func (f controllerRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestAPIInjectedTransportAndTimeout(t *testing.T) {
	r := controllerTestRuntime(t, "")
	sentinel := errors.New("transport failed")
	r.HTTP = &http.Client{Transport: controllerRoundTrip(func(req *http.Request) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		if !ok || time.Until(deadline) > apiTimeout {
			t.Error("missing API deadline")
		}
		return nil, sentinel
	})}
	if _, err := r.GetConfigs(); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	r.HTTP = &http.Client{Timeout: 10 * time.Millisecond, Transport: controllerRoundTrip(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	if _, err := r.GetConfigs(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestActionHotReloadAndInvalidInputs(t *testing.T) {
	r := controllerTestRuntime(t, "")
	mode := "rule"
	tun := true
	server := controllerTestServer(t, r, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"mode": mode, "tun": map[string]bool{"enable": tun}})
			return
		}
		var payload struct {
			Mode string
			Tun  *struct{ Enable bool }
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.Mode != "" {
			mode = payload.Mode
		}
		if payload.Tun != nil {
			tun = payload.Tun.Enable
		}
		w.WriteHeader(204)
	})
	base := "clash_api_port=" + controllerTestPort(server) + "\n"
	path := filepath.Join(r.Root, "scripts", "box.config")
	controllerTestWrite(t, path, base+"toggle_action=tun\ntoggle_tun_target=toggle\n")
	if err := r.Action("toggle", nil); err != nil || tun {
		t.Fatalf("tun=%v: %v", tun, err)
	}
	controllerTestWrite(t, path, base+"toggle_action=mode_cycle\ntoggle_mode_cycle='rule; global;direct'\n")
	if err := r.Action("toggle", nil); err != nil || mode != "global" {
		t.Fatalf("mode=%s: %v", mode, err)
	}
	if err := r.Action("toggle", nil); err != nil || mode != "direct" {
		t.Fatalf("mode=%s: %v", mode, err)
	}
	for _, test := range []struct {
		action string
		args   []string
	}{
		{"wat", nil}, {"switch_tun", nil}, {"switch_tun", []string{"on"}}, {"switch_mode", []string{"INVALID"}}, {"status", []string{"extra"}},
	} {
		if err := r.Action(test.action, test.args); err == nil {
			t.Errorf("accepted %v", test)
		}
	}
	var out bytes.Buffer
	r.Out = &out
	if err := r.Action("get_configs", nil); err != nil || !json.Valid(bytes.TrimSpace(out.Bytes())) {
		t.Fatalf("%s %v", out.String(), err)
	}
	controllerTestWrite(t, path, "clash_api_port=broken")
	if err := r.Action("get_configs", nil); err == nil {
		t.Fatal("ignored invalid config")
	}
}

func TestActionMalformedTUNToggle(t *testing.T) {
	r := controllerTestRuntime(t, "")
	server := controllerTestServer(t, r, func(w http.ResponseWriter, req *http.Request) { io.WriteString(w, `{"tun":{"enable":"true"}}`) })
	controllerTestWrite(t, filepath.Join(r.Root, "scripts", "box.config"), "clash_api_port="+controllerTestPort(server)+"\ntoggle_action=tun\n")
	if err := r.Action("toggle", nil); err == nil {
		t.Fatal("accepted malformed TUN JSON")
	}
}
