package base

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// ConfigSizeLimit is the maximum size in bytes of box.config.
const ConfigSizeLimit = 16 << 20

type Config struct {
	BinName         string
	CoreUserGroup   string
	APIPort         int
	APISecret       string
	ToggleAction    string
	ToggleTunTarget string
	ToggleModeCycle string
	CtrMode         string
	SelectOutbound  string
	TargetCellular  string
	TargetWiFi      string
	TargetWiFiList  string

	AutoRecovery              bool
	AutoUpdateConfig          bool
	ConfigUpdateIntervalHours int
	ConfigUpdateWiFiOnly      bool
}

func DefaultConfig() Config {
	return Config{
		BinName: "mihomo", CoreUserGroup: "root:net_admin", APIPort: 9090,
		ToggleAction: "service", ToggleTunTarget: "toggle", ToggleModeCycle: "rule,global,direct", CtrMode: "disable",
		ConfigUpdateIntervalHours: 24, ConfigUpdateWiFiOnly: true,
	}
}

func LoadConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, ConfigSizeLimit+1))
	if err != nil {
		return Config{}, err
	}
	return ParseConfig(string(data))
}

// ParseConfig reads literal shell assignments without executing user content.
func ParseConfig(content string) (Config, error) {
	return parseConfig(content, false)
}

// ParseAutomationConfig reads only automation assignments. Invalid statements and
// values for other fields are ignored so automation can be disabled independently.
func ParseAutomationConfig(content string) (Config, error) {
	return parseConfig(content, true)
}

func parseConfig(content string, automationOnly bool) (Config, error) {
	if len(content) > ConfigSizeLimit {
		return Config{}, fmt.Errorf("box.config exceeds %d bytes", ConfigSizeLimit)
	}
	c := DefaultConfig()
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, raw, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if automationOnly && !isAutomationKey(key) {
			// A malformed assignment to an automation field must still fail.
			if fields := strings.Fields(key); len(fields) == 0 || !isAutomationKey(fields[0]) {
				continue
			}
			return c, fmt.Errorf("box.config line %d: expected literal assignment", n)
		}
		if !ok {
			return c, fmt.Errorf("box.config line %d: expected literal assignment", n)
		}
		value, err := ParseLiteral(strings.TrimSpace(raw))
		if err != nil {
			return c, fmt.Errorf("box.config line %d: %w", n, err)
		}
		switch key {
		case "auto_recovery", "auto_update_config", "config_update_wifi_only":
			if value != "true" && value != "false" {
				return c, fmt.Errorf("box.config line %d: %s must be true or false", n, key)
			}
			enabled := value == "true"
			switch key {
			case "auto_recovery":
				c.AutoRecovery = enabled
			case "auto_update_config":
				c.AutoUpdateConfig = enabled
			case "config_update_wifi_only":
				c.ConfigUpdateWiFiOnly = enabled
			}
		case "config_update_interval_hours":
			c.ConfigUpdateIntervalHours, err = strconv.Atoi(value)
			if err != nil || c.ConfigUpdateIntervalHours < 1 || c.ConfigUpdateIntervalHours > 168 {
				return c, fmt.Errorf("box.config line %d: config_update_interval_hours must be 1..168", n)
			}
		case "bin_name":
			c.BinName = value
		case "CORE_USER_GROUP":
			c.CoreUserGroup = value
		case "clash_api_port":
			if value == "" {
				c.APIPort = 9090
			} else {
				c.APIPort, err = strconv.Atoi(value)
				if err != nil {
					return c, fmt.Errorf("invalid clash_api_port")
				}
			}
		case "clash_api_secret":
			c.APISecret = value
		case "toggle_action":
			c.ToggleAction = value
		case "toggle_tun_target":
			c.ToggleTunTarget = value
		case "toggle_mode_cycle":
			c.ToggleModeCycle = value
		case "ctr_mode":
			c.CtrMode = value
		case "select_outbound":
			c.SelectOutbound = value
		case "target_cellular":
			c.TargetCellular = value
		case "target_wifi":
			c.TargetWiFi = value
		case "target_wifi_list":
			c.TargetWiFiList = value
		}
	}
	if err := scanner.Err(); err != nil {
		return c, err
	}
	if automationOnly {
		return c, nil
	}
	if c.BinName == "" {
		c.BinName = "mihomo"
	}
	if strings.ContainsAny(c.BinName, "/\\\x00\r\n") || c.BinName == "." || c.BinName == ".." {
		return c, fmt.Errorf("invalid bin_name")
	}
	if c.APIPort < 1 || c.APIPort > 65535 {
		return c, fmt.Errorf("clash_api_port must be 1..65535")
	}
	if c.CoreUserGroup == "" {
		c.CoreUserGroup = "root:net_admin"
	}
	if c.ToggleAction == "" {
		c.ToggleAction = "service"
	}
	if c.ToggleTunTarget == "" {
		c.ToggleTunTarget = "toggle"
	}
	if c.ToggleModeCycle == "" {
		c.ToggleModeCycle = "rule,global,direct"
	}
	switch c.CtrMode {
	case "switch", "tun", "selector", "mode":
	default:
		c.CtrMode = "disable"
	}
	return c, nil
}

func isAutomationKey(key string) bool {
	switch key {
	case "auto_recovery", "auto_update_config", "config_update_interval_hours", "config_update_wifi_only":
		return true
	}
	return false
}

func ParseLiteral(raw string) (string, error) {
	var b strings.Builder
	var quote byte
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if quote == '\'' {
			if ch == '\'' {
				quote = 0
			} else {
				b.WriteByte(ch)
			}
			continue
		}
		if ch == '\\' {
			if i+1 >= len(raw) {
				return "", fmt.Errorf("trailing escape")
			}
			next := raw[i+1]
			if quote == '"' && next != '$' && next != '`' && next != '"' && next != '\\' {
				b.WriteByte(ch)
			} else {
				i++
				b.WriteByte(next)
			}
			continue
		}
		if quote == '"' {
			if ch == '"' {
				quote = 0
			} else {
				b.WriteByte(ch)
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '#':
			if i == 0 || raw[i-1] == ' ' || raw[i-1] == '\t' {
				return b.String(), nil
			}
			b.WriteByte(ch)
		case ' ', '\t':
			rest := strings.TrimSpace(raw[i:])
			if rest == "" || strings.HasPrefix(rest, "#") {
				return b.String(), nil
			}
			return "", fmt.Errorf("unquoted whitespace")
		case ';', '|', '&', '<', '>':
			return "", fmt.Errorf("expected literal value")
		default:
			b.WriteByte(ch)
		}
	}
	if quote != 0 {
		return "", fmt.Errorf("unterminated quote")
	}
	return b.String(), nil
}
