package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
	"github.com/LufsX/mono-box/internal/box/service"
)

func (r *Runtime) reloadController() (*Runtime, error) {
	config, err := base.LoadConfig(filepath.Join(r.Root, "scripts", "box.config"))
	if err != nil {
		return nil, err
	}
	current := *r
	current.Runtime = r.Runtime.WithConfig(config)
	return &current, nil
}

func androidOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	// Android binaries must not depend on a caller's PATH.
	child, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	data, err := exec.CommandContext(child, filepath.Join("/system/bin", name), args...).Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return data, nil
}

func (r *Runtime) policyTarget(wifi bool, ssid string) string {
	if !wifi {
		return r.Config.TargetCellular
	}
	for _, entry := range strings.Split(r.Config.TargetWiFiList, ";") {
		key, target, ok := strings.Cut(entry, ",")
		if ok && key == ssid && target != "" {
			return target
		}
	}
	return r.Config.TargetWiFi
}

func wifiSSID(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "mWifiInfo") {
			continue
		}
		if _, tail, ok := strings.Cut(line, "\""); ok {
			if value, _, ok := strings.Cut(tail, "\""); ok {
				return value
			}
		}
	}
	return "<unknown ssid>"
}

func (r *Runtime) applyNetwork(ctx context.Context, deps controllerDeps) error {
	if base.Exists(filepath.Join(r.ModuleDir, "disable")) || base.Exists(filepath.Join(r.Root, "manual")) || r.Config.CtrMode == "disable" {
		return nil
	}
	tables, err := os.ReadFile(deps.netPath)
	if err != nil {
		return fmt.Errorf("read rt_tables: %w", err)
	}
	wifi := strings.Contains(string(tables), "wlan")
	ssid := ""
	if wifi && r.Config.TargetWiFiList != "" {
		data, err := deps.output(ctx, "dumpsys", "wifi")
		if err != nil {
			return err
		}
		ssid = wifiSSID(data)
	}
	target := r.policyTarget(wifi, ssid)
	switch r.Config.CtrMode {
	case "switch":
		var start bool
		switch target {
		case "":
			return nil
		case "start", "on", "true":
			start = true
		case "stop", "off", "false":
			start = false
		default:
			return fmt.Errorf("invalid switch target %q", target)
		}
		pid, err := deps.running(r)
		err = controllerRunningError(err)
		if err != nil {
			return err
		}
		if start && pid == 0 {
			return deps.service(r, "start")
		}
		if !start {
			// A dead core can still have a running intent. Record policy stop
			// even then, so recovery cannot undo this network decision.
			if pid == 0 {
				return service.New(r.Runtime).EnsureStopped()
			}
			return deps.service(r, "stop")
		}
		return nil
	case "tun":
		if target == "" {
			return nil
		}
		if target != "true" && target != "false" {
			return fmt.Errorf("invalid TUN target %q", target)
		}
		return r.SetTun(target == "true")
	case "selector", "mode":
		return r.applySavedPolicy(wifi, target)
	default:
		return fmt.Errorf("invalid controller mode %q", r.Config.CtrMode)
	}
}

type savedPolicy struct {
	Baseline string `json:"baseline"`
	WiFi     bool   `json:"wifi"`
}

// Baseline and Wi-Fi ownership are committed together before the API mutation.
// Retries and SSID changes must retain the cellular value even when a request
// times out after being applied. Callers hold the controller lock.
func (r *Runtime) applySavedPolicy(wifi bool, target string) error {
	policy := r.Config.CtrMode
	name := ".policy-mode.json"
	if policy == "selector" {
		if r.Config.SelectOutbound == "" {
			return errors.New("selector group is empty")
		}
		name = ".policy-selector.json"
	}
	path := filepath.Join(r.RunDir(), name)
	var saved savedPolicy
	if err := base.ReadPrivateJSON(path, &saved, 16<<10); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !wifi && target == "last" {
		target = saved.Baseline
	}
	if target == "" {
		if !wifi {
			return saved.endWiFi(path)
		}
		return nil
	}
	if policy == "mode" && !validMode(target) {
		return fmt.Errorf("invalid Clash mode %q", target)
	}
	if wifi && !saved.WiFi {
		var current string
		var err error
		if policy == "mode" {
			current, err = r.GetNowMode()
		} else {
			current, err = r.GetNowOutbound()
		}
		if err != nil {
			return err
		}
		saved = savedPolicy{Baseline: current, WiFi: true}
		if err = base.WritePrivateJSON(path, saved, 16<<10); err != nil {
			return err
		}
	}
	var err error
	if policy == "mode" {
		err = r.SetMode(target)
	} else {
		err = r.SetOutbound(r.Config.SelectOutbound, target)
	}
	if err != nil {
		return err
	}
	if !wifi {
		return saved.endWiFi(path)
	}
	return nil
}

func (s savedPolicy) endWiFi(path string) error {
	if !s.WiFi {
		return nil
	}
	s.WiFi = false
	return base.WritePrivateJSON(path, s, 16<<10)
}
