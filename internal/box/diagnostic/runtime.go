// Package diagnostic produces a bounded, shareable local health report.
// All messages are curated summaries: external errors and probe output must
// never be copied into the report.
package diagnostic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
	"github.com/LufsX/mono-box/internal/box/service"
)

type Runtime struct {
	*base.Runtime
}

func New(runtime *base.Runtime) *Runtime { return &Runtime{Runtime: runtime} }

type check struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type report struct {
	GeneratedAt int64   `json:"generatedAt"`
	Checks      []check `json:"checks"`
	Text        string  `json:"text"`
}

type dependencies struct {
	now     func() time.Time
	config  func(string) (base.Config, bool, error)
	pid     func(*base.Runtime) (int, error)
	dial    func(context.Context, string, string) (net.Conn, error)
	tun     func(string) (bool, error)
	routes  func(context.Context, ...string) ([]byte, error)
	timeout time.Duration
}

func defaultDependencies() dependencies {
	return dependencies{
		now: time.Now, config: readConfig,
		pid:  func(r *base.Runtime) (int, error) { return service.New(r).InspectPID() },
		dial: (&net.Dialer{}).DialContext, tun: inspectTun,
		routes: routeOutput, timeout: 2 * time.Second,
	}
}

// Run writes one JSON report. Unhealthy checks are report data, not execution
// errors. Only inability to produce or write the report is a fatal error.
func (r *Runtime) Run() error { return r.run(defaultDependencies()) }

func (r *Runtime) run(deps dependencies) error {
	if r == nil || r.Runtime == nil || r.Out == nil {
		return errors.New("诊断报告输出不可用")
	}
	result := report{GeneratedAt: deps.now().UnixMilli(), Checks: make([]check, 0, 7)}
	add := func(id, title, status, message string) {
		result.Checks = append(result.Checks, check{id, title, status, message})
	}
	skipNetwork := func(reason string) {
		add("api", "API 鉴权", "skipped", reason)
		add("tun", "TUN 设备", "skipped", reason)
		add("routes", "TUN 路由", "skipped", reason)
		add("dns", "Mihomo DNS", "skipped", reason)
	}
	finish := func() error {
		var text strings.Builder
		fmt.Fprintf(&text, "Mono Box 诊断报告\n生成时间：%s\n", time.UnixMilli(result.GeneratedAt).UTC().Format(time.RFC3339))
		labels := map[string]string{"ok": "正常", "warning": "注意", "error": "异常", "skipped": "跳过"}
		for _, c := range result.Checks {
			fmt.Fprintf(&text, "\n[%s] %s（%s）\n%s\n", labels[c.Status], c.Title, c.ID, c.Message)
		}
		result.Text = text.String()
		if err := json.NewEncoder(r.Out).Encode(result); err != nil {
			// A writer error may itself contain user data.
			return errors.New("无法写入诊断报告")
		}
		return nil
	}

	cfg, missing, err := deps.config(filepath.Join(r.Root, "scripts", "box.config"))
	if err != nil {
		add("config", "模块配置", "error", "box.config 无法读取或包含无效配置；请检查文件权限、赋值语法和端口范围。")
		add("service", "核心服务", "skipped", "模块配置无效，已跳过进程身份检查。")
		add("controller", "控制器端口", "skipped", "模块配置无效，已跳过端口检查。")
		skipNetwork("模块配置无效，已跳过依赖配置的检查。")
		return finish()
	}
	current := r.Runtime.WithConfig(cfg)
	if missing {
		add("config", "模块配置", "warning", "box.config 不存在；以下检查使用默认设置。")
	} else {
		add("config", "模块配置", "ok", "box.config 可读取，赋值语法和模块参数校验通过。")
	}

	pid, pidErr := deps.pid(current)
	running := pidErr == nil && pid > 1
	stopped := errors.Is(pidErr, service.ErrStopped)
	switch {
	case running:
		add("service", "核心服务", "ok", "已验证受管核心的可执行文件、启动身份和命令行。")
	case stopped:
		add("service", "核心服务", "warning", "未发现经身份验证的核心进程；服务未启动或 PID 记录已失效。")
	case errors.Is(pidErr, base.ErrServiceLockTimeout):
		add("service", "核心服务", "warning", "服务正在执行其他操作；本次未等待服务锁，请稍后重试。")
	default:
		add("service", "核心服务", "warning", "无法完成核心身份检查；请检查运行目录和进程信息的访问权限。")
	}

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.APIPort))
	ctx, cancel := context.WithTimeout(context.Background(), deps.timeout)
	conn, portErr := deps.dial(ctx, "tcp", address)
	cancel()
	if conn != nil {
		conn.Close()
	}
	switch {
	case portErr == nil && running:
		add("controller", "控制器端口", "ok", "本机配置的控制器 TCP 端口可连接。")
	case portErr == nil && stopped:
		add("controller", "控制器端口", "warning", "未发现受管核心，但控制器端口正在监听；可能被其他进程占用。")
	case portErr == nil:
		add("controller", "控制器端口", "warning", "控制器端口正在监听；核心身份尚未确认，未验证端口所属进程。")
	case errors.Is(portErr, syscall.ECONNREFUSED) && stopped:
		add("controller", "控制器端口", "skipped", "核心未启动，本机控制器端口未监听。")
	case errors.Is(portErr, syscall.ECONNREFUSED):
		add("controller", "控制器端口", "error", "本机控制器端口拒绝连接；请核对核心状态和控制器端口配置。")
	default:
		add("controller", "控制器端口", "warning", "本机控制器端口连接失败或超时；请检查监听地址和访问权限。")
	}
	if !running {
		reason := "核心身份尚未确认，已跳过此项检查。"
		if stopped {
			reason = "核心未启动，已跳过此项检查。"
		}
		skipNetwork(reason)
		return finish()
	}
	if portErr != nil {
		skipNetwork("控制器端口不可达，已跳过依赖 API 的检查。")
		return finish()
	}

	client, closeClient := localClient(current.HTTP, deps.timeout)
	defer closeClient()
	data, status, err := queryAPI(client, current.Config, "/configs", deps.timeout)
	if err != nil || status != 200 {
		add("api", "API 鉴权", "error", apiFailure(status, err))
		add("tun", "TUN 设备", "skipped", "无法获取核心 TUN 设置，已跳过设备检查。")
		add("routes", "TUN 路由", "skipped", "无法获取核心 TUN 设置，已跳过路由检查。")
		add("dns", "Mihomo DNS", "skipped", "API 不可用，已跳过核心 DNS 查询。")
		return finish()
	}
	tun, valid := parseConfigs(data)
	if !valid {
		add("api", "API 鉴权", "error", "API 返回的配置数据格式异常；请核对控制器端口是否属于 Mihomo。")
		add("tun", "TUN 设备", "skipped", "API 配置格式异常，已跳过设备检查。")
		add("routes", "TUN 路由", "skipped", "API 配置格式异常，已跳过路由检查。")
		add("dns", "Mihomo DNS", "skipped", "API 配置格式异常，已跳过核心 DNS 查询。")
		return finish()
	}
	if cfg.APISecret == "" {
		add("api", "API 鉴权", "warning", "本机 API 可访问，但模块未配置 API 密钥；请核对核心鉴权设置。")
	} else {
		add("api", "API 鉴权", "ok", "本机 API 接受配置的密钥，并返回有效配置数据。")
	}

	switch {
	case tun.Enable == nil:
		add("tun", "TUN 设备", "warning", "API 未提供有效的 TUN 开关状态，无法确认设备设置。")
		add("routes", "TUN 路由", "skipped", "TUN 开关状态未知，已跳过路由检查。")
	case !*tun.Enable:
		add("tun", "TUN 设备", "ok", "核心已关闭 TUN，无需检查 TUN 设备。")
		add("routes", "TUN 路由", "skipped", "核心已关闭 TUN，已跳过路由检查。")
	case !validDevice(tun.Device):
		add("tun", "TUN 设备", "warning", "API 未提供可核查的 TUN 设备名；请检查核心 TUN 设置。")
		add("routes", "TUN 路由", "skipped", "TUN 设备名不可用，已跳过路由检查。")
	default:
		up, err := deps.tun(tun.Device)
		switch {
		case err != nil:
			add("tun", "TUN 设备", "error", "TUN 已开启，但配置的 TUN 接口不存在或无法核查；请检查设备和访问权限。")
		case !up:
			add("tun", "TUN 设备", "error", "TUN 接口存在，但接口未启用。")
		default:
			add("tun", "TUN 设备", "ok", "配置的 TUN 接口存在且已启用。")
		}
		if err != nil || !up {
			add("routes", "TUN 路由", "skipped", "TUN 设备不可用，已跳过路由检查。")
		} else {
			state, message := checkRoutes(deps, tun.Device)
			add("routes", "TUN 路由", state, message)
		}
	}
	dnsState, dnsMessage := checkDNS(client, cfg, deps.timeout)
	add("dns", "Mihomo DNS", dnsState, dnsMessage)
	return finish()
}
