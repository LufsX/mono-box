import { exec, enableEdgeToEdge } from "kernelsu";

export interface CommandResult {
  errno: number;
  stdout: string;
  stderr: string;
}

export interface ModuleInfo {
  id: string;
  name: string;
  version: string;
  versionCode: string;
  author: string;
  description: string;
  updateJson: string;
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

function parseKeyValueText(content: string): Record<string, string> {
  const result: Record<string, string> = {};
  const lines = content.split(/\r?\n/);

  for (const rawLine of lines) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) {
      continue;
    }

    const separatorIndex = line.indexOf("=");
    if (separatorIndex <= 0) {
      continue;
    }

    const key = line.slice(0, separatorIndex).trim();
    const value = line.slice(separatorIndex + 1).trim();
    result[key] = value;
  }

  return result;
}

function buildErrorDetails(result: CommandResult, fallbackMessage: string): Error {
  const details = [result.stderr, result.stdout]
    .filter((part) => typeof part === "string" && part.trim().length > 0)
    .join("\n")
    .trim();
  return new Error(details || `${fallbackMessage} (errno=${result.errno})`);
}

export async function runActionScript(actionCmd: string): Promise<CommandResult> {
  const command = `su -c '${MODULE_ROOT}/action.sh ${actionCmd}'`;
  const result = await exec(command, { cwd: MODULE_ROOT });
  return normalizeResult(result);
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
    throw buildErrorDetails(result, "am start failed");
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
    throw buildErrorDetails(result, "Read module.prop failed");
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

export async function readBoxConfigRaw(): Promise<CommandResult> {
  const command = `su -c 'cat ${BOX_CONFIG_PATH}'`;
  const result = await exec(command, { cwd: BOX_ROOT });
  return normalizeResult(result);
}

export async function writeBoxConfigRaw(content: string): Promise<CommandResult> {
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
