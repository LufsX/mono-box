import { enableEdgeToEdge } from "kernelsu";
import { runCommand, type CommandOutputListener } from "../bridge";
import { runNativeCommand, shellQuote, toBase64Utf8 } from "./native";
import { parseKeyValueText } from "./config-parser";
import { buildExecError } from "./error-utils";
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

export type MihomoConfigFileKind = "current" | "profile";

export interface MihomoConfigFile {
  kind: MihomoConfigFileKind;
  name: string;
  path: string;
  size: number;
  updatedAt: number;
  sourceUrl: string;
  active: boolean;
}

export function normalizeMihomoConfigName(value: string): string {
  const rawName = value.split(/[\\/]/).filter(Boolean).pop() || "config";
  let name = rawName
    .normalize("NFC")
    .replace(/[\u0000-\u001f\u007f:*?"<>|]+/g, "_")
    .replace(/\.{2,}/g, "_")
    .trim()
    .replace(/^_+|_+$/g, "");

  if (!name || name === "." || name === "..") name = "config";
  if (!/\.ya?ml$/i.test(name)) name = `${name}.yaml`;
  return name;
}

function fromBase64Utf8(text: string): string {
  if (!text) return "";

  const binary = atob(text);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }

  return new TextDecoder().decode(bytes);
}

export async function runActionScript(actionCmd: string, onLine?: CommandOutputListener): Promise<CommandResult> {
  if (/[\r\n\0]/.test(actionCmd)) throw new Error("Invalid action command");
  const parts = actionCmd.trim().split(/[ \t]+/);
  const [action, value] = parts;
  if (parts.length === 1) {
    switch (action) {
      case "toggle":
      case "start":
      case "stop":
      case "restart":
      case "status":
      case "get_configs":
      case "upgrade_core":
      case "-h":
      case "--help":
        return runNativeCommand(["action", action], onLine);
      case "logs_size":
      case "log_size":
        return runNativeCommand(["service", "logs_size"], onLine);
      case "clear_logs":
      case "clean_logs":
        return runNativeCommand(["service", "clear_logs"], onLine);
    }
  }
  if (parts.length === 2 && (
    (action === "switch_mode" && ["rule", "global", "direct"].includes(value)) ||
    (action === "switch_tun" && ["true", "false"].includes(value))
  )) {
    return runNativeCommand(["action", action, value], onLine);
  }
  throw new Error("Invalid action command or arguments");
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

function parseMihomoConfigRows(stdout: string): MihomoConfigFile[] {
  const files: MihomoConfigFile[] = [];

  for (const rawLine of stdout.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line) continue;

    const [kindValue, name, path, sizeValue, mtimeValue, sourceValue, activeValue] = line.split("\t");
    const kind: MihomoConfigFileKind = kindValue === "current" ? "current" : "profile";
    if (!name || !path) continue;

    const size = parsePositiveInteger(sizeValue);
    const mtimeSeconds = parsePositiveInteger(mtimeValue);

    files.push({
      kind,
      name,
      path,
      size,
      updatedAt: mtimeSeconds > 0 ? mtimeSeconds * 1000 : 0,
      sourceUrl: fromBase64Utf8(sourceValue || ""),
      active: activeValue === "true",
    });
  }

  return files;
}

export async function readLogFile(path: string): Promise<string> {
  const result = await runNativeCommand(["logs", "read", toBase64Utf8(path)]);
  if (result.errno !== 0) throw buildExecError("Read log file failed", result);
  return result.stdout;
}

export async function openLogFileLocation(path: string): Promise<CommandResult> {
  const result = await runNativeCommand(["logs", "open", toBase64Utf8(path)]);
  if (result.errno !== 0) throw buildExecError("Open log file location failed", result);
  return result;
}

export async function listMihomoConfigFiles(): Promise<MihomoConfigFile[]> {
  const result = await runNativeCommand(["profiles", "list"]);
  if (result.errno !== 0) throw buildExecError("List Mihomo configs failed", result);
  return parseMihomoConfigRows(result.stdout);
}

async function runProfileCommand(args: string[], message: string): Promise<MihomoConfigFile> {
  const result = await runNativeCommand(["profiles", ...args]);
  if (result.errno !== 0) throw buildExecError(message, result);
  const [file] = parseMihomoConfigRows(result.stdout);
  if (!file) throw new Error(`${message}: empty result`);
  return file;
}

export async function importMihomoConfigFile(name: string, content: string): Promise<MihomoConfigFile> {
  return runProfileCommand([
    "import", toBase64Utf8(normalizeMihomoConfigName(name)), toBase64Utf8(content.replace(/\r\n/g, "\n")),
  ], "Import Mihomo config failed");
}

export async function downloadMihomoConfigFromUrl(url: string, name: string): Promise<MihomoConfigFile> {
  return runProfileCommand([
    "download", toBase64Utf8(url.trim()), toBase64Utf8(normalizeMihomoConfigName(name.trim() || "remote-config.yaml")),
  ], "Download Mihomo config failed");
}

export async function updateMihomoConfigFromUrl(name: string): Promise<MihomoConfigFile> {
  return runProfileCommand(["update", toBase64Utf8(normalizeMihomoConfigName(name))], "Update Mihomo config failed");
}

export async function switchMihomoConfigFile(name: string, kind: MihomoConfigFileKind = "profile"): Promise<MihomoConfigFile> {
  if (kind !== "current" && kind !== "profile") throw new Error("Invalid profile kind");
  return runProfileCommand(["switch", toBase64Utf8(normalizeMihomoConfigName(name)), kind], "Switch Mihomo config failed");
}

export async function deleteMihomoConfigFile(name: string): Promise<void> {
  const result = await runNativeCommand(["profiles", "delete", toBase64Utf8(normalizeMihomoConfigName(name))]);
  if (result.errno !== 0) throw buildExecError("Delete Mihomo config failed", result);
}

export async function setEdgeToEdge(enabled: boolean): Promise<void> {
  if (typeof enableEdgeToEdge === "function") {
    enableEdgeToEdge(enabled);
  }
}

export async function openExternalUrlCommand(url: string): Promise<CommandResult> {
  return runCommand(`am start -a android.intent.action.VIEW -d ${shellQuote(url)}`, { cwd: "/" });
}

export async function openExternalUrl(url: string): Promise<void> {
  const result = await openExternalUrlCommand(url);
  if (result.errno !== 0) throw buildExecError("am start failed", result);
}

export async function readModulePropRaw(): Promise<CommandResult> {
  return runNativeCommand(["module", "info"]);
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
  return runNativeCommand(["config", "read"]);
}

async function writeBoxConfigRaw(content: string): Promise<CommandResult> {
  return runNativeCommand(["config", "write", toBase64Utf8(content.replace(/\r\n/g, "\n"))]);
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

export async function patchBoxConfig(updates: Record<string, string>): Promise<void> {
  const result = await runNativeCommand(["config", "patch", toBase64Utf8(JSON.stringify(updates))]);
  if (result.errno !== 0) throw buildExecError("Patch box.config", result);
}

export const configPortAdapter: ConfigPort = {
  readBoxConfig,
  writeBoxConfig,
  updateBoxConfigValues: patchBoxConfig,
};
