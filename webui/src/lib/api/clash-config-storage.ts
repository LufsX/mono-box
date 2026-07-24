import type { ClashConfig } from "./clash.types";

const STORAGE_KEY = "mono-box.clash-api";
const LEGACY_SECRET_KEYS = ["clash_api_secret", "clashApiSecret"];
const LEGACY_PORT_KEYS = ["clash_api_port", "clashApiPort"];

function normalizeClashConfig(input: unknown): ClashConfig | null {
  if (!input || typeof input !== "object") return null;
  const raw = input as Partial<Record<keyof ClashConfig, unknown>>;
  const port = typeof raw.port === "number" ? Math.trunc(raw.port) : Number(raw.port);
  if (!Number.isInteger(port) || port < 1 || port > 65535 || typeof raw.secret !== "string") return null;
  return { port, secret: raw.secret };
}

export function loadStoredClashConfig(): ClashConfig | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    const stored = raw ? normalizeClashConfig(JSON.parse(raw)) : null;
    const legacySecret = LEGACY_SECRET_KEYS.map((key) => localStorage.getItem(key)).find((value) => value?.trim())?.trim();
    if (!legacySecret) return stored;

    const legacyPortValue = LEGACY_PORT_KEYS.map((key) => localStorage.getItem(key)).find((value) => value?.trim());
    const legacyPort = legacyPortValue ? Number(legacyPortValue) : NaN;
    return {
      port: Number.isInteger(legacyPort) && legacyPort >= 1 && legacyPort <= 65535 ? legacyPort : stored?.port || 9090,
      secret: legacySecret,
    };
  } catch {
    return null;
  }
}

export function saveStoredClashConfig(config: ClashConfig): void {
  if (typeof window === "undefined") return;
  const normalized = normalizeClashConfig(config);
  if (!normalized) return;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(normalized));
    localStorage.setItem("clash_api_port", normalized.port.toString());
    localStorage.setItem("clash_api_secret", normalized.secret);
  } catch {
    // Fall back to box.config in restricted WebViews.
  }
}
