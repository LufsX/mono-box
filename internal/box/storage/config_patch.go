package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/LufsX/mono-box/internal/box/base"
)

// PatchConfig merges form updates into the latest on-disk config while holding
// the service lock followed by the config lock.
func (r *Runtime) PatchConfig(encoded string) error {
	data, err := base.DecodeText(encoded, base.ConfigSizeLimit)
	if err != nil {
		return err
	}
	updates := map[string]string{}
	if err = json.Unmarshal(data, &updates); err != nil || updates == nil {
		return errors.New("invalid config patch")
	}
	allowed := map[string]bool{}
	for _, key := range []string{"bin_name", "CORE_USER_GROUP", "clash_api_port", "clash_api_secret", "toggle_action", "toggle_tun_target", "toggle_mode_cycle", "ctr_mode", "select_outbound", "target_cellular", "target_wifi", "target_wifi_list", "auto_recovery", "auto_update_config", "config_update_interval_hours", "config_update_wifi_only"} {
		allowed[key] = true
	}
	for key, value := range updates {
		if !allowed[key] || strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("invalid config patch field")
		}
		if _, err := base.ParseLiteral(value); err != nil {
			return errors.New("invalid config patch value")
		}
	}
	unlock, err := r.storageLock()
	if err != nil {
		return err
	}
	defer unlock()
	lock, err := r.storageConfigLock()
	if err != nil {
		return err
	}
	defer lock.Close()
	dir, err := openStorageDir(filepath.Join(r.Root, "scripts"), true)
	if err != nil {
		return err
	}
	defer dir.Close()
	content, err := dir.read("box.config", base.ConfigSizeLimit)
	if err != nil {
		return err
	}
	merged := base.MergeConfigPatch(string(content), updates)
	if _, err = base.ParseConfig(merged); err != nil {
		return errors.New("config patch would produce invalid configuration")
	}
	if err = base.InvalidateAutomationConfigGenerations(r.RunDir(), string(content), merged); err != nil {
		return err
	}
	if err = dir.write("box.config", []byte(merged)); err != nil {
		return err
	}
	return nil
}

// storageConfigLock must be called with the service lock already held.
func (r *Runtime) storageConfigLock() (*os.File, error) {
	ctx, cancel := context.WithTimeout(context.Background(), base.ServiceLockTimeout)
	defer cancel()
	return base.AcquireConfigLock(ctx, filepath.Join(r.Root, "run"))
}

func mergeConfigPatch(content string, updates map[string]string) string {
	return base.MergeConfigPatch(content, updates)
}
