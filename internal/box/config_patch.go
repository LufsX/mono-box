package box

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"
)

// PatchConfig merges form updates into the latest on-disk config under the same
// lock used by service startup and profile selection.
func (r *Runtime) PatchConfig(encoded string) error {
	data, err := decodeStorageText(encoded, storageContentLimit)
	if err != nil {
		return err
	}
	updates := map[string]string{}
	if err = json.Unmarshal(data, &updates); err != nil || updates == nil {
		return errors.New("invalid config patch")
	}
	allowed := map[string]bool{}
	for _, key := range []string{"bin_name", "CORE_USER_GROUP", "clash_api_port", "clash_api_secret", "toggle_action", "toggle_tun_target", "toggle_mode_cycle", "ctr_mode", "select_outbound", "target_cellular", "target_wifi", "target_wifi_list"} {
		allowed[key] = true
	}
	for key, value := range updates {
		if !allowed[key] || strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("invalid config patch field")
		}
		if _, err := parseLiteral(value); err != nil {
			return errors.New("invalid config patch value")
		}
	}
	unlock, err := r.storageLock()
	if err != nil {
		return err
	}
	defer unlock()
	dir, err := openStorageDir(filepath.Join(r.Root, "scripts"), true)
	if err != nil {
		return err
	}
	defer dir.Close()
	content, err := dir.read("box.config", storageContentLimit)
	if err != nil {
		return err
	}
	merged := mergeConfigPatch(string(content), updates)
	if _, err = ParseConfig(merged); err != nil {
		return errors.New("config patch would produce invalid configuration")
	}
	if err = dir.write("box.config", []byte(merged)); err != nil {
		return err
	}
	return nil
}

func mergeConfigPatch(content string, updates map[string]string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	last := map[string]int{}
	for i, line := range lines {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "#") {
			continue
		}
		key, _, ok := strings.Cut(strings.TrimPrefix(text, "export "), "=")
		if ok {
			last[strings.TrimSpace(key)] = i
		}
	}
	keys := make([]string, 0, len(updates))
	for key := range updates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := updates[key]
		if i, ok := last[key]; ok {
			eq := strings.IndexByte(lines[i], '=')
			raw := lines[i][eq+1:]
			comment := configComment(raw)
			lines[i] = lines[i][:eq+1] + value
			if comment != "" {
				lines[i] += " " + comment
			}
		} else {
			if len(lines) > 0 && lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			lines = append(lines, key+"="+value, "")
		}
	}
	return strings.Join(lines, "\n")
}

func configComment(raw string) string {
	var quote byte
	escaped := false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '#' && (i == 0 || raw[i-1] == ' ' || raw[i-1] == '\t') {
			return raw[i:]
		}
	}
	return ""
}
