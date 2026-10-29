package control

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func delayTestRuntime(t *testing.T, handler http.HandlerFunc) (*Runtime, int, *bytes.Buffer) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	address, _ := url.Parse(server.URL)
	_, portText, _ := net.SplitHostPort(address.Host)
	port, _ := strconv.Atoi(portText)
	var out bytes.Buffer
	r := &Runtime{Runtime: &base.Runtime{Out: &out, Err: io.Discard, HTTP: server.Client()}}
	return r, port, &out
}
func encodeDelayRequest(t *testing.T, request proxyDelayRequest) string {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(data)
}
func TestProxyDelayRouteAndAliases(t *testing.T) {
	var paths []string
	r, port, out := delayTestRuntime(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.EscapedPath())
		if got := req.Header.Get("Authorization"); got != "Bearer test" {
			t.Errorf("%s", got)
		}
		if req.URL.Query().Get("timeout") != "32767" || req.URL.Query().Get("url") != "https://example.com/probe?a=b&c=d" {
			t.Error(req.URL.RawQuery)
		}
		if len(paths) == 1 {
			w.WriteHeader(404)
			io.WriteString(w, `{"message":"Resource not found"}`)
			return
		}
		io.WriteString(w, `{"delay":42}`)
	})
	timeout := 99999.0
	request := proxyDelayRequest{Port: port, Secret: "test", Name: "HK / 专线", Aliases: []string{"backup", "backup"}, ProviderName: "订阅/A", URL: "https://example.com/probe?a=b&c=d", Timeout: &timeout}
	if err := r.ProxyDelay(encodeDelayRequest(t, request)); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "/providers/proxies/"+url.PathEscape("订阅/A")+"/"+url.PathEscape("HK / 专线")+"/healthcheck" {
		t.Fatal(paths)
	}
	if out.String() != "{\"delay\":42}\n" {
		t.Fatal(out.String())
	}
}
func TestProxyDelayFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		success bool
	}{
		{"probe failed", 503, `{"message":"An error occurred in the delay test"}`, true},
		{"probe timeout", 504, `{"message":"Timeout"}`, true},
		{"authentication", 401, `{"message":"Unauthorized"}`, false},
		{"server error", 500, `{"message":"internal error"}`, false},
		{"bad JSON", 200, "not JSON", false},
		{"missing delay", 200, `{}`, false},
		{"zero delay", 200, `{"delay":0}`, false},
		{"missing resource", 404, `{"message":"Resource not found"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, port, out := delayTestRuntime(t, func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(tc.status); io.WriteString(w, tc.body) })
			err := r.ProxyDelay(encodeDelayRequest(t, proxyDelayRequest{Port: port, Name: "DIRECT"}))
			if tc.success {
				if err != nil || out.String() != "{\"delay\":0}\n" {
					t.Fatalf("%s %v", out, err)
				}
			} else if err == nil {
				t.Fatalf("accepted %s", out)
			}
		})
	}
}
func TestProxyDelayRequestValidationAndDeadline(t *testing.T) {
	r := &Runtime{Runtime: &base.Runtime{Out: io.Discard}}
	for _, request := range []proxyDelayRequest{{Port: 0, Name: "DIRECT"}, {Port: 9090}, {Port: 9090, Name: "N", URL: "file:///etc/passwd"}} {
		if err := r.ProxyDelay(encodeDelayRequest(t, request)); err == nil {
			t.Fatal("accepted invalid request")
		}
	}
	timeout := 1.0
	var remaining time.Duration
	r.HTTP = &http.Client{Transport: delayRoundTripper(func(req *http.Request) (*http.Response, error) {
		deadline, _ := req.Context().Deadline()
		remaining = time.Until(deadline)
		return nil, context.DeadlineExceeded
	})}
	if err := r.ProxyDelay(encodeDelayRequest(t, proxyDelayRequest{Port: 9090, Name: "N", Timeout: &timeout})); err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatal(err)
	}
	if remaining < 2*time.Second || remaining > 3*time.Second {
		t.Fatalf("deadline %s", remaining)
	}
}

type delayRoundTripper func(*http.Request) (*http.Response, error)

func (f delayRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
