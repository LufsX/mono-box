package box

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const apiTimeout = 15 * time.Second

// apiRequest keeps injected transports, but never uses environment proxies for
// the local API. A copied client avoids changing the caller's shared client.
func (r *Runtime) apiRequest(method, path string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	port := r.Config.APIPort
	if port == 0 {
		port = 9090
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("invalid API port")
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, "http://"+net.JoinHostPort("127.0.0.1", strconv.Itoa(port))+path, body)
	if err != nil {
		return nil, err
	}
	if r.Config.APISecret != "" {
		req.Header.Set("Authorization", "Bearer "+r.Config.APISecret)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := http.Client{}
	if r.HTTP != nil {
		client = *r.HTTP
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if t, ok := transport.(*http.Transport); ok {
		local := t.Clone()
		local.Proxy = nil
		transport = local
		defer local.CloseIdleConnections()
	}
	client.Transport = transport
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Clash API %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("read Clash API: %w", err)
	}
	if len(data) > 8<<20 {
		return nil, errors.New("Clash API response exceeds 8 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(string(data))
		if len(detail) > 512 {
			detail = detail[:512]
		}
		return nil, fmt.Errorf("Clash API %s %s: HTTP %d: %s", method, path, resp.StatusCode, detail)
	}
	return data, nil
}

func (r *Runtime) GetConfigs() ([]byte, error) {
	data, err := r.apiRequest(http.MethodGet, "/configs", nil)
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("invalid configs JSON: %w", err)
	}
	if obj == nil {
		return nil, errors.New("configs must be a JSON object")
	}
	return data, nil
}

func validMode(mode string) bool { return mode == "rule" || mode == "global" || mode == "direct" }

func (r *Runtime) SetMode(mode string) error {
	if !validMode(mode) {
		return fmt.Errorf("invalid Clash mode %q", mode)
	}
	_, err := r.apiRequest(http.MethodPatch, "/configs", map[string]string{"mode": mode})
	return err
}

func (r *Runtime) SetTun(enable bool) error {
	_, err := r.apiRequest(http.MethodPatch, "/configs", map[string]any{"tun": map[string]bool{"enable": enable}})
	return err
}

func (r *Runtime) GetNowMode() (string, error) {
	data, err := r.GetConfigs()
	if err != nil {
		return "", err
	}
	var config struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return "", err
	}
	config.Mode = strings.ToLower(config.Mode)
	if !validMode(config.Mode) {
		return "", errors.New("configs.mode is missing or invalid")
	}
	return config.Mode, nil
}

func (r *Runtime) GetNowOutbound() (string, error) {
	return r.getOutbound(r.Config.SelectOutbound)
}

func (r *Runtime) getOutbound(group string) (string, error) {
	if group == "" {
		return "", errors.New("selector group is empty")
	}
	data, err := r.apiRequest(http.MethodGet, "/proxies/"+url.PathEscape(group), nil)
	if err != nil {
		return "", err
	}
	var result struct {
		Now string `json:"now"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("invalid proxy JSON: %w", err)
	}
	if result.Now == "" {
		return "", errors.New("proxy.now is missing or empty")
	}
	return result.Now, nil
}

func (r *Runtime) SetOutbound(group, name string) error {
	if group == "" || name == "" {
		return errors.New("selector group and outbound must be nonempty")
	}
	_, err := r.apiRequest(http.MethodPut, "/proxies/"+url.PathEscape(group), map[string]string{"name": name})
	return err
}

func (r *Runtime) UpgradeCore() ([]byte, error) {
	return r.apiRequest(http.MethodPost, "/upgrade", map[string]any{})
}

// Action implements the module button, lifecycle, log and API actions.
func (r *Runtime) Action(action string, args []string) error {
	current, err := r.reloadController()
	if err != nil {
		return err
	}
	r = current
	want := 0
	if action == "switch_mode" || action == "switch_tun" {
		want = 1
	}
	if len(args) != want {
		return fmt.Errorf("%s expects %d arguments", action, want)
	}
	switch action {
	case "start", "stop", "restart", "status":
		return r.Service(action)
	case "logs_size", "log_size":
		return r.LogsSize()
	case "clear_logs", "clean_logs":
		return r.ClearLogs()
	case "", "toggle":
		return r.toggleAction()
	case "switch_mode":
		return r.SetMode(args[0])
	case "switch_tun":
		if args[0] != "true" && args[0] != "false" {
			return errors.New("TUN target must be true or false")
		}
		return r.SetTun(args[0] == "true")
	case "get_configs", "upgrade_core":
		var data []byte
		if action == "get_configs" {
			data, err = r.GetConfigs()
		} else {
			data, err = r.UpgradeCore()
		}
		if err != nil {
			return err
		}
		if r.Out != nil {
			_, err = fmt.Fprintln(r.Out, string(data))
		}
		return err
	case "-h", "--help":
		if r.Out != nil {
			_, err = fmt.Fprintln(r.Out, "toggle|start|stop|restart|status|switch_mode <rule|global|direct>|switch_tun <true|false>|get_configs|upgrade_core")
		}
		return err
	default:
		return fmt.Errorf("unknown action %q", action)
	}
}

func (r *Runtime) toggleAction() error {
	switch r.Config.ToggleAction {
	case "service", "":
		pid, err := r.RunningPID()
		err = controllerRunningError(err)
		if err != nil {
			return err
		}
		if pid > 0 {
			return r.Service("stop")
		}
		return r.Service("start")
	case "tun":
		var enable bool
		switch r.Config.ToggleTunTarget {
		case "on":
			enable = true
		case "off":
			enable = false
		case "toggle", "":
			data, err := r.GetConfigs()
			if err != nil {
				return err
			}
			var config struct {
				Tun struct {
					Enable *bool `json:"enable"`
				} `json:"tun"`
			}
			if err = json.Unmarshal(data, &config); err != nil {
				return err
			}
			if config.Tun.Enable == nil {
				return errors.New("configs.tun.enable is missing or invalid")
			}
			enable = !*config.Tun.Enable
		default:
			return fmt.Errorf("invalid toggle_tun_target %q", r.Config.ToggleTunTarget)
		}
		return r.SetTun(enable)
	case "mode_cycle":
		modes := []string{}
		normalized := strings.Map(func(ch rune) rune {
			if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
				return -1
			}
			if ch == ';' {
				return ','
			}
			return ch
		}, strings.ToLower(r.Config.ToggleModeCycle))
		for _, mode := range strings.Split(normalized, ",") {
			if !validMode(mode) {
				return fmt.Errorf("invalid mode cycle entry %q", mode)
			}
			modes = append(modes, mode)
		}
		current, err := r.GetNowMode()
		if err != nil {
			return err
		}
		next := modes[0]
		for i, mode := range modes {
			if mode == current {
				next = modes[(i+1)%len(modes)]
				break
			}
		}
		return r.SetMode(next)
	default:
		return fmt.Errorf("invalid toggle_action %q", r.Config.ToggleAction)
	}
}
