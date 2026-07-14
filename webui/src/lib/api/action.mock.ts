import type { CommandResult, ModuleInfo } from "./clash.types";
import { parseKeyValueText } from "./config-parser";
import type { ConfigPort } from "./config-port";
import type { MihomoConfigFile } from "./action";

export interface LogFileInfo {
  path: string;
  size: number;
}

export interface LogSizeReport {
  files: LogFileInfo[];
  totalBytes: number;
  count: number;
}

interface MockMihomoProfile {
  name: string;
  content: string;
  sourceUrl: string;
  updatedAt: number;
}

const MOCK_MODULE_PROP = [
  "id=mono_box",
  "name=Mono Box",
  "version=v6.1",
  "versionCode=2026033100",
  "author=LufsX, CHIZI-0618",
  "description=Use mihomo on Android devices.",
  "updateJson=https://cors.isteed.cc/https://github.com/LufsX/mono-box/releases/latest/download/update.json",
].join("\n");

let mockLogFiles: LogFileInfo[] = [
  { path: "/data/adb/box/mihomo/mihomo_202606061015.log", size: 420 * 1024 },
  { path: "/data/adb/box/mihomo/mihomo_202606061030.log", size: 768 * 1024 },
];

let mockBoxConfig = [
  'bin_name="mihomo"',
  'CORE_USER_GROUP="root:net_admin"',
  "clash_api_port=9090",
  "clash_api_secret=",
  'toggle_action="service"',
  'toggle_tun_target="toggle"',
  'toggle_mode_cycle="rule,global,direct"',
  "ctr_mode=disable",
  'select_outbound=""',
  'target_cellular=""',
  'target_wifi=""',
  'target_wifi_list=""',
].join("\n");

let mockMihomoCurrentConfig = ["mixed-port: 7890", "allow-lan: false", "mode: rule", "log-level: info", "proxies: []", "proxy-groups: []", "rules:", "  - MATCH,DIRECT"].join("\n");

let mockMihomoProfiles: MockMihomoProfile[] = [
  {
    name: "daily.yaml",
    content: mockMihomoCurrentConfig,
    sourceUrl: "https://example.com/mihomo/daily.yaml",
    updatedAt: Date.now() - 1000 * 60 * 18,
  },
  {
    name: "direct-only.yaml",
    content: ["mixed-port: 7890", "mode: direct", "log-level: warning", "rules:", "  - MATCH,DIRECT"].join("\n"),
    sourceUrl: "",
    updatedAt: Date.now() - 1000 * 60 * 60 * 24,
  },
];

let mockActiveMihomoProfile: string | null = "daily.yaml";

function ok(stdout = "", stderr = ""): CommandResult {
  return {
    errno: 0,
    stdout,
    stderr,
  };
}

function latestMockLogPath(): string | undefined {
  const sortedFiles = [...mockLogFiles].sort((a, b) => a.path.localeCompare(b.path));
  return sortedFiles[sortedFiles.length - 1]?.path;
}

function buildMockLogContent(file: LogFileInfo, minLength: number): string {
  const parts: string[] = [];
  let index = 1;
  let length = 0;

  while (length < minLength) {
    const line = `[2026-06-06 10:${String(index % 60).padStart(2, "0")}:00][info] ${file.path} mock log entry ${index}`;
    parts.push(line);
    length += line.length + 1;
    if (index % 13 === 0) {
      const warningLine = `[2026-06-06 10:${String(index % 60).padStart(2, "0")}:01][warning] provider update took ${index * 3}ms`;
      parts.push(warningLine);
      length += warningLine.length + 1;
    }
    index += 1;
  }

  return parts.join("\n");
}

function logFileName(path: string): string {
  const parts = path.split("/").filter(Boolean);
  return parts[parts.length - 1] || "mono-box.log";
}

function sanitizeMihomoConfigName(value: string): string {
  const rawName = value.split(/[\\/]/).filter(Boolean).pop() || "config";
  let name = rawName.replace(/[^A-Za-z0-9._-]+/g, "_").replace(/^_+|_+$/g, "");
  if (!name || name === "." || name === "..") {
    name = "config";
  }
  if (!/\.(ya?ml)$/i.test(name)) {
    name = `${name}.yaml`;
  }
  return name;
}

function inferMihomoConfigNameFromUrl(url: string): string {
  try {
    const parsed = new URL(url);
    return sanitizeMihomoConfigName(parsed.pathname.split("/").filter(Boolean).pop() || "remote-config.yaml");
  } catch {
    return "remote-config.yaml";
  }
}

function upsertMockMihomoProfile(profile: MockMihomoProfile): MockMihomoProfile {
  const index = mockMihomoProfiles.findIndex((item) => item.name === profile.name);
  if (index === -1) {
    mockMihomoProfiles = [...mockMihomoProfiles, profile];
    return profile;
  }

  mockMihomoProfiles[index] = profile;
  return profile;
}

function toMihomoConfigFile(profile: MockMihomoProfile): MihomoConfigFile {
  return {
    kind: "profile",
    name: profile.name,
    path: `/data/adb/box/mihomo/.mono-box/profiles/${profile.name}`,
    size: new TextEncoder().encode(profile.content).length,
    updatedAt: profile.updatedAt,
    sourceUrl: profile.sourceUrl,
    active: profile.name === mockActiveMihomoProfile,
  };
}

function assertHttpUrl(url: string): void {
  if (!/^https?:\/\//i.test(url.trim())) {
    throw new Error("only HTTP or HTTPS URLs are allowed");
  }
}

export async function runActionScript(actionCmd: string): Promise<CommandResult> {
  await new Promise((resolve) => setTimeout(resolve, 1000));
  if (actionCmd === "logs_size") {
    const lines = mockLogFiles.map((file) => `file\t${file.size}\t${file.path}`);
    const totalBytes = mockLogFiles.reduce((sum, file) => sum + file.size, 0);
    lines.push(`total\t${totalBytes}\t${mockLogFiles.length}`);
    return ok(lines.join("\n"));
  }

  if (actionCmd === "clear_logs") {
    const activeLogPath = latestMockLogPath();
    let totalBytes = 0;
    let count = 0;
    let skipped = 0;
    const lines: string[] = [];

    mockLogFiles = mockLogFiles.filter((file) => {
      if (file.path === activeLogPath) {
        skipped += 1;
        lines.push(`file\tskipped\t${file.size}\t${file.path}`);
        return true;
      }

      totalBytes += file.size;
      count += 1;
      lines.push(`file\tcleared\t${file.size}\t${file.path}`);
      return false;
    });

    lines.push(`cleared\t${totalBytes}\t${count}\t0\t${skipped}`);
    return ok(lines.join("\n"));
  }

  return ok(`Mock execute: ${actionCmd}`);
}

export async function getLogSizeReport(): Promise<LogSizeReport> {
  await new Promise((resolve) => setTimeout(resolve, 400));
  const files = mockLogFiles.map((file) => ({ ...file }));
  return {
    files,
    totalBytes: files.reduce((sum, file) => sum + file.size, 0),
    count: files.length,
  };
}

export async function clearLogFiles(): Promise<CommandResult> {
  return runActionScript("clear_logs");
}

export async function readLogFile(path: string): Promise<string> {
  await new Promise((resolve) => setTimeout(resolve, 160));
  const file = mockLogFiles.find((item) => item.path === path);
  if (!file) {
    throw new Error("Read log file failed: file not found");
  }

  return buildMockLogContent(file, file.size).slice(0, file.size);
}

export async function openLogFileLocation(path: string): Promise<CommandResult> {
  await new Promise((resolve) => setTimeout(resolve, 180));
  const file = mockLogFiles.find((item) => item.path === path);
  if (!file) {
    throw new Error("Open log file location failed: file not found");
  }

  if (typeof document !== "undefined" && typeof URL !== "undefined") {
    const content = buildMockLogContent(file, file.size).slice(0, file.size);
    const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = logFileName(file.path);
    anchor.style.display = "none";
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 0);
  }

  return ok(`Mock downloaded: ${file.path}`);
}

export async function listMihomoConfigFiles(): Promise<MihomoConfigFile[]> {
  await new Promise((resolve) => setTimeout(resolve, 220));
  const current: MihomoConfigFile = {
    kind: "current",
    name: "config.yaml",
    path: "/data/adb/box/mihomo/config.yaml",
    size: new TextEncoder().encode(mockMihomoCurrentConfig).length,
    updatedAt: Date.now() - 1000 * 60 * 60,
    sourceUrl: "",
    active: mockActiveMihomoProfile === null,
  };

  return [current, ...mockMihomoProfiles.map(toMihomoConfigFile).sort((a, b) => a.name.localeCompare(b.name))];
}

export async function importMihomoConfigFile(name: string, content: string): Promise<MihomoConfigFile> {
  await new Promise((resolve) => setTimeout(resolve, 260));
  const normalizedContent = content.replace(/\r\n/g, "\n");
  if (!normalizedContent.trim()) {
    throw new Error("config file is empty");
  }

  const profile = upsertMockMihomoProfile({
    name: sanitizeMihomoConfigName(name),
    content: normalizedContent,
    sourceUrl: "",
    updatedAt: Date.now(),
  });

  if (profile.name === mockActiveMihomoProfile) {
    mockMihomoCurrentConfig = profile.content;
  }

  return toMihomoConfigFile(profile);
}

export async function downloadMihomoConfigFromUrl(url: string, name: string): Promise<MihomoConfigFile> {
  await new Promise((resolve) => setTimeout(resolve, 520));
  const sourceUrl = url.trim();
  assertHttpUrl(sourceUrl);

  const profileName = sanitizeMihomoConfigName(name.trim() || inferMihomoConfigNameFromUrl(sourceUrl));
  const profile = upsertMockMihomoProfile({
    name: profileName,
    content: ["mixed-port: 7890", "allow-lan: false", "mode: rule", "log-level: info", `# Mock downloaded from ${sourceUrl}`, "rules:", "  - MATCH,DIRECT"].join("\n"),
    sourceUrl,
    updatedAt: Date.now(),
  });

  if (profile.name === mockActiveMihomoProfile) {
    mockMihomoCurrentConfig = profile.content;
  }

  return toMihomoConfigFile(profile);
}

export async function updateMihomoConfigFromUrl(name: string): Promise<MihomoConfigFile> {
  await new Promise((resolve) => setTimeout(resolve, 480));
  const profileName = sanitizeMihomoConfigName(name);
  const profile = mockMihomoProfiles.find((item) => item.name === profileName);
  if (!profile) {
    throw new Error("config profile not found");
  }
  if (!profile.sourceUrl) {
    throw new Error("config profile has no source URL");
  }

  profile.content = `${profile.content}\n# Mock updated at ${new Date().toISOString()}`;
  profile.updatedAt = Date.now();

  if (profile.name === mockActiveMihomoProfile) {
    mockMihomoCurrentConfig = profile.content;
  }

  return toMihomoConfigFile(profile);
}

export async function switchMihomoConfigFile(name: string, kind: "current" | "profile" = "profile"): Promise<MihomoConfigFile> {
  await new Promise((resolve) => setTimeout(resolve, 260));
  if (kind === "current") {
    mockActiveMihomoProfile = null;
    return {
      kind: "current",
      name: "config.yaml",
      path: "/data/adb/box/mihomo/config.yaml",
      size: new TextEncoder().encode(mockMihomoCurrentConfig).length,
      updatedAt: Date.now(),
      sourceUrl: "",
      active: true,
    };
  }

  const profileName = sanitizeMihomoConfigName(name);
  const profile = mockMihomoProfiles.find((item) => item.name === profileName);
  if (!profile) {
    throw new Error("config profile not found");
  }

  mockActiveMihomoProfile = profile.name;
  mockMihomoCurrentConfig = profile.content;
  return toMihomoConfigFile(profile);
}

export async function deleteMihomoConfigFile(name: string): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 220));
  const profileName = sanitizeMihomoConfigName(name);
  const profile = mockMihomoProfiles.find((item) => item.name === profileName);
  if (!profile) {
    throw new Error("config profile not found");
  }
  if (profile.name === mockActiveMihomoProfile) {
    throw new Error("active config cannot be deleted");
  }

  mockMihomoProfiles = mockMihomoProfiles.filter((item) => item.name !== profileName);
}

export async function setEdgeToEdge(_enabled: boolean): Promise<void> {
  // no-op in mock/dev
}

export async function openExternalUrlCommand(url: string): Promise<CommandResult> {
  if (typeof window !== "undefined") {
    window.open(url, "_blank", "noopener,noreferrer");
  }
  return ok("Mock open url success");
}

export async function openExternalUrl(url: string): Promise<void> {
  const result = await openExternalUrlCommand(url);
  if (result.errno !== 0) {
    throw new Error("am start failed");
  }
}

export async function readModulePropRaw(): Promise<CommandResult> {
  return ok(MOCK_MODULE_PROP);
}

export async function getModuleInfo(): Promise<ModuleInfo> {
  const parsed = parseKeyValueText(MOCK_MODULE_PROP);
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

export async function readBoxConfig(): Promise<string> {
  return mockBoxConfig;
}

export async function writeBoxConfig(content: string): Promise<void> {
  mockBoxConfig = content.replace(/\r\n/g, "\n");
}

export const configPortAdapter: ConfigPort = {
  readBoxConfig,
  writeBoxConfig,
};
