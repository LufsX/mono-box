package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LufsX/mono-box/internal/box/base"
)

func controllerTestDeps(t *testing.T) controllerDeps {
	t.Helper()
	deps := defaultControllerDeps()
	deps.netPath = filepath.Join(t.TempDir(), "rt_tables")
	controllerTestWrite(t, deps.netPath, "100 wlan0")
	deps.output = func(context.Context, string, ...string) ([]byte, error) { return []byte(`mWifiInfo SSID: "home"`), nil }
	deps.ready = func(context.Context) (bool, error) { return true, nil }
	deps.running = func(*Runtime) (int, error) { return 0, nil }
	deps.bootPlan = func(*Runtime) (uint64, error) { return 0, nil }
	deps.bootStart = func(*Runtime, uint64) (bool, error) { return true, nil }
	deps.service = func(*Runtime, string) error { t.Error("unexpected service action"); return nil }
	return deps
}

func TestPolicyRestoresCellularAcrossWifiEventsAndSSID(t *testing.T) {
	for _, policy := range []string{"mode", "selector"} {
		t.Run(policy, func(t *testing.T) {
			r := controllerTestRuntime(t, "")
			deps := controllerTestDeps(t)
			current := "direct"
			wifiTarget := "global"
			if policy == "selector" {
				current = "cellular node"
				wifiTarget = "wifi node"
			}
			baseline := current
			writes := 0
			server := controllerTestServer(t, r, func(w http.ResponseWriter, req *http.Request) {
				if req.Method == "GET" {
					if strings.HasPrefix(req.URL.Path, "/proxies/") {
						json.NewEncoder(w).Encode(map[string]string{"now": current})
					} else {
						json.NewEncoder(w).Encode(map[string]string{"mode": current})
					}
					return
				}
				var payload map[string]string
				json.NewDecoder(req.Body).Decode(&payload)
				current = payload["mode"]
				if policy == "selector" {
					current = payload["name"]
				}
				writes++
				w.WriteHeader(204)
			})
			config := "clash_api_port=" + controllerTestPort(server) + "\nctr_mode=" + policy + "\nselect_outbound=PROXY\ntarget_cellular=last\ntarget_wifi='" + wifiTarget + "'\n"
			path := filepath.Join(r.Root, "scripts", "box.config")
			controllerTestWrite(t, path, config)
			event := []string{"wc", deps.netPath}
			for i := 0; i < 3; i++ {
				if err := r.controller(context.Background(), event, deps); err != nil {
					t.Fatal(err)
				}
			}
			if current != wifiTarget {
				t.Fatal(current)
			}
			// A different SSID selects another target but retains the cellular baseline.
			mapped := "rule"
			if policy == "selector" {
				mapped = "other wifi"
			}
			controllerTestWrite(t, path, config+"target_wifi_list='home,"+mapped+"'\n")
			if err := r.controller(context.Background(), event, deps); err != nil {
				t.Fatal(err)
			}
			controllerTestWrite(t, deps.netPath, "100 rmnet_data0")
			if err := r.controller(context.Background(), event, deps); err != nil {
				t.Fatal(err)
			}
			if current != baseline {
				t.Fatalf("baseline overwritten: got %q want %q", current, baseline)
			}
			path = filepath.Join(r.RunDir(), ".policy-"+policy+".json")
			var saved savedPolicy
			if err := base.ReadPrivateJSON(path, &saved, 16<<10); err != nil || saved.Baseline != baseline || saved.WiFi {
				t.Fatalf("saved %+v: %v", saved, err)
			}
			if writes != 5 {
				t.Fatal(writes)
			}
			// A later Wi-Fi period captures a new user-selected cellular value.
			current = "rule"
			controllerTestWrite(t, deps.netPath, "100 wlan0")
			if err := r.controller(context.Background(), event, deps); err != nil {
				t.Fatal(err)
			}
			if err := base.ReadPrivateJSON(path, &saved, 16<<10); err != nil || saved.Baseline != "rule" || !saved.WiFi {
				t.Fatalf("new baseline %+v: %v", saved, err)
			}
		})
	}
}

func TestPolicySwitchUsesProcessAndPropagatesErrors(t *testing.T) {
	r := controllerTestRuntime(t, "ctr_mode=switch\ntarget_wifi=start")
	deps := controllerTestDeps(t)
	calls := 0
	sentinel := errors.New("service failed")
	deps.service = func(*Runtime, string) error { calls++; return sentinel }
	deps.running = func(*Runtime) (int, error) { return 123, nil }
	if err := r.controller(context.Background(), []string{"w", deps.netPath}, deps); err != nil || calls != 0 {
		t.Fatalf("%d %v", calls, err)
	}
	deps.running = func(*Runtime) (int, error) { return 0, ErrStopped }
	if err := r.controller(context.Background(), []string{"w", deps.netPath}, deps); !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("%d %v", calls, err)
	}
	deps.running = func(*Runtime) (int, error) { return 0, errors.Join(ErrStopped, sentinel) }
	if err := r.controller(context.Background(), []string{"w", deps.netPath}, deps); !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("%d %v", calls, err)
	}
	controllerTestWrite(t, filepath.Join(r.Root, "scripts", "box.config"), "ctr_mode=switch\ntarget_wifi=stop")
	deps.running = func(*Runtime) (int, error) { return 456, nil }
	if err := r.controller(context.Background(), []string{"w", deps.netPath}, deps); !errors.Is(err, sentinel) || calls != 2 {
		t.Fatalf("%d %v", calls, err)
	}
}

func TestPolicyFailedMutationKeepsBaseline(t *testing.T) {
	r := controllerTestRuntime(t, "")
	deps := controllerTestDeps(t)
	current := "direct"
	fail := true
	server := controllerTestServer(t, r, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]string{"mode": current})
			return
		}
		if fail {
			w.WriteHeader(503)
			return
		}
		var payload map[string]string
		json.NewDecoder(req.Body).Decode(&payload)
		current = payload["mode"]
		w.WriteHeader(204)
	})
	controllerTestWrite(t, filepath.Join(r.Root, "scripts", "box.config"), "clash_api_port="+controllerTestPort(server)+"\nctr_mode=mode\ntarget_wifi=global\ntarget_cellular=last")
	event := []string{"w", deps.netPath}
	if err := r.controller(context.Background(), event, deps); err == nil {
		t.Fatal("missing mutation error")
	}
	// Simulate a timed-out mutation which actually reached the core.
	current = "global"
	fail = false
	if err := r.controller(context.Background(), event, deps); err != nil {
		t.Fatal(err)
	}
	controllerTestWrite(t, deps.netPath, "100 rmnet0")
	if err := r.controller(context.Background(), event, deps); err != nil {
		t.Fatal(err)
	}
	if current != "direct" {
		t.Fatalf("baseline overwritten after failed request: %s", current)
	}
}

func TestPolicyManualDisableAndHTTPFailure(t *testing.T) {
	r := controllerTestRuntime(t, "")
	deps := controllerTestDeps(t)
	requests := 0
	server := controllerTestServer(t, r, func(w http.ResponseWriter, req *http.Request) {
		requests++
		w.WriteHeader(503)
		io.WriteString(w, "not ready")
	})
	controllerTestWrite(t, filepath.Join(r.Root, "scripts", "box.config"), "clash_api_port="+controllerTestPort(server)+"\nctr_mode=tun\ntarget_wifi=false")
	event := []string{"w", deps.netPath}
	for _, path := range []string{filepath.Join(r.Root, "manual"), filepath.Join(r.ModuleDir, "disable")} {
		controllerTestWrite(t, path, "")
		if err := r.controller(context.Background(), event, deps); err != nil || requests != 0 {
			t.Fatalf("%d %v", requests, err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.controller(context.Background(), event, deps); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatal(err)
	}
	controllerTestWrite(t, filepath.Join(r.Root, "scripts", "box.config"), "ctr_mode=tun\ntarget_wifi=bad")
	if err := r.controller(context.Background(), event, deps); err == nil {
		t.Fatal("accepted invalid target")
	}
}
