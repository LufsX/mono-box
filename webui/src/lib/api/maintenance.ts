import { runNativeCommand } from "./native";
import { buildExecError } from "./error-utils";
import * as mock from "./maintenance.mock";

export interface DiagnosticCheck {
  id: string;
  title: string;
  status: "ok" | "warning" | "error" | "skipped";
  message: string;
}

export interface DiagnosticReport {
  generatedAt: number;
  checks: DiagnosticCheck[];
  text: string;
}

export interface AutomationSettings {
  recoveryEnabled: boolean;
  updateEnabled: boolean;
  updateIntervalHours: number;
  updateWifiOnly: boolean;
}

export interface AutomationState {
  settings: AutomationSettings;
  recovery: {
    state: string;
    attempts: number;
    nextAttemptAt: number;
    lastError: string;
  };
  update: {
    state: string;
    lastAttemptAt: number;
    lastSuccessAt: number;
    nextRunAt: number;
    lastError: string;
    updated: number;
  };
}

function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("维护服务返回的数据格式无效");
  return value as Record<string, unknown>;
}

function string(value: unknown): string {
  if (typeof value !== "string") throw new Error("维护服务返回的文本格式无效");
  return value;
}

function number(value: unknown): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) throw new Error("维护服务返回的数值格式无效");
  return value;
}

function validateAutomationSettings(value: AutomationSettings): AutomationSettings {
  if (typeof value.recoveryEnabled !== "boolean" || typeof value.updateEnabled !== "boolean" || typeof value.updateWifiOnly !== "boolean") {
    throw new Error("自动化开关设置无效");
  }
  if (!Number.isInteger(value.updateIntervalHours) || value.updateIntervalHours < 1 || value.updateIntervalHours > 168) {
    throw new Error("更新间隔必须是 1 至 168 小时的整数");
  }
  return {
    recoveryEnabled: value.recoveryEnabled,
    updateEnabled: value.updateEnabled,
    updateIntervalHours: value.updateIntervalHours,
    updateWifiOnly: value.updateWifiOnly,
  };
}

function parseAutomationState(value: unknown): AutomationState {
  const data = record(value);
  const settings = record(data.settings);
  const recovery = record(data.recovery);
  const update = record(data.update);
  return {
    settings: validateAutomationSettings({
      recoveryEnabled: settings.recoveryEnabled as boolean,
      updateEnabled: settings.updateEnabled as boolean,
      updateIntervalHours: settings.updateIntervalHours as number,
      updateWifiOnly: settings.updateWifiOnly as boolean,
    }),
    recovery: {
      state: string(recovery.state),
      attempts: number(recovery.attempts),
      nextAttemptAt: number(recovery.nextAttemptAt),
      lastError: string(recovery.lastError),
    },
    update: {
      state: string(update.state),
      lastAttemptAt: number(update.lastAttemptAt),
      lastSuccessAt: number(update.lastSuccessAt),
      nextRunAt: number(update.nextRunAt),
      lastError: string(update.lastError),
      updated: number(update.updated),
    },
  };
}

function parseJson(stdout: string): unknown {
  try {
    return JSON.parse(stdout);
  } catch {
    throw new Error("维护服务返回的 JSON 无效，请重试");
  }
}

export async function runDiagnostics(): Promise<DiagnosticReport> {
  if (import.meta.env.MODE !== "production") return mock.runDiagnostics();
  const result = await runNativeCommand(["diagnostics"]);
  if (result.errno !== 0) throw buildExecError("运行诊断", result);
  const data = record(parseJson(result.stdout));
  if (!Array.isArray(data.checks)) throw new Error("诊断项目格式无效");
  return {
    generatedAt: number(data.generatedAt),
    checks: data.checks.map((value) => {
      const check = record(value);
      const status = string(check.status);
      if (!["ok", "warning", "error", "skipped"].includes(status)) throw new Error("诊断状态格式无效");
      return {
        id: string(check.id),
        title: string(check.title),
        status: status as DiagnosticCheck["status"],
        message: string(check.message),
      };
    }),
    text: string(data.text),
  };
}

export async function getAutomationState(): Promise<AutomationState> {
  if (import.meta.env.MODE !== "production") return mock.getAutomationState();
  const result = await runNativeCommand(["automation", "get"]);
  if (result.errno !== 0) throw buildExecError("读取自动化设置", result);
  return parseAutomationState(parseJson(result.stdout));
}
