import { exec, enableEdgeToEdge } from "kernelsu";
import { parseKeyValueText } from "./config-parser";
import { getErrorMessage, buildExecError } from "./error-utils";
import type { ConfigPort } from "./config-port";
import type { CommandResult, ModuleInfo } from "./clash.types";

export type { CommandResult, ModuleInfo } from "./clash.types";

export interface LogFileInfo {
  path: string;
  size: number;
}

export interface LogSizeReport {
  files: LogFileInfo[];
  totalBytes: number;
  count: number;
}

const MODULE_ROOT = "/data/adb/modules/mono_box";
const BOX_ROOT = "/data/adb/box";
const BOX_CONFIG_PATH = "/data/adb/box/scripts/box.config";
const MODULE_PROP_PATH = "/data/adb/modules/mono_box/module.prop";

function toBase64Utf8(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary);
}

function normalizeResult(result: { errno: number; stdout?: string; stderr?: string }): CommandResult {
  return {
    errno: result.errno,
    stdout: result.stdout || "",
    stderr: result.stderr || "",
  };
}

export async function runActionScript(actionCmd: string): Promise<CommandResult> {
  const command = `su -c '${MODULE_ROOT}/action.sh ${actionCmd}'`;
  const result = await exec(command, { cwd: MODULE_ROOT });
  return normalizeResult(result);
}

function parsePositiveInteger(value: string | undefined): number {
  if (!value) return 0;
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

function parseLogSizeReport(stdout: string): LogSizeReport {
  const files: LogFileInfo[] = [];
  let totalBytes = 0;
  let count = 0;
  let hasTotal = false;

  for (const rawLine of stdout.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line) continue;

    const parts = line.split("\t");
    const kind = parts[0];

    if (kind === "file") {
      const size = parsePositiveInteger(parts[1]);
      const path = parts.slice(2).join("\t");
      if (!path) continue;
      files.push({ path, size });
      continue;
    }

    if (kind === "total") {
      totalBytes = parsePositiveInteger(parts[1]);
      count = parsePositiveInteger(parts[2]);
      hasTotal = true;
    }
  }

  if (!hasTotal) {
    totalBytes = files.reduce((sum, file) => sum + file.size, 0);
    count = files.length;
  }

  return { files, totalBytes, count };
}

export async function getLogSizeReport(): Promise<LogSizeReport> {
  const result = await runActionScript("logs_size");
  if (result.errno !== 0) {
    throw buildExecError("Read log size failed", result);
  }

  return parseLogSizeReport(result.stdout);
}

export async function clearLogFiles(): Promise<CommandResult> {
  const result = await runActionScript("clear_logs");
  if (result.errno !== 0) {
    throw buildExecError("Clear logs failed", result);
  }

  return result;
}

export async function setEdgeToEdge(enabled: boolean): Promise<void> {
  if (typeof enableEdgeToEdge === "function") {
    enableEdgeToEdge(enabled);
  }
}

export async function openExternalUrlCommand(url: string): Promise<CommandResult> {
  const safeUrl = url.replace(/"/g, '\\"');
  const command = `am start -a android.intent.action.VIEW -d "${safeUrl}"`;
  const result = await exec(command, { cwd: "/" });
  return normalizeResult(result);
}

export async function openExternalUrl(url: string): Promise<void> {
  const result = await openExternalUrlCommand(url);
  if (result.errno !== 0) {
    throw buildExecError("am start failed", result);
  }
}

export async function readModulePropRaw(): Promise<CommandResult> {
  const command = `su -c 'cat ${MODULE_PROP_PATH}'`;
  const result = await exec(command, { cwd: MODULE_ROOT });
  return normalizeResult(result);
}

export async function getModuleInfo(): Promise<ModuleInfo> {
  const result = await readModulePropRaw();
  if (result.errno !== 0 || !result.stdout) {
    throw buildExecError("Read module.prop failed", result);
  }

  const parsed = parseKeyValueText(result.stdout);
  return {
    id: parsed.id || "mono_box",
    name: parsed.name || "Mono Box",
    version: parsed.version || "unknown",
    versionCode: parsed.versionCode || "unknown",
    author: parsed.author || "",
    description: parsed.description || "",
    updateJson: parsed.updateJson || "",
  };
}

async function readBoxConfigRaw(): Promise<CommandResult> {
  const command = `su -c 'cat ${BOX_CONFIG_PATH}'`;
  const result = await exec(command, { cwd: BOX_ROOT });
  return normalizeResult(result);
}

async function writeBoxConfigRaw(content: string): Promise<CommandResult> {
  const normalizedContent = content.replace(/\r\n/g, "\n");
  const base64Content = toBase64Utf8(normalizedContent);
  let marker = "__MONO_BOX_B64_EOF__";
  while (base64Content.includes(marker)) {
    marker = `${marker}_X`;
  }

  const command = `su -c 'if command -v base64 >/dev/null 2>&1; then base64 -d; else busybox base64 -d; fi > ${BOX_CONFIG_PATH} <<"${marker}"\n${base64Content}\n${marker}'`;
  const result = await exec(command, { cwd: BOX_ROOT });
  return normalizeResult(result);
}

export async function readBoxConfig(): Promise<string> {
  const result = await readBoxConfigRaw();
  if (result.errno !== 0) {
    throw buildExecError("Read box.config", result);
  }
  if (!result.stdout) {
    throw new Error("Read box.config failed: empty file content");
  }
  return result.stdout;
}

export async function writeBoxConfig(content: string): Promise<void> {
  const result = await writeBoxConfigRaw(content);
  if (result.errno !== 0) {
    throw buildExecError("Write box.config", result);
  }
}

export const configPortAdapter: ConfigPort = {
  readBoxConfig,
  writeBoxConfig,
};
