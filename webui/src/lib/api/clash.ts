import type { ConfigPort } from "./config-port";
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

export function createClashApi(configPort: ConfigPort): ClashApiPort {
  async function getClashConfig(): Promise<ClashConfig> {
    try {
      const content = await configPort.readBoxConfig();
      const parsed = parseBoxConfig(content);
      return { port: parsed.clashApiPort, secret: parsed.clashApiSecret };
    } catch (e) {
      console.error("Failed to get clash config:", e);
      return { port: 9090, secret: "" };
    }
  }

  function buildHeaders(secret?: string): Record<string, string> {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };

    if (secret && secret.trim().length > 0) {
      headers.Authorization = `Bearer ${secret.trim()}`;
    }

    return headers;
  }

  async function clashRequest<T>(method: string, path: string, data?: unknown, options?: { port?: number; secret?: string }): Promise<T> {
    const config = await getClashConfig();
    const port = options?.port ?? config.port;
    const secret = options?.secret ?? config.secret;
    const url = `http://127.0.0.1:${port}${path}`;

    const headers = buildHeaders(secret);
    const fetchOptions: RequestInit = { method, headers };

    if (data !== undefined) {
      fetchOptions.body = JSON.stringify(data);
    }

    const response = await fetch(url, fetchOptions);
    const text = await response.text();

    if (!response.ok) {
      if (text) {
        try {
          const errorData = JSON.parse(text) as { message?: string };
          if (errorData.message) {
            throw new Error(errorData.message);
          }
        } catch (e) {
          if (e instanceof Error && e.message !== text) {
            throw e;
          }
        }
      }
      throw new Error(`Clash API error: ${response.status} ${response.statusText}`);
    }

    if (!text) {
      return null as T;
    }

    return JSON.parse(text) as T;
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
    return clashRequest<UpgradeResult>("POST", "/upgrade", {});
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
    await clashRequest("PUT", `/proxies/${selector}`, { name: outbound });
  }

  async function getProxies(): Promise<ClashProxyMap> {
    const result = await clashRequest<{ proxies: ClashProxyMap }>("GET", "/proxies");
    return result?.proxies || {};
  }

  async function testProxyDelay(name: string, options?: { url?: string; timeout?: number }): Promise<number> {
    const url = options?.url || "http://cp.cloudflare.com/generate_204";
    const timeout = options?.timeout ?? 5000;
    const result = await clashRequest<{ delay?: number }>("GET", `/proxies/${encodeURIComponent(name)}/delay?url=${encodeURIComponent(url)}&timeout=${timeout}`);
    return typeof result?.delay === "number" ? result.delay : 0;
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
    return clashRequest<ClashProxyDetail>("GET", `/proxies/${name}`);
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
