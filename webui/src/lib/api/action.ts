import { exec, enableEdgeToEdge } from "kernelsu";
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

const MODULE_ROOT = "/data/adb/modules/mono_box";
const BOX_ROOT = "/data/adb/box";
const BOX_CONFIG_PATH = "/data/adb/box/scripts/box.config";
const MODULE_PROP_PATH = "/data/adb/modules/mono_box/module.prop";
const MIHOMO_CONFIG_ROOT = "/data/adb/box/mihomo";
const MIHOMO_CORE_PATH = "/data/adb/box/bin/mihomo";
const MIHOMO_PROFILE_ROOT = "/data/adb/box/mihomo/.mono-box/profiles";
const MIHOMO_PROFILE_SOURCE_ROOT = "/data/adb/box/mihomo/.mono-box/sources";
const MIHOMO_PROFILE_ACTIVE_PATH = "/data/adb/box/mihomo/.mono-box/active-profile";

function toBase64Utf8(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary);
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

function buildResolveLogPathScript(encodedPath: string): string[] {
  return [
    `encoded_path=${encodedPath}`,
    "if command -v base64 >/dev/null 2>&1; then",
    '  file=$(printf "%s" "$encoded_path" | base64 -d 2>/dev/null) || exit 1',
    "elif command -v busybox >/dev/null 2>&1; then",
    '  file=$(printf "%s" "$encoded_path" | busybox base64 -d 2>/dev/null) || exit 1',
    "else",
    '  printf "%s\\n" "Error: base64 command not found" >&2',
    "  exit 1",
    "fi",
    'case "$file" in',
    "  /*) ;;",
    '  *) printf "%s\\n" "Error: log path is not absolute" >&2; exit 1 ;;',
    "esac",
    'case "$file" in',
    '  *"/../"*|*"/./"*) printf "%s\\n" "Error: log path is not allowed" >&2; exit 1 ;;',
    "esac",
    "log_name=${file##*/}",
    "log_dir=${file%/*}",
    "core_name=${log_dir##*/}",
    'case "$log_name" in',
    '  "$core_name"_*.log) ;;',
    '  *) printf "%s\\n" "Error: log path is not allowed" >&2; exit 1 ;;',
    "esac",
    '[ -f "$file" ] || { printf "%s\\n" "Error: log file not found" >&2; exit 1; }',
  ];
}

async function runRootShellScript(lines: string[], cwd = MODULE_ROOT): Promise<CommandResult> {
  const command = `su -c '${lines.join("\n")}'`;
  const result = await exec(command, { cwd });
  return normalizeResult(result);
}

function buildMihomoConfigShellHelpers(): string[] {
  return [
    `config_dir=${MIHOMO_CONFIG_ROOT}`,
    `core_bin=${MIHOMO_CORE_PATH}`,
    `profile_dir=${MIHOMO_PROFILE_ROOT}`,
    `source_dir=${MIHOMO_PROFILE_SOURCE_ROOT}`,
    `active_file=${MIHOMO_PROFILE_ACTIVE_PATH}`,
    "b64_decode() {",
    "  if command -v base64 >/dev/null 2>&1; then",
    '    printf "%s" "$1" | base64 -d 2>/dev/null',
    "  elif command -v busybox >/dev/null 2>&1; then",
    '    printf "%s" "$1" | busybox base64 -d 2>/dev/null',
    "  else",
    '    printf "%s\\n" "Error: base64 command not found" >&2',
    "    return 1",
    "  fi",
    "}",
    "b64_encode() {",
    "  if command -v base64 >/dev/null 2>&1; then",
    '    printf "%s" "$1" | base64 2>/dev/null | tr -d "\\r\\n"',
    "  elif command -v busybox >/dev/null 2>&1; then",
    '    printf "%s" "$1" | busybox base64 2>/dev/null | tr -d "\\r\\n"',
    "  fi",
    "}",
    "file_size() {",
    '  size=$(wc -c < "$1" 2>/dev/null | tr -d "[:space:]")',
    '  case "$size" in ""|*[!0-9]*) size=0 ;; esac',
    '  printf "%s" "$size"',
    "}",
    "file_mtime() {",
    '  mtime=$(stat -c "%Y" "$1" 2>/dev/null)',
    '  if [ -z "$mtime" ] && command -v busybox >/dev/null 2>&1; then',
    '    mtime=$(busybox stat -c "%Y" "$1" 2>/dev/null)',
    "  fi",
    '  case "$mtime" in ""|*[!0-9]*) mtime=0 ;; esac',
    '  printf "%s" "$mtime"',
    "}",
    "sanitize_profile_name() {",
    '  raw="${1##*/}"',
    '  raw=$(printf "%s" "$raw" | tr -c "A-Za-z0-9._-" "_" | sed "s/^_*//; s/_*$//")',
    '  case "$raw" in ""|"."|"..") raw="config" ;; esac',
    '  case "$raw" in *.yaml|*.yml) ;; *) raw="${raw}.yaml" ;; esac',
    '  printf "%s" "$raw"',
    "}",
    "print_config_row() {",
    '  row_kind="$1"',
    '  row_name="$2"',
    '  row_path="$3"',
    '  row_source="$4"',
    '  row_active="$5"',
    '  row_size=$(file_size "$row_path")',
    '  row_mtime=$(file_mtime "$row_path")',
    '  row_source_b64=$(b64_encode "$row_source")',
    '  printf "%s\\t%s\\t%s\\t%s\\t%s\\t%s\\t%s\\n" "$row_kind" "$row_name" "$row_path" "$row_size" "$row_mtime" "$row_source_b64" "$row_active"',
    "}",
    'mkdir -p "$config_dir" "$profile_dir" "$source_dir" || exit 1',
  ];
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
  const encodedPath = toBase64Utf8(path);
  const result = await runRootShellScript([
    ...buildResolveLogPathScript(encodedPath),
    "if command -v cat >/dev/null 2>&1; then",
    '  cat "$file"',
    "elif command -v busybox >/dev/null 2>&1; then",
    '  busybox cat "$file"',
    "else",
    '  printf "%s\\n" "Error: cat command not found" >&2',
    "  exit 1",
    "fi",
  ]);

  if (result.errno !== 0) {
    throw buildExecError("Read log file failed", result);
  }

  return result.stdout;
}

export async function openLogFileLocation(path: string): Promise<CommandResult> {
  const encodedPath = toBase64Utf8(path);
  const result = await runRootShellScript([
    ...buildResolveLogPathScript(encodedPath),
    "share_dir=/sdcard/Download/MonoBox/logs",
    "share_file=$share_dir/$log_name",
    'mkdir -p "$share_dir" 2>/dev/null || { printf "%s\\n" "Error: failed to create share directory" >&2; exit 1; }',
    "if command -v cp >/dev/null 2>&1; then",
    '  cp -f "$file" "$share_file" || exit 1',
    "elif command -v busybox >/dev/null 2>&1; then",
    '  busybox cp -f "$file" "$share_file" || exit 1',
    "else",
    '  printf "%s\\n" "Error: cp command not found" >&2',
    "  exit 1",
    "fi",
    'chmod 0644 "$share_file" 2>/dev/null || true',
    'dir_uri="content://com.android.externalstorage.documents/document/primary%3ADownload%2FMonoBox%2Flogs"',
    'file_uri="content://com.android.externalstorage.documents/document/primary%3ADownload%2FMonoBox%2Flogs%2F$log_name"',
    'if am start -a android.intent.action.VIEW -d "$dir_uri" -t "vnd.android.document/directory" >/dev/null 2>&1; then',
    '  printf "opened\\t%s\\n" "$share_file"',
    "  exit 0",
    "fi",
    'am start -a android.intent.action.VIEW -d "$file_uri" -t text/plain --grant-read-uri-permission',
  ]);

  if (result.errno !== 0) {
    throw buildExecError("Open log file location failed", result);
  }

  return result;
}

export async function listMihomoConfigFiles(): Promise<MihomoConfigFile[]> {
  const result = await runRootShellScript([
    ...buildMihomoConfigShellHelpers(),
    'active_name=""',
    '[ -f "$active_file" ] && active_name=$(cat "$active_file" 2>/dev/null)',
    '[ -n "$active_name" ] && [ ! -f "$profile_dir/$active_name" ] && active_name=""',
    'current_config="$config_dir/config.yaml"',
    'if [ -f "$current_config" ]; then',
    '  current_active="false"',
    '  [ -z "$active_name" ] && current_active="true"',
    '  print_config_row "current" "config.yaml" "$current_config" "" "$current_active"',
    "fi",
    'find "$profile_dir" -type f 2>/dev/null | sort | while IFS= read -r profile; do',
    '  profile_name="${profile##*/}"',
    '  case "$profile_name" in *.yaml|*.yml) ;; *) continue ;; esac',
    '  profile_source=""',
    '  [ -f "$source_dir/$profile_name.url" ] && profile_source=$(cat "$source_dir/$profile_name.url" 2>/dev/null)',
    '  profile_active="false"',
    '  [ "$profile_name" = "$active_name" ] && profile_active="true"',
    '  print_config_row "profile" "$profile_name" "$profile" "$profile_source" "$profile_active"',
    "done",
  ]);

  if (result.errno !== 0) {
    throw buildExecError("List Mihomo configs failed", result);
  }

  return parseMihomoConfigRows(result.stdout);
}

export async function importMihomoConfigFile(name: string, content: string): Promise<MihomoConfigFile> {
  const encodedName = toBase64Utf8(name);
  const encodedContent = toBase64Utf8(content.replace(/\r\n/g, "\n"));
  const result = await runRootShellScript([
    `encoded_name=${encodedName}`,
    `encoded_content=${encodedContent}`,
    ...buildMihomoConfigShellHelpers(),
    'raw_name=$(b64_decode "$encoded_name") || exit 1',
    'profile_name=$(sanitize_profile_name "$raw_name")',
    'target="$profile_dir/$profile_name"',
    'tmp_file="$target.tmp.$$"',
    "if command -v base64 >/dev/null 2>&1; then",
    '  printf "%s" "$encoded_content" | base64 -d > "$tmp_file" 2>/dev/null || exit 1',
    "elif command -v busybox >/dev/null 2>&1; then",
    '  printf "%s" "$encoded_content" | busybox base64 -d > "$tmp_file" 2>/dev/null || exit 1',
    "else",
    '  printf "%s\\n" "Error: base64 command not found" >&2',
    "  exit 1",
    "fi",
    '[ -s "$tmp_file" ] || { rm -f "$tmp_file"; printf "%s\\n" "Error: config file is empty" >&2; exit 1; }',
    'mv -f "$tmp_file" "$target" || exit 1',
    'rm -f "$source_dir/$profile_name.url" 2>/dev/null || true',
    'active_name=""',
    '[ -f "$active_file" ] && active_name=$(cat "$active_file" 2>/dev/null)',
    'profile_active="false"',
    '[ "$profile_name" = "$active_name" ] && profile_active="true"',
    'print_config_row "profile" "$profile_name" "$target" "" "$profile_active"',
  ]);

  if (result.errno !== 0) {
    throw buildExecError("Import Mihomo config failed", result);
  }

  const [file] = parseMihomoConfigRows(result.stdout);
  if (!file) {
    throw new Error("Import Mihomo config failed: empty result");
  }
  return file;
}

export async function downloadMihomoConfigFromUrl(url: string, name: string): Promise<MihomoConfigFile> {
  const encodedUrl = toBase64Utf8(url.trim());
  const encodedName = toBase64Utf8(name.trim() || "remote-config.yaml");
  const result = await runRootShellScript([
    `encoded_url=${encodedUrl}`,
    `encoded_name=${encodedName}`,
    ...buildMihomoConfigShellHelpers(),
    'source_url=$(b64_decode "$encoded_url") || exit 1',
    'raw_name=$(b64_decode "$encoded_name") || exit 1',
    'case "$source_url" in http://*|https://*) ;; *) printf "%s\\n" "Error: only HTTP or HTTPS URLs are allowed" >&2; exit 1 ;; esac',
    'profile_name=$(sanitize_profile_name "$raw_name")',
    'target="$profile_dir/$profile_name"',
    'tmp_file="$target.tmp.$$"',
    'curl -fL --connect-timeout 20 --retry 2 --retry-delay 2 -o "$tmp_file" "$source_url" 2>&1 || { rm -f "$tmp_file"; exit 1; }',
    '[ -s "$tmp_file" ] || { rm -f "$tmp_file"; printf "%s\\n" "Error: downloaded config file is empty" >&2; exit 1; }',
    'mv -f "$tmp_file" "$target" || exit 1',
    'printf "%s" "$source_url" > "$source_dir/$profile_name.url" || exit 1',
    'active_name=""',
    '[ -f "$active_file" ] && active_name=$(cat "$active_file" 2>/dev/null)',
    'profile_active="false"',
    '[ "$profile_name" = "$active_name" ] && profile_active="true"',
    'print_config_row "profile" "$profile_name" "$target" "$source_url" "$profile_active"',
  ]);

  if (result.errno !== 0) {
    throw buildExecError("Download Mihomo config failed", result);
  }

  const [file] = parseMihomoConfigRows(result.stdout);
  if (!file) {
    throw new Error("Download Mihomo config failed: empty result");
  }
  return file;
}

export async function updateMihomoConfigFromUrl(name: string): Promise<MihomoConfigFile> {
  const encodedName = toBase64Utf8(name);
  const result = await runRootShellScript([
    `encoded_name=${encodedName}`,
    ...buildMihomoConfigShellHelpers(),
    'raw_name=$(b64_decode "$encoded_name") || exit 1',
    'profile_name=$(sanitize_profile_name "$raw_name")',
    'target="$profile_dir/$profile_name"',
    'source_file="$source_dir/$profile_name.url"',
    '[ -f "$target" ] || { printf "%s\\n" "Error: config profile not found" >&2; exit 1; }',
    '[ -s "$source_file" ] || { printf "%s\\n" "Error: config profile has no source URL" >&2; exit 1; }',
    'source_url=$(cat "$source_file" 2>/dev/null)',
    'case "$source_url" in http://*|https://*) ;; *) printf "%s\\n" "Error: stored source URL is invalid" >&2; exit 1 ;; esac',
    'tmp_file="$target.tmp.$$"',
    'curl -fL --connect-timeout 20 --retry 2 --retry-delay 2 -o "$tmp_file" "$source_url" 2>&1 || { rm -f "$tmp_file"; exit 1; }',
    '[ -s "$tmp_file" ] || { rm -f "$tmp_file"; printf "%s\\n" "Error: downloaded config file is empty" >&2; exit 1; }',
    'mv -f "$tmp_file" "$target" || exit 1',
    'active_name=""',
    '[ -f "$active_file" ] && active_name=$(cat "$active_file" 2>/dev/null)',
    'profile_active="false"',
    '[ "$profile_name" = "$active_name" ] && profile_active="true"',
    'print_config_row "profile" "$profile_name" "$target" "$source_url" "$profile_active"',
  ]);

  if (result.errno !== 0) {
    throw buildExecError("Update Mihomo config failed", result);
  }

  const [file] = parseMihomoConfigRows(result.stdout);
  if (!file) {
    throw new Error("Update Mihomo config failed: empty result");
  }
  return file;
}

export async function switchMihomoConfigFile(name: string, kind: MihomoConfigFileKind = "profile"): Promise<MihomoConfigFile> {
  const encodedName = toBase64Utf8(name);
  const encodedKind = toBase64Utf8(kind);
  const result = await runRootShellScript([
    `encoded_name=${encodedName}`,
    `encoded_kind=${encodedKind}`,
    ...buildMihomoConfigShellHelpers(),
    'raw_name=$(b64_decode "$encoded_name") || exit 1',
    'config_kind=$(b64_decode "$encoded_kind") || exit 1',
    'if [ "$config_kind" = "current" ]; then',
    '  target="$config_dir/config.yaml"',
    '  [ -f "$target" ] || { printf "%s\\n" "Error: default config not found" >&2; exit 1; }',
    '  if [ -x "$core_bin" ]; then',
    '    "$core_bin" -t -d "$config_dir" >&2 || { printf "%s\\n" "Error: config validation failed" >&2; exit 1; }',
    "  fi",
    '  rm -f "$active_file" || exit 1',
    '  print_config_row "current" "config.yaml" "$target" "" "true"',
    "  exit 0",
    "fi",
    'profile_name=$(sanitize_profile_name "$raw_name")',
    'target="$profile_dir/$profile_name"',
    '[ -f "$target" ] || { printf "%s\\n" "Error: config profile not found" >&2; exit 1; }',
    'if [ -x "$core_bin" ]; then',
    '  "$core_bin" -t -d "$config_dir" -f "$target" >&2 || { printf "%s\\n" "Error: config validation failed" >&2; exit 1; }',
    "fi",
    'printf "%s" "$profile_name" > "$active_file" || exit 1',
    'profile_source=""',
    '[ -f "$source_dir/$profile_name.url" ] && profile_source=$(cat "$source_dir/$profile_name.url" 2>/dev/null)',
    'print_config_row "profile" "$profile_name" "$target" "$profile_source" "true"',
  ]);

  if (result.errno !== 0) {
    throw buildExecError("Switch Mihomo config failed", result);
  }

  const [file] = parseMihomoConfigRows(result.stdout);
  if (!file) {
    throw new Error("Switch Mihomo config failed: empty result");
  }
  return file;
}

export async function deleteMihomoConfigFile(name: string): Promise<void> {
  const encodedName = toBase64Utf8(name);
  const result = await runRootShellScript([
    `encoded_name=${encodedName}`,
    ...buildMihomoConfigShellHelpers(),
    'raw_name=$(b64_decode "$encoded_name") || exit 1',
    'profile_name=$(sanitize_profile_name "$raw_name")',
    'target="$profile_dir/$profile_name"',
    '[ -f "$target" ] || { printf "%s\\n" "Error: config profile not found" >&2; exit 1; }',
    'active_name=""',
    '[ -f "$active_file" ] && active_name=$(cat "$active_file" 2>/dev/null)',
    '[ "$profile_name" != "$active_name" ] || { printf "%s\\n" "Error: active config cannot be deleted" >&2; exit 1; }',
    'rm -f "$target" "$source_dir/$profile_name.url" || exit 1',
  ]);

  if (result.errno !== 0) {
    throw buildExecError("Delete Mihomo config failed", result);
  }
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
