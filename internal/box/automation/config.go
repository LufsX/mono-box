package automation

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/LufsX/mono-box/internal/box/base"
)

const configName = "box.config"

// All callers hold config.lock. Directory descriptors pin reads and replacements
// even if an ancestor is renamed during the operation.
func (r *Runtime) loadSettings() (Settings, error) {
	dir, err := base.OpenDirectory(filepath.Join(r.Root, "scripts"), false)
	if errors.Is(err, os.ErrNotExist) {
		return defaultSettings(), nil
	}
	if err != nil {
		return Settings{}, errors.New("automation configuration directory unavailable")
	}
	defer dir.Close()
	content, err := readBoxConfig(dir)
	if err != nil {
		return Settings{}, err
	}
	return settingsFromConfig(content)
}

func settingsFromConfig(content string) (Settings, error) {
	config, err := base.ParseAutomationConfig(content)
	if err != nil {
		return Settings{}, errors.New("automation configuration unavailable or invalid")
	}
	return Settings{
		RecoveryEnabled: config.AutoRecovery, UpdateEnabled: config.AutoUpdateConfig,
		UpdateIntervalHours: config.ConfigUpdateIntervalHours, UpdateWifiOnly: config.ConfigUpdateWiFiOnly,
	}, nil
}

func settingsPatch(settings Settings) map[string]string {
	return map[string]string{
		"auto_recovery":                strconv.FormatBool(settings.RecoveryEnabled),
		"auto_update_config":           strconv.FormatBool(settings.UpdateEnabled),
		"config_update_interval_hours": strconv.Itoa(settings.UpdateIntervalHours),
		"config_update_wifi_only":      strconv.FormatBool(settings.UpdateWifiOnly),
	}
}

func (r *Runtime) saveSettings(settings Settings) error {
	dir, err := base.OpenDirectory(filepath.Join(r.Root, "scripts"), true)
	if err != nil {
		return errors.New("automation configuration directory unavailable")
	}
	defer dir.Close()
	content, err := readBoxConfig(dir)
	if err != nil {
		return err
	}
	merged := base.MergeConfigPatch(content, settingsPatch(settings))
	return writeBoxConfig(dir, merged)
}

func readConfigFile(dir *os.File, name string, limit int64) ([]byte, error) {
	f, err := base.OpenRegularAt(dir, name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("automation configuration unreadable or too large")
	}
	return data, nil
}

func readBoxConfig(dir *os.File) (string, error) {
	data, err := readConfigFile(dir, configName, base.ConfigSizeLimit)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", errors.New("automation configuration unavailable or invalid")
	}
	return string(data), nil
}

func writeBoxConfig(dir *os.File, content string) error {
	if _, err := settingsFromConfig(content); err != nil {
		return err
	}
	// Reject unsafe existing targets before the atomic replacement.
	f, err := base.OpenRegularAt(dir, configName, os.O_RDONLY, 0)
	if err == nil {
		f.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("automation configuration target unavailable or unsafe")
	}
	path := filepath.Join("/proc/self/fd", strconv.Itoa(int(dir.Fd())), configName)
	if err := base.AtomicWrite(path, []byte(content), 0600); err != nil {
		return errors.New("cannot save automation configuration")
	}
	// Persist the replacement directory entry before reporting success.
	if err := dir.Sync(); err != nil {
		return errors.New("cannot sync automation configuration")
	}
	return nil
}
