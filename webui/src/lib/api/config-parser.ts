import type { BoxConfigValues, ControlMode, ProxyMode } from "./clash.types";

export function escapeRegExp(input: string): string {
  return input.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function findShellCommentIndex(input: string): number {
  let quote: '"' | "'" | null = null;
  let escaped = false;
  const shellWordBreakers = ";&()<>|";

  for (let i = 0; i < input.length; i += 1) {
    const char = input[i];

    if (escaped) {
      escaped = false;
      continue;
    }

    if (char === "\\" && quote !== "'") {
      escaped = true;
      continue;
    }

    if (quote) {
      if (char === quote) {
        quote = null;
      }
      continue;
    }

    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }

    if (char === "#" && (i === 0 || /\s/.test(input[i - 1]) || shellWordBreakers.includes(input[i - 1]))) {
      return i;
    }
  }

  return -1;
}

function splitConfigValueComment(value: string): { valuePart: string; commentPart: string } {
  const commentIndex = findShellCommentIndex(value);
  if (commentIndex === -1) {
    return { valuePart: value, commentPart: "" };
  }

  return {
    valuePart: value.slice(0, commentIndex),
    commentPart: value.slice(commentIndex),
  };
}

export function stripConfigValue(value: string): string {
  const trimmed = splitConfigValueComment(value).valuePart.trim();
  let result = "";
  let quote: '"' | "'" | null = null;
  for (let i = 0; i < trimmed.length; i += 1) {
    const char = trimmed[i];
    if (quote === "'") {
      if (char === "'") quote = null;
      else result += char;
    } else if (char === "\\" && i + 1 < trimmed.length) {
      const next = trimmed[i + 1];
      if (quote === '"' && !['$', '`', '"', "\\"].includes(next)) result += char;
      else result += trimmed[++i];
    } else if (quote === '"') {
      if (char === '"') quote = null;
      else result += char;
    } else if (char === '"' || char === "'") {
      quote = char;
    } else {
      result += char;
    }
  }
  return result;
}

export function readConfigValue(content: string, key: string, fallback = ""): string {
  const regex = new RegExp(`^\\s*${escapeRegExp(key)}=(.*)$`);
  let value = fallback;

  for (const line of content.split(/\r?\n/)) {
    if (line.trimStart().startsWith("#")) {
      continue;
    }

    const match = line.match(regex);
    if (match) {
      value = stripConfigValue(match[1]);
    }
  }

  return value;
}

export function normalizeControlMode(value: string): ControlMode {
  return value === "switch" || value === "tun" || value === "selector" || value === "mode" ? value : "disable";
}

export function normalizeProxyMode(value: string): ProxyMode | null {
  const mode = value.trim().toLowerCase();
  return mode === "rule" || mode === "global" || mode === "direct" ? mode : null;
}

export function upsertConfigValue(content: string, key: string, value: string): string {
  const regex = new RegExp(`^(\\s*${escapeRegExp(key)}=)(.*)$`);
  const lines = content.split("\n");
  let targetIndex = -1;
  let targetMatch: RegExpMatchArray | null = null;

  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i];
    if (line.trimStart().startsWith("#")) {
      continue;
    }

    const match = line.match(regex);
    if (!match) {
      continue;
    }

    targetIndex = i;
    targetMatch = match;
  }

  if (targetIndex !== -1 && targetMatch) {
    const { valuePart, commentPart } = splitConfigValueComment(targetMatch[2]);
    const commentGap = commentPart ? valuePart.match(/\s*$/)?.[0] || " " : "";
    lines[targetIndex] = `${targetMatch[1]}${value}${commentGap}${commentPart}`;
    return lines.join("\n");
  }

  const suffix = content.endsWith("\n") || content.length === 0 ? "" : "\n";
  const line = `${key}=${value}`;
  return `${content}${suffix}${line}\n`;
}

export function parseBoxConfig(content: string): BoxConfigValues {
  const portValue = readConfigValue(content, "clash_api_port", "9090");
  const toggleActionRaw = readConfigValue(content, "toggle_action", "service");
  const toggleTunTargetRaw = readConfigValue(content, "toggle_tun_target", "toggle");
  const toggleModeCycleRaw = readConfigValue(content, "toggle_mode_cycle", "rule,global,direct");

  const toggleAction = ["service", "tun", "mode_cycle"].includes(toggleActionRaw) ? (toggleActionRaw as "service" | "tun" | "mode_cycle") : "service";

  const toggleTunTarget = ["toggle", "on", "off"].includes(toggleTunTargetRaw) ? (toggleTunTargetRaw as "toggle" | "on" | "off") : "toggle";

  const modeSet = new Set<ProxyMode>();
  for (const mode of toggleModeCycleRaw.split(",")) {
    const normalized = normalizeProxyMode(mode);
    if (normalized) {
      modeSet.add(normalized);
    }
  }
  const toggleModeCycle = [...modeSet];
  if (!toggleModeCycle.length) {
    toggleModeCycle.push("rule", "global", "direct");
  }
  const controlMode = normalizeControlMode(readConfigValue(content, "ctr_mode", "disable"));

  return {
    clashApiPort: /^\d+$/.test(portValue) ? parseInt(portValue, 10) : 9090,
    clashApiSecret: readConfigValue(content, "clash_api_secret"),
    toggleAction,
    toggleTunTarget,
    toggleModeCycle,
    controlMode,
    selectOutbound: readConfigValue(content, "select_outbound"),
    targetCellular: readConfigValue(content, "target_cellular"),
    targetWifi: readConfigValue(content, "target_wifi"),
    targetWifiList: readConfigValue(content, "target_wifi_list"),
    autoRecovery: readConfigValue(content, "auto_recovery", "false") === "true",
    autoUpdateConfig: readConfigValue(content, "auto_update_config", "false") === "true",
    configUpdateIntervalHours: Number(readConfigValue(content, "config_update_interval_hours", "24")),
    configUpdateWifiOnly: readConfigValue(content, "config_update_wifi_only", "true") === "true",
  };
}

export function parseKeyValueText(content: string): Record<string, string> {
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
