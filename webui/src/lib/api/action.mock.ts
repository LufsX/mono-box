import type { CommandResult, ModuleInfo } from "./clash.types";
import { parseKeyValueText } from "./config-parser";
import type { ConfigPort } from "./config-port";

const MOCK_MODULE_PROP = [
  "id=mono_box",
  "name=Mono Box",
  "version=v6.1",
  "versionCode=2026033100",
  "author=LufsX, CHIZI-0618",
  "description=Use mihomo on Android devices.",
  "updateJson=https://cors.isteed.cc/https://github.com/LufsX/mono-box/releases/latest/download/update.json",
].join("\n");

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

function ok(stdout = "", stderr = ""): CommandResult {
  return {
    errno: 0,
    stdout,
    stderr,
  };
}

export async function runActionScript(actionCmd: string): Promise<CommandResult> {
  await new Promise((resolve) => setTimeout(resolve, 1000));
  return ok(`Mock execute: ${actionCmd}`);
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
