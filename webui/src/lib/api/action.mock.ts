import type { CommandResult, ModuleInfo } from "./action";

const MOCK_MODULE_PROP = [
  "id=mono_box",
  "name=Mono Box",
  "version=v6.1",
  "versionCode=2026033100",
  "author=LufsX, CHIZI-0618",
  "description=Use mihomo on Android devices.",
  "updateJson=https://cors.isteed.cc/https://github.com/LufsX/mono-box/releases/latest/download/update.json",
].join("\n");

let mockBoxConfig = "clash_api_port=9090\nclash_api_secret=\n";

function ok(stdout = "", stderr = ""): CommandResult {
  return {
    errno: 0,
    stdout,
    stderr,
  };
}

export async function runActionScript(actionCmd: string): Promise<CommandResult> {
  // 模拟脚本执行时间
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
  await openExternalUrlCommand(url);
}

export async function readModulePropRaw(): Promise<CommandResult> {
  return ok(MOCK_MODULE_PROP);
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

export async function readBoxConfigRaw(): Promise<CommandResult> {
  return ok(mockBoxConfig);
}

export async function writeBoxConfigRaw(content: string): Promise<CommandResult> {
  mockBoxConfig = content.replace(/\r\n/g, "\n");
  return ok("Mock write box.config success");
}
