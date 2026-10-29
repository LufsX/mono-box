package box

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (r *Runtime) reloadController() (*Runtime, error) {
	config, err := LoadConfig(filepath.Join(r.Root, "scripts", "box.config"))
	if err != nil {
		return nil, err
	}
	current := *r
	current.Config = config
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
	if exists(filepath.Join(r.ModuleDir, "disable")) || exists(filepath.Join(r.Root, "manual")) || r.Config.CtrMode == "disable" {
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
		if !start && pid > 0 {
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

// The marker lasts for the entire Wi-Fi period, including SSID changes.
// It is committed before the API mutation, so retries cannot replace the
// cellular baseline even when an API request times out after being applied.
func (r *Runtime) applySavedPolicy(wifi bool, target string) error {
	policy := r.Config.CtrMode
	lastName := ".lastClashMode"
	if policy == "selector" {
		if r.Config.SelectOutbound == "" {
			return errors.New("selector group is empty")
		}
		lastName = ".lastOutbound"
	}
	last := filepath.Join(r.RunDir(), lastName)
	marker := filepath.Join(r.RunDir(), ".policy-wifi-"+policy)
	if !wifi && target == "last" {
		data, err := os.ReadFile(last)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		target = strings.TrimSpace(string(data))
	}
	if target == "" {
		if !wifi {
			return removePolicyMarker(marker)
		}
		return nil
	}
	if policy == "mode" && !validMode(target) {
		return fmt.Errorf("invalid Clash mode %q", target)
	}
	if wifi && !exists(marker) {
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
		if err = AtomicWrite(last, []byte(current), 0600); err != nil {
			return err
		}
		if err = AtomicWrite(marker, []byte("wifi"), 0600); err != nil {
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
		return removePolicyMarker(marker)
	}
	return nil
}

func removePolicyMarker(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
