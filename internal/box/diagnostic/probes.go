package diagnostic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

const responseLimit = 256 << 10

func readConfig(path string) (base.Config, bool, error) {
	// Resolve the trusted root just as the service does, then reject symlinks
	// and special files below it. O_NONBLOCK prevents a FIFO from hanging us.
	root, err := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(path)))
	if err != nil {
		return base.Config{}, false, err
	}
	path, err = filepath.Abs(filepath.Join(root, "scripts", "box.config"))
	if err != nil {
		return base.Config{}, false, err
	}
	f, err := base.OpenRegular(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return base.DefaultConfig(), true, nil
	}
	if err != nil {
		return base.Config{}, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, base.ConfigSizeLimit+1))
	if err != nil {
		return base.Config{}, false, err
	}
	config, err := base.ParseConfig(string(data))
	return config, false, err
}

func localClient(injected *http.Client, timeout time.Duration) (*http.Client, func()) {
	client := http.Client{}
	if injected != nil {
		client = *injected
	}
	transport := client.Transport
	if transport == nil {
		transport = base.NewDirectHTTPTransport()
	}
	closeClient := func() {}
	if t, ok := transport.(*http.Transport); ok {
		local := t.Clone()
		local.Proxy = nil
		transport = local
		closeClient = local.CloseIdleConnections
	}
	client.Transport = transport
	client.Jar = nil
	client.Timeout = timeout
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client, closeClient
}

func queryAPI(client *http.Client, config base.Config, path string, timeout time.Duration) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	endpoint := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(config.APIPort)) + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	if config.APISecret != "" {
		req.Header.Set("Authorization", "Bearer "+config.APISecret)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Even error bodies can contain configuration, URLs or credentials.
		return nil, resp.StatusCode, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if len(data) > responseLimit {
		return nil, resp.StatusCode, errors.New("API response too large")
	}
	return data, resp.StatusCode, nil
}

func apiFailure(status int, err error) string {
	switch {
	case status == 401:
		return "API 鉴权失败（HTTP 401）；请核对模块与核心的 API 密钥。"
	case status == 403:
		return "API 拒绝访问（HTTP 403）；请核对密钥和控制器访问限制。"
	case errors.Is(err, context.DeadlineExceeded):
		return "本机 API 请求超时；请检查核心是否仍在响应。"
	case err != nil:
		return "本机 API 请求或响应读取失败；请检查核心状态和控制器设置。"
	default:
		return fmt.Sprintf("本机 API 返回 HTTP %d；请核对核心状态和控制器设置。", status)
	}
}

type tunConfig struct {
	Enable *bool  `json:"enable"`
	Device string `json:"device"`
}

func parseConfigs(data []byte) (tunConfig, bool) {
	var config map[string]json.RawMessage
	if json.Unmarshal(data, &config) != nil || config == nil || len(config) == 0 {
		return tunConfig{}, false
	}
	// A valid Mihomo configs response always includes a known operating mode.
	var mode string
	if json.Unmarshal(config["mode"], &mode) != nil || (mode != "rule" && mode != "global" && mode != "direct") {
		return tunConfig{}, false
	}
	var tun tunConfig
	if value, ok := config["tun"]; ok {
		if json.Unmarshal(value, &tun) != nil {
			return tunConfig{}, false
		}
	}
	return tun, true
}

func checkDNS(client *http.Client, config base.Config, timeout time.Duration) (string, string) {
	data, status, err := queryAPI(client, config, "/dns/query?name=example.com&type=A", timeout)
	switch {
	case status == 404 || status == 405 || status == 501:
		return "skipped", "当前核心 API 不支持 DNS 查询，已跳过此项检查。"
	case err != nil || status != 200:
		return "error", "核心 DNS 查询失败：" + apiFailure(status, err)
	}
	var reply struct {
		Status *int `json:"Status"`
		Answer []struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if json.Unmarshal(data, &reply) != nil || reply.Status == nil || *reply.Status < 0 || *reply.Status > 15 {
		return "error", "核心 DNS 查询返回的数据格式异常。"
	}
	if *reply.Status != 0 {
		return "error", fmt.Sprintf("核心解析 example.com 返回 DNS 错误（代码 %d）。", *reply.Status)
	}
	for _, answer := range reply.Answer {
		if ip := net.ParseIP(answer.Data); answer.Type == 1 && ip != nil && ip.To4() != nil {
			return "ok", "Mihomo 已通过自身 DNS 查询接口返回 example.com 的 A 记录。"
		}
	}
	return "warning", "核心 DNS 查询已完成，但未返回有效的 A 记录。"
}

func validDevice(device string) bool {
	if len(device) == 0 || len(device) > 15 || device == "." || device == ".." {
		return false
	}
	for _, ch := range device {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' || ch == '.') {
			return false
		}
	}
	return true
}

func inspectTun(device string) (bool, error) {
	if !validDevice(device) {
		return false, errors.New("invalid TUN device")
	}
	iface, err := net.InterfaceByName(device)
	if err != nil {
		return false, err
	}
	// Distinguish a TUN from an unrelated interface with the configured name.
	f, err := os.Open(filepath.Join("/sys/class/net", device, "tun_flags"))
	if err != nil {
		return false, err
	}
	defer f.Close()
	flags, err := io.ReadAll(io.LimitReader(f, 64))
	if err != nil {
		return false, err
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(flags)), 0, 32)
	if err != nil || value&1 == 0 {
		return false, errors.New("not a TUN interface")
	}
	return iface.Flags&net.FlagUp != 0, nil
}

func checkRoutes(deps dependencies, device string) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), deps.timeout)
	defer cancel()
	found, failed := false, false
	for _, family := range []string{"-4", "-6"} {
		data, err := deps.routes(ctx, family, "route", "show", "table", "all", "dev", device)
		if err != nil {
			failed = true
			continue
		}
		// Match fields, not substrings (tun0 must not match tun01).
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			for i := 0; i+1 < len(fields); i++ {
				if fields[i] == "dev" && fields[i+1] == device {
					found = true
				}
			}
		}
	}
	switch {
	case found && failed:
		return "warning", "已发现 TUN 接口的相关路由，但部分路由查询失败；尚未验证策略规则和流量转发。"
	case found:
		return "ok", "已发现 TUN 接口的相关路由；此项仅核查路由条目，尚未验证策略规则和流量转发。"
	case failed:
		return "warning", "无法完整读取 TUN 路由；请检查 ip 命令、访问权限和 IPv4/IPv6 支持。"
	default:
		return "warning", "未发现 TUN 接口的相关路由；请核对核心自动路由或手动路由设置。"
	}
}

type boundedOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, errors.New("route output exceeds limit")
	}
	return b.buffer.Write(p)
}

func routeOutput(ctx context.Context, args ...string) ([]byte, error) {
	command := "/system/bin/ip"
	if _, err := os.Stat(command); err != nil {
		command = "ip"
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.WaitDelay = 100 * time.Millisecond
	out := &boundedOutput{limit: 64 << 10}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.buffer.Bytes(), nil
}
