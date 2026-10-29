//go:build linux || android

package base

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const configGenerationStatusLimit = 16 << 10

// InvalidateAutomationConfigGenerations invalidates tasks affected by a config
// replacement. Callers hold config.lock (after service.lock if needed) and must
// call this before replacing box.config. A failed later config write can only
// invalidate extra tasks. Missing status is left absent.
func InvalidateAutomationConfigGenerations(runDir, oldContent, newContent string) error {
	next, err := ParseAutomationConfig(newContent)
	if err != nil {
		return err
	}
	previous, oldErr := ParseAutomationConfig(oldContent)
	// A full config replacement may repair malformed automation assignments.
	// Their prior values are unknown, so invalidate both features in that case.
	recoveryChanged := oldErr != nil || previous.AutoRecovery != next.AutoRecovery
	updateChanged := oldErr != nil || previous.AutoUpdateConfig != next.AutoUpdateConfig ||
		previous.ConfigUpdateIntervalHours != next.ConfigUpdateIntervalHours ||
		previous.ConfigUpdateWiFiOnly != next.ConfigUpdateWiFiOnly
	if !recoveryChanged && !updateChanged {
		return nil
	}

	path := filepath.Join(runDir, "automation-status.json")
	var status map[string]json.RawMessage
	if err := ReadPrivateJSON(path, &status, configGenerationStatusLimit); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil || status == nil {
		return errors.New("automation status unavailable or invalid")
	}
	for key, changed := range map[string]bool{
		"recoveryGeneration": recoveryChanged,
		"updateGeneration":   updateChanged,
	} {
		if !changed {
			continue
		}
		var generation uint64
		if raw, ok := status[key]; ok {
			if err := json.Unmarshal(raw, &generation); err != nil {
				return errors.New("invalid automation generation")
			}
		}
		if generation == ^uint64(0) {
			return errors.New("automation generation overflow")
		}
		status[key], err = json.Marshal(generation + 1)
		if err != nil {
			return err
		}
	}
	return WritePrivateJSON(path, status, configGenerationStatusLimit)
}
