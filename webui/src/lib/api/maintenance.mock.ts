import type { AutomationSettings, AutomationState, DiagnosticCheck, DiagnosticReport } from "./maintenance";
import { readBoxConfig, writeBoxConfig } from "./action.mock";
import { readConfigValue, upsertConfigValue } from "./config-parser";

let nextRunAt = 0;
let previousSettings: AutomationSettings | undefined;

async function snapshot(): Promise<AutomationState> {
  const content = await readBoxConfig();
  const settings: AutomationSettings = {
    recoveryEnabled: readConfigValue(content, "auto_recovery", "false") === "true",
    updateEnabled: readConfigValue(content, "auto_update_config", "false") === "true",
    updateIntervalHours: Number(readConfigValue(content, "config_update_interval_hours", "24")),
    updateWifiOnly: readConfigValue(content, "config_update_wifi_only", "true") === "true",
  };
  if (!settings.updateEnabled) nextRunAt = 0;
  else if (!previousSettings?.updateEnabled || previousSettings.updateIntervalHours !== settings.updateIntervalHours) {
    nextRunAt = Date.now() + settings.updateIntervalHours * 3_600_000;
  }
  previousSettings = settings;
  return {
    settings,
    recovery: { state: settings.recoveryEnabled ? "watching" : "disabled", attempts: 0, nextAttemptAt: 0, lastError: "" },
    update: {
      state: settings.updateEnabled ? "waiting" : "disabled",
      lastAttemptAt: 0,
      lastSuccessAt: 0,
      nextRunAt,
      lastError: "",
      updated: 0,
    },
  };
}

const pause = () => new Promise<void>((resolve) => setTimeout(resolve, 250));

export async function getAutomationState(): Promise<AutomationState> {
  await pause();
  return snapshot();
}

export async function setAutomationSettings(value: AutomationSettings): Promise<AutomationState> {
  await pause();
  let content = await readBoxConfig();
  const updates = {
    auto_recovery: String(value.recoveryEnabled),
    auto_update_config: String(value.updateEnabled),
    config_update_interval_hours: String(value.updateIntervalHours),
    config_update_wifi_only: String(value.updateWifiOnly),
  };
  for (const [key, setting] of Object.entries(updates)) {
    content = upsertConfigValue(content, key, setting);
  }
  await writeBoxConfig(content);
  return snapshot();
}

export async function runDiagnostics(): Promise<DiagnosticReport> {
  await pause();
  const generatedAt = Date.now();
  const checks: DiagnosticCheck[] = [
    { id: "native", title: "模块运行环境", status: "ok", message: "开发预览：模拟模块文件和运行目录可用。" },
    { id: "core", title: "内核状态", status: "ok", message: "开发预览：模拟内核正在运行。" },
    { id: "tun", title: "TUN 接口", status: "warning", message: "开发预览：模拟 TUN 接口未启用，请按使用需求检查。" },
    { id: "dns", title: "DNS 查询", status: "error", message: "开发预览：模拟查询超时，用于预览异常项目样式。" },
    { id: "network", title: "网络探测", status: "skipped", message: "开发预览：浏览器中跳过设备探测。" },
  ];
  const labels = { ok: "正常", warning: "注意", error: "异常", skipped: "已跳过" };
  return {
    generatedAt,
    checks,
    text: [
      "Mono Box 诊断报告（开发预览，模拟数据）",
      new Date(generatedAt).toLocaleString("zh-CN"),
      ...checks.map((check) => `[${labels[check.status]}] ${check.title}：${check.message}`),
    ].join("\n"),
  };
}
