package diagnostic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
	"github.com/LufsX/mono-box/internal/box/service"
)

const privateMarker = "private-secret-token-subscription"
const tunEnabled = `{"mode":"rule","tun":{"enable":true,"device":"tun0"}}`
const tunDisabled = `{"mode":"rule","tun":{"enable":false}}`
const dnsOK = `{"Status":0,"Answer":[{"type":1,"data":"198.18.0.1"}]}`

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type fixture struct {
	r     *Runtime
	out   *bytes.Buffer
	deps  dependencies
	calls []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	out := &bytes.Buffer{}
	r := &base.Runtime{Root: t.TempDir(), Config: base.DefaultConfig(), Out: out}
	f := &fixture{r: New(r), out: out, deps: defaultDependencies()}
	writeConfig(t, r.Root, "clash_api_port=9090\nclash_api_secret='"+privateMarker+"'\n")
	f.deps.now = func() time.Time { return time.UnixMilli(1234567890000) }
	f.deps.pid = func(*base.Runtime) (int, error) { return 42, nil }
	f.deps.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		if _, ok := ctx.Deadline(); !ok || network != "tcp" || address != "127.0.0.1:9090" {
			t.Errorf("unbounded or non-local port probe: %s %s", network, address)
		}
		left, right := net.Pipe()
		right.Close()
		return left, nil
	}
	f.deps.tun = func(device string) (bool, error) {
		if device != "tun0" {
			t.Errorf("unexpected device: %q", device)
		}
		return true, nil
	}
	f.deps.routes = func(ctx context.Context, args ...string) ([]byte, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("route query has no deadline")
		}
		expected := []string{args[0], "route", "show", "table", "all", "dev", "tun0"}
		if !reflect.DeepEqual(args, expected) || (args[0] != "-4" && args[0] != "-6") {
			t.Errorf("unexpected route command: %v", args)
		}
		return []byte("default dev tun0 table 2022\n"), nil
	}
	f.serve(t, tunEnabled, 200, dnsOK, 200)
	return f
}

func writeConfig(t *testing.T, root, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "box.config"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) serve(t *testing.T, configs string, configStatus int, dns string, dnsStatus int) {
	t.Helper()
	f.r.HTTP = &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		if _, ok := req.Context().Deadline(); !ok {
			t.Error("API request has no deadline")
		}
		if req.URL.Scheme != "http" || req.URL.Host != "127.0.0.1:9090" || req.Method != http.MethodGet {
			t.Errorf("unexpected API request: %s %s", req.Method, req.URL)
		}
		if req.Header.Get("Authorization") != "Bearer "+privateMarker {
			t.Error("API request did not use the parsed config secret")
		}
		f.calls = append(f.calls, req.URL.RequestURI())
		body, status := configs, configStatus
		switch req.URL.RequestURI() {
		case "/configs":
		case "/dns/query?name=example.com&type=A":
			body, status = dns, dnsStatus
		default:
			t.Errorf("unexpected request path: %s", req.URL.RequestURI())
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
}

func (f *fixture) run(t *testing.T) report {
	t.Helper()
	if err := f.r.run(f.deps); err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(f.out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.GeneratedAt != 1234567890000 || len(got.Checks) != 7 {
		t.Fatalf("incomplete report: %+v", got)
	}
	if strings.Contains(f.out.String(), privateMarker) {
		t.Fatal("report exposed external data or credentials")
	}
	seen := map[string]bool{}
	for _, c := range got.Checks {
		if seen[c.ID] || c.Title == "" || c.Message == "" {
			t.Fatalf("invalid check: %+v", c)
		}
		seen[c.ID] = true
		switch c.Status {
		case "ok", "warning", "error", "skipped":
		default:
			t.Fatalf("invalid status: %s", c.Status)
		}
		for _, field := range []string{c.ID, c.Title, c.Message} {
			if !strings.Contains(got.Text, field) {
				t.Fatalf("text report omitted %q", field)
			}
		}
	}
	return got
}

func statusOf(t *testing.T, got report, id, want string) check {
	t.Helper()
	for _, c := range got.Checks {
		if c.ID == id {
			if c.Status != want {
				t.Fatalf("%s status = %s, want %s: %s", id, c.Status, want, c.Message)
			}
			return c
		}
	}
	t.Fatalf("missing check %s", id)
	return check{}
}

func TestHealthyReportAndRuntimeConfigIsolation(t *testing.T) {
	f := newFixture(t)
	f.r.Config.APIPort = 1
	f.r.Config.APISecret = "stale"
	got := f.run(t)
	for _, c := range got.Checks {
		if c.Status != "ok" {
			t.Fatalf("unexpected unhealthy check: %+v", c)
		}
	}
	if !strings.Contains(statusOf(t, got, "routes", "ok").Message, "尚未验证策略规则和流量转发") {
		t.Fatal("route result overstates its scope")
	}
	if f.r.Config.APIPort != 1 || f.r.Config.APISecret != "stale" {
		t.Fatal("diagnostic changed shared runtime config")
	}
	if len(f.calls) != 2 {
		t.Fatalf("unexpected HTTP calls: %v", f.calls)
	}
}

func TestInvalidConfigStillProducesSafeReport(t *testing.T) {
	f := newFixture(t)
	writeConfig(t, f.r.Root, "clash_api_secret='"+privateMarker+"'\nclash_api_port=99999\n")
	f.deps.pid = func(*base.Runtime) (int, error) { t.Fatal("invalid config used by PID check"); return 0, nil }
	f.deps.dial = func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("invalid config used by port check")
		return nil, nil
	}
	got := f.run(t)
	statusOf(t, got, "config", "error")
	for _, id := range []string{"service", "controller", "api", "tun", "routes", "dns"} {
		statusOf(t, got, id, "skipped")
	}
}

func TestStoppedCoreAndOccupiedPort(t *testing.T) {
	for _, listening := range []bool{false, true} {
		t.Run(strconv.FormatBool(listening), func(t *testing.T) {
			f := newFixture(t)
			f.deps.pid = func(*base.Runtime) (int, error) { return 0, service.ErrStopped }
			if !listening {
				f.deps.dial = func(context.Context, string, string) (net.Conn, error) { return nil, syscall.ECONNREFUSED }
			}
			got := f.run(t)
			statusOf(t, got, "service", "warning")
			want := "skipped"
			if listening {
				want = "warning"
				if !strings.Contains(statusOf(t, got, "controller", want).Message, "占用") {
					t.Fatal("missing occupied-port explanation")
				}
			}
			statusOf(t, got, "controller", want)
			for _, id := range []string{"api", "tun", "routes", "dns"} {
				statusOf(t, got, id, "skipped")
			}
			if len(f.calls) != 0 {
				t.Fatal("unverified process triggered API requests")
			}
		})
	}
}

func TestServiceLockContentionAndErrorsSkipAPI(t *testing.T) {
	for _, err := range []error{base.ErrServiceLockTimeout, errors.New(privateMarker)} {
		f := newFixture(t)
		f.deps.pid = func(*base.Runtime) (int, error) { return 0, err }
		got := f.run(t)
		statusOf(t, got, "service", "warning")
		statusOf(t, got, "api", "skipped")
		if len(f.calls) != 0 {
			t.Fatal("unknown identity triggered API requests")
		}
	}
}

func TestRunningCoreWithClosedPort(t *testing.T) {
	f := newFixture(t)
	f.deps.dial = func(context.Context, string, string) (net.Conn, error) { return nil, syscall.ECONNREFUSED }
	got := f.run(t)
	statusOf(t, got, "controller", "error")
	statusOf(t, got, "api", "skipped")
	if len(f.calls) != 0 {
		t.Fatal("closed port triggered API requests")
	}
}

func TestAPIAuthAndMalformedResponses(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		code int
		hint string
	}{
		{"unauthorized", privateMarker, 401, "鉴权失败（HTTP 401）"},
		{"forbidden", privateMarker, 403, "HTTP 403"},
		{"server-error", privateMarker, 500, "HTTP 500"},
		{"redirect", privateMarker, 302, "HTTP 302"},
		{"invalid-json", privateMarker, 200, "格式异常"},
		{"array", "[]", 200, "格式异常"},
		{"null", "null", 200, "格式异常"},
		{"empty-object", "{}", 200, "格式异常"},
		{"bad-tun", `{"mode":"rule","tun":{"enable":"yes"}}`, 200, "格式异常"},
		{"too-large", strings.Repeat("x", responseLimit+1), 200, "读取失败"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.serve(t, tc.body, tc.code, privateMarker, 200)
			got := f.run(t)
			c := statusOf(t, got, "api", "error")
			if !strings.Contains(c.Message, tc.hint) {
				t.Fatalf("missing hint %q: %s", tc.hint, c.Message)
			}
			for _, id := range []string{"tun", "routes", "dns"} {
				statusOf(t, got, id, "skipped")
			}
			if len(f.calls) != 1 {
				t.Fatalf("unexpected requests: %v", f.calls)
			}
		})
	}
}

func TestTunDisabledMissingAndUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name, body, tunStatus, routeStatus string
		probe                              bool
		up                                 bool
		err                                error
	}{
		{"disabled", tunDisabled, "ok", "skipped", false, false, nil},
		{"unknown", `{"mode":"rule"}`, "warning", "skipped", false, false, nil},
		{"no-device", `{"mode":"rule","tun":{"enable":true}}`, "warning", "skipped", false, false, nil},
		{"unsafe-device", `{"mode":"rule","tun":{"enable":true,"device":"../secret"}}`, "warning", "skipped", false, false, nil},
		{"missing", tunEnabled, "error", "skipped", true, false, errors.New(privateMarker)},
		{"down", tunEnabled, "error", "skipped", true, false, nil},
		{"up", tunEnabled, "ok", "ok", true, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.serve(t, tc.body, 200, dnsOK, 200)
			called := false
			f.deps.tun = func(string) (bool, error) { called = true; return tc.up, tc.err }
			got := f.run(t)
			if called != tc.probe {
				t.Fatalf("TUN device probe = %v, want %v", called, tc.probe)
			}
			statusOf(t, got, "tun", tc.tunStatus)
			statusOf(t, got, "routes", tc.routeStatus)
			statusOf(t, got, "dns", "ok")
		})
	}
}

func TestDNSResults(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		code             int
	}{
		{"success", dnsOK, "ok", 200},
		{"unsupported", privateMarker, "skipped", 404},
		{"method-unsupported", privateMarker, "skipped", 405},
		{"not-implemented", privateMarker, "skipped", 501},
		{"auth-lost", privateMarker, "error", 401},
		{"failed", privateMarker, "error", 500},
		{"invalid-json", privateMarker, "error", 200},
		{"missing-status", "{}", "error", 200},
		{"nxdomain", `{"Status":3}`, "error", 200},
		{"invalid-status", `{"Status":99999}`, "error", 200},
		{"empty-answer", `{"Status":0,"Answer":[]}`, "warning", 200},
		{"invalid-address", `{"Status":0,"Answer":[{"type":1,"data":"private-secret-token-subscription"}]}`, "warning", 200},
		{"cname-only", `{"Status":0,"Answer":[{"type":5,"data":"example.net"}]}`, "warning", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.serve(t, tunDisabled, 200, tc.body, tc.code)
			statusOf(t, f.run(t), "dns", tc.want)
		})
	}
}

func TestPortAndAPIHaveBoundedTimeouts(t *testing.T) {
	for _, phase := range []string{"port", "api", "dns"} {
		t.Run(phase, func(t *testing.T) {
			f := newFixture(t)
			f.deps.timeout = 20 * time.Millisecond
			if phase == "port" {
				f.deps.dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				}
			} else {
				original := f.r.HTTP.Transport
				f.r.HTTP.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
					if phase == "api" || strings.HasPrefix(req.URL.Path, "/dns") {
						<-req.Context().Done()
						return nil, req.Context().Err()
					}
					return original.RoundTrip(req)
				})
			}
			start := time.Now()
			got := f.run(t)
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Fatalf("timeout not bounded: %v", elapsed)
			}
			switch phase {
			case "port":
				statusOf(t, got, "controller", "warning")
				statusOf(t, got, "api", "skipped")
			case "api":
				c := statusOf(t, got, "api", "error")
				if !strings.Contains(c.Message, "超时") {
					t.Fatal("API timeout was not identified")
				}
			case "dns":
				statusOf(t, got, "dns", "error")
			}
		})
	}
}

func TestRoutesMatchDeviceAndHandlePartialFailure(t *testing.T) {
	for _, tc := range []struct {
		name, ipv4, ipv6, want string
		failed                 string
	}{
		{"found", "default dev tun0 table 2022", "", "ok", ""},
		{"ipv6", "", "default dev tun0", "ok", ""},
		{"no-substring", "default dev tun01", "default dev wlan0", "warning", ""},
		{"empty", "", "", "warning", ""},
		{"partial", "default dev tun0", "", "warning", "-6"},
		{"failed", "", "", "warning", "both"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.deps.routes = func(_ context.Context, args ...string) ([]byte, error) {
				if tc.failed == args[0] || tc.failed == "both" {
					return nil, errors.New(privateMarker)
				}
				if args[0] == "-4" {
					return []byte(tc.ipv4), nil
				}
				return []byte(tc.ipv6), nil
			}
			statusOf(t, f.run(t), "routes", tc.want)
		})
	}
}

func TestLocalClientDisablesProxyRedirectAndCookies(t *testing.T) {
	leaked := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		leaked++
	}))
	defer target.Close()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer "+privateMarker {
			t.Error("local request missing auth")
		}
		http.Redirect(w, req, target.URL, http.StatusFound)
	}))
	defer local.Close()
	proxyCalls := 0
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(*http.Request) (*url.URL, error) {
		proxyCalls++
		return nil, errors.New("proxy must not run")
	}
	original := &http.Client{Transport: transport}
	client, closeClient := localClient(original, time.Second)
	defer closeClient()
	req, err := http.NewRequest(http.MethodGet, local.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+privateMarker)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || leaked != 0 || proxyCalls != 0 {
		t.Fatalf("redirect/proxy leaked request: status=%d target=%d proxy=%d", resp.StatusCode, leaked, proxyCalls)
	}
	if original.CheckRedirect != nil || transport.Proxy == nil || client.Jar != nil {
		t.Fatal("shared client was mutated or cookie jar retained")
	}
}

func TestEmptyAPISecretAndTransportErrors(t *testing.T) {
	t.Run("empty-secret", func(t *testing.T) {
		f := newFixture(t)
		writeConfig(t, f.r.Root, "clash_api_port=9090\nclash_api_secret=''\n")
		f.r.HTTP.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Authorization") != "" {
				t.Fatal("unexpected authorization with empty secret")
			}
			body := tunDisabled
			if req.URL.Path == "/dns/query" {
				body = dnsOK
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		got := f.run(t)
		statusOf(t, got, "api", "warning")
		statusOf(t, got, "dns", "ok")
	})
	for _, phase := range []string{"api", "dns"} {
		t.Run(phase, func(t *testing.T) {
			f := newFixture(t)
			original := f.r.HTTP.Transport
			f.r.HTTP.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
				if phase == "api" || req.URL.Path == "/dns/query" {
					return nil, errors.New(privateMarker)
				}
				return original.RoundTrip(req)
			})
			statusOf(t, f.run(t), phase, "error")
		})
	}
}

func TestRunOutputFailuresAreSanitized(t *testing.T) {
	for _, r := range []*Runtime{nil, New(nil), New(&base.Runtime{})} {
		if err := r.Run(); err == nil {
			t.Fatal("missing output was not fatal")
		}
	}
	f := newFixture(t)
	f.r.Out = failingWriter{}
	if err := f.r.run(f.deps); err == nil || strings.Contains(err.Error(), privateMarker) {
		t.Fatalf("unsafe writer error: %v", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New(privateMarker) }
