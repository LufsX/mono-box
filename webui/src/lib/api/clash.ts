import type { ConfigPort } from "./config-port";
import { runNativeCommand, toBase64Utf8 } from "./native";
import { loadStoredClashConfig, saveStoredClashConfig } from "./clash-config-storage";
import { parseBoxConfig } from "./config-parser";
import { getErrorMessage, classifyConnectionError } from "./error-utils";
import type {
  ClashApiPort,
  ClashConfig,
  MemoryData,
  TrafficData,
  ClashProxyMap,
  ClashProxyProviderMap,
  ClashRuleProviderMap,
  ClashVersionResponse,
  ClashVersionCheckResult,
  ClashConnectionsResponse,
  ClashRulesResponse,
  ClashConfigs,
  UpgradeResult,
  ClashProxyDetail,
  CoreStatusResult,
  ClashLogEntry,
  ClashLogLevel,
} from "./clash.types";

export type {
  ClashApiPort,
  ClashConfig,
  ClashLogEntry,
  ClashLogLevel,
  MemoryData,
  TrafficData,
  ClashProxyHistory,
  ClashProxyHealth,
  ClashProxy,
  ClashProxyMap,
  ClashProxyProvider,
  ClashProxyProviderMap,
  ClashRuleProvider,
  ClashRuleProviderMap,
  ControlMode,
  ProxyMode,
  BoxConfigValues,
  ClashVersionResponse,
  ClashVersionCheckResult,
  ClashConnectionMetadata,
  ClashConnection,
  ClashConnectionsResponse,
  ClashRule,
  ClashRulesResponse,
  ClashConfigs,
  UpgradeResult,
  ClashProxyDetail,
  CoreStatusResult,
  CommandResult,
  ModuleInfo,
} from "./clash.types";

const DEFAULT_WS_TOKEN = "1145141919810";
const REQUEST_TIMEOUT_MS = 15_000;
const UPGRADE_TIMEOUT_MS = 120_000;

class ClashApiError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
  }
}

function isAlreadyLatestMessage(message: unknown): message is string {
  return typeof message === "string" && /^update error: already using latest version [0-9A-Za-z][0-9A-Za-z.+_-]*$/.test(message);
}

export function createClashApi(configPort: ConfigPort): ClashApiPort {
  async function getClashConfig(): Promise<ClashConfig> {
    const stored = loadStoredClashConfig();
    if (stored) return stored;

    try {
      const content = await configPort.readBoxConfig();
      const parsed = parseBoxConfig(content);
      const config = { port: parsed.clashApiPort, secret: parsed.clashApiSecret };
      saveStoredClashConfig(config);
      return config;
    } catch (e) {
      console.error("Failed to get clash config:", e);
      return { port: 9090, secret: "" };
    }
  }

  function buildHeaders(secret?: string): Record<string, string> {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };
    if (secret?.trim()) headers.Authorization = `Bearer ${secret.trim()}`;
    return headers;
  }

  async function clashRequest<T>(method: string, path: string, data?: unknown, options?: { port?: number; secret?: string; timeoutMs?: number }): Promise<T> {
    const config = await getClashConfig();
    const port = options?.port ?? config.port;
    const secret = options?.secret ?? config.secret;
    const url = `http://127.0.0.1:${port}${path}`;

    const headers = buildHeaders(secret);
    const fetchOptions: RequestInit = { method, headers };

    if (data !== undefined) {
      fetchOptions.body = JSON.stringify(data);
    }

    const controller = new AbortController();
    fetchOptions.signal = controller.signal;
    const timeoutMs = options?.timeoutMs ?? REQUEST_TIMEOUT_MS;
    const timeoutError = new Error(`Clash API request timed out after ${timeoutMs} ms (${method} ${path})`);
    const timer = setTimeout(() => controller.abort(timeoutError), timeoutMs);

    try {
      const response = await fetch(url, fetchOptions);
      // Keep the same deadline while reading the response body.
      const text = await response.text();

      if (!response.ok) {
        let message = `Clash API error: ${response.status} ${response.statusText}`;
        if (text) {
          try {
            const errorData: unknown = JSON.parse(text);
            if (errorData && typeof errorData === "object" && "message" in errorData && typeof errorData.message === "string" && errorData.message) {
              message = errorData.message;
            }
          } catch {
            // Non-JSON errors still retain their HTTP status.
          }
        }
        throw new ClashApiError(message, response.status);
      }

      if (!text) {
        return null as T;
      }

      return JSON.parse(text) as T;
    } catch (error) {
      // Body reads can report AbortError instead of the supplied abort reason.
      if (controller.signal.aborted && (error === timeoutError || (error instanceof Error && error.name === "AbortError"))) {
        throw timeoutError;
      }
      throw error;
    } finally {
      clearTimeout(timer);
    }
  }

  async function checkVersion(options?: { port?: number; secret?: string }): Promise<ClashVersionCheckResult> {
    try {
      const data = await clashRequest<ClashVersionResponse & { message?: string }>("GET", "/version", undefined, options);

      if (!data?.version) {
        return {
          ok: false,
          reason: "invalid-response",
          message: "Clash API 返回格式异常",
        };
      }

      return {
        ok: true,
        version: data.version,
        meta: Boolean(data.meta),
      };
    } catch (error) {
      const message = getErrorMessage(error);
      if (message.toLowerCase().includes("unauthorized") || message.includes("401")) {
        return {
          ok: false,
          reason: "unauthorized",
          message: "Clash API 认证失败，请检查 Secret",
        };
      }

      return {
        ok: false,
        reason: "unreachable",
        message: `无法连接 Clash API: ${message}`,
      };
    }
  }

  async function getConfigs(): Promise<ClashConfigs> {
    return clashRequest<ClashConfigs>("GET", "/configs");
  }

  async function setMode(mode: string): Promise<void> {
    await clashRequest("PATCH", "/configs", { mode });
  }

  async function setTun(enable: boolean): Promise<void> {
    await clashRequest("PATCH", "/configs", { tun: { enable } });
  }

  async function upgradeCore(): Promise<UpgradeResult> {
    let result: { status?: unknown; message?: unknown } | null;
    try {
      result = await clashRequest<typeof result>("POST", "/upgrade", {}, { timeoutMs: UPGRADE_TIMEOUT_MS });
    } catch (error) {
      if (error instanceof ClashApiError && error.status === 500 && isAlreadyLatestMessage(error.message)) {
        return { status: "unchanged", message: error.message };
      }
      throw error;
    }

    if (isAlreadyLatestMessage(result?.message)) {
      return { status: "unchanged", message: result.message };
    }
    if (result !== null && (typeof result !== "object" || Array.isArray(result) || (result.status !== undefined && result.status !== "ok"))) {
      throw new Error(typeof result?.message === "string" ? result.message : "Clash API 返回格式异常");
    }
    return typeof result?.message === "string" ? { status: "ok", message: result.message } : { status: "ok" };
  }

  function createClashWebSocket(path: string, onMessage: (data: unknown) => void, onError?: (error: Event) => void): Promise<WebSocket> {
    return (async () => {
      const config = await getClashConfig();
      const token = encodeURIComponent(config.secret || DEFAULT_WS_TOKEN);
      const separator = path.includes("?") ? "&" : "?";
      const url = `ws://127.0.0.1:${config.port}${path}${separator}token=${token}`;

      const ws = new WebSocket(url);

      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data);
          onMessage(data);
        } catch (e) {
          console.error(`Failed to parse ${path} data:`, e);
        }
      };

      ws.onerror = (error) => {
        console.error(`${path} WebSocket error:`, error);
        if (onError) onError(error);
      };

      return ws;
    })();
  }

  async function createMemoryWebSocket(onMessage: (data: MemoryData) => void, onError?: (error: Event) => void): Promise<WebSocket> {
    return createClashWebSocket("/memory", onMessage as (data: unknown) => void, onError);
  }

  async function createTrafficWebSocket(onMessage: (data: TrafficData) => void, onError?: (error: Event) => void): Promise<WebSocket> {
    return createClashWebSocket("/traffic", onMessage as (data: unknown) => void, onError);
  }

  async function createLogsWebSocket(level: ClashLogLevel | undefined, onMessage: (data: ClashLogEntry) => void, onError?: (error: Event) => void): Promise<WebSocket> {
    const suffix = level ? `?level=${encodeURIComponent(level)}` : "";
    return createClashWebSocket(`/logs${suffix}`, onMessage as (data: unknown) => void, onError);
  }

  async function setOutbound(selector: string, outbound: string): Promise<void> {
    await clashRequest("PUT", `/proxies/${encodeURIComponent(selector)}`, { name: outbound });
  }

  async function getProxies(): Promise<ClashProxyMap> {
    const result = await clashRequest<{ proxies: ClashProxyMap }>("GET", "/proxies");
    return result?.proxies || {};
  }

  async function testProxyDelay(name: string, options?: { url?: string; timeout?: number; aliases?: string[]; providerName?: string }): Promise<number> {
    const config = await getClashConfig();
    const request = JSON.stringify({
      port: config.port,
      secret: config.secret.trim(),
      name,
      aliases: options?.aliases,
      providerName: options?.providerName,
      url: options?.url,
      timeout: options?.timeout ?? 5000,
    });
    const response = await runNativeCommand(["proxy-delay", toBase64Utf8(request)]);
    if (response.errno !== 0) {
      throw new Error(response.stderr.trim() || `Proxy delay command exited with code ${response.errno}`);
    }
    let result: { delay?: number };
    try {
      result = JSON.parse(response.stdout) as { delay?: number };
    } catch {
      throw new Error("Clash API 返回格式异常");
    }
    if (typeof result.delay !== "number" || !Number.isFinite(result.delay) || result.delay < 0) {
      throw new Error("Clash API 未返回有效延迟");
    }
    return result.delay;
  }

  async function getProxyProviders(): Promise<ClashProxyProviderMap> {
    const result = await clashRequest<{ providers: ClashProxyProviderMap }>("GET", "/providers/proxies");
    return result?.providers || {};
  }

  async function getRuleProviders(): Promise<ClashRuleProviderMap> {
    const result = await clashRequest<{ providers?: ClashRuleProviderMap } & ClashRuleProviderMap>("GET", "/providers/rules");
    if (result && typeof result === "object") {
      if (result.providers && typeof result.providers === "object") {
        return result.providers;
      }
      return result as ClashRuleProviderMap;
    }
    return {};
  }

  async function updateRuleProvider(name: string): Promise<void> {
    await clashRequest("PUT", `/providers/rules/${encodeURIComponent(name)}`);
  }

  async function updateProxyProvider(name: string): Promise<void> {
    await clashRequest("PUT", `/providers/proxies/${encodeURIComponent(name)}`);
  }

  async function healthCheckProxyProvider(name: string): Promise<void> {
    await clashRequest("GET", `/providers/proxies/${encodeURIComponent(name)}/healthcheck`);
  }

  async function getProxy(name: string): Promise<ClashProxyDetail> {
    return clashRequest<ClashProxyDetail>("GET", `/proxies/${encodeURIComponent(name)}`);
  }

  async function checkStatus(): Promise<CoreStatusResult> {
    try {
      await getConfigs();
      return { ok: true };
    } catch (e) {
      return classifyConnectionError(e);
    }
  }

  async function getConnections(): Promise<ClashConnectionsResponse> {
    return clashRequest<ClashConnectionsResponse>("GET", "/connections");
  }

  async function deleteConnection(id: string): Promise<void> {
    await clashRequest("DELETE", `/connections/${id}`);
  }

  async function closeAllConnections(): Promise<void> {
    await clashRequest("DELETE", "/connections");
  }

  async function getRules(): Promise<ClashRulesResponse> {
    return clashRequest<ClashRulesResponse>("GET", "/rules");
  }

  async function patchRulesDisable(payload: Record<string, boolean>): Promise<void> {
    await clashRequest("PATCH", "/rules/disable", payload);
  }

  return {
    checkVersion,
    getConfigs,
    setMode,
    setTun,
    upgradeCore,
    createMemoryWebSocket,
    createTrafficWebSocket,
    createLogsWebSocket,
    setOutbound,
    getProxies,
    testProxyDelay,
    getProxyProviders,
    getRuleProviders,
    updateRuleProvider,
    updateProxyProvider,
    healthCheckProxyProvider,
    getProxy,
    checkStatus,
    getConnections,
    deleteConnection,
    closeAllConnections,
    getRules,
    patchRulesDisable,
  };
}
