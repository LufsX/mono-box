import { readBoxConfigRaw, writeBoxConfigRaw } from "./action";

export interface ClashConfig {
  port: number;
  secret: string;
}

export interface MemoryData {
  inuse: number;
  oslimit: number;
}

export interface TrafficData {
  up: number;
  down: number;
  upTotal: number;
  downTotal: number;
}

export interface ClashProxyHistory {
  time: string;
  delay: number;
}

export interface ClashProxy {
  name: string;
  type: string;
  now?: string;
  all?: string[];
  history?: ClashProxyHistory[];
  udp?: boolean;
}

export type ClashProxyMap = Record<string, ClashProxy>;

export interface ClashProxyProvider {
  name: string;
  type: string;
  vehicleType: string;
  updatedAt: string;
  proxies: { name: string; type: string }[];
  subscriptionInfo?: {
    Download: number;
    Upload: number;
    Total: number;
    Expire: number;
  };
}

export type ClashProxyProviderMap = Record<string, ClashProxyProvider>;

export interface ClashRuleProvider {
  name: string;
  type?: string;
  vehicleType?: string;
  updatedAt?: string;
  // 规则集合常见字段，尽量宽松兼容不同内核返回
  rules?: unknown[];
  ruleCount?: number;
  [key: string]: unknown;
}

export type ClashRuleProviderMap = Record<string, ClashRuleProvider>;

export interface BoxConfigValues {
  clashApiPort: number;
  clashApiSecret: string;
  toggleAction: "service" | "tun" | "mode_cycle";
  toggleTunTarget: "toggle" | "on" | "off";
  toggleModeCycle: ("rule" | "global" | "direct")[];
}

export interface ClashVersionResponse {
  meta?: boolean;
  version?: string;
}

export type ClashVersionCheckResult =
  | {
      ok: true;
      version: string;
      meta: boolean;
    }
  | {
      ok: false;
      reason: "unauthorized" | "unreachable" | "invalid-response";
      message: string;
    };

let cachedConfig: ClashConfig | null = null;

function getErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

function formatExecFailure(action: string, result: { errno: number; stdout?: string; stderr?: string }): string {
  const details = [result.stderr, result.stdout]
    .filter((part) => typeof part === "string" && part.trim().length > 0)
    .join("\n")
    .trim();

  return details ? `${action} failed (errno=${result.errno}): ${details}` : `${action} failed (errno=${result.errno})`;
}

function parseBoxConfigContent(content: string): BoxConfigValues {
  const portMatch = content.match(/^clash_api_port=(\d+)\s*$/m);
  const secretMatch = content.match(/^clash_api_secret=(.*)$/m);
  const toggleActionMatch = content.match(/^toggle_action=(.*)$/m);
  const toggleTunTargetMatch = content.match(/^toggle_tun_target=(.*)$/m);
  const toggleModeCycleMatch = content.match(/^toggle_mode_cycle=(.*)$/m);

  const toggleActionRaw = toggleActionMatch ? toggleActionMatch[1].replace(/"/g, "").trim() : "service";
  const toggleTunTargetRaw = toggleTunTargetMatch ? toggleTunTargetMatch[1].replace(/"/g, "").trim() : "toggle";
  const toggleModeCycleRaw = toggleModeCycleMatch ? toggleModeCycleMatch[1].replace(/"/g, "").trim() : "rule,global,direct";

  const toggleAction = ["service", "tun", "mode_cycle"].includes(toggleActionRaw) ? (toggleActionRaw as "service" | "tun" | "mode_cycle") : "service";

  const toggleTunTarget = ["toggle", "on", "off"].includes(toggleTunTargetRaw) ? (toggleTunTargetRaw as "toggle" | "on" | "off") : "toggle";

  const modeSet = new Set<"rule" | "global" | "direct">();
  for (const mode of toggleModeCycleRaw.split(",")) {
    const normalized = mode.trim().toLowerCase();
    if (normalized === "rule" || normalized === "global" || normalized === "direct") {
      modeSet.add(normalized);
    }
  }
  const toggleModeCycle = [...modeSet];
  if (!toggleModeCycle.length) {
    toggleModeCycle.push("rule", "global", "direct");
  }

  return {
    clashApiPort: portMatch ? parseInt(portMatch[1], 10) : 9090,
    clashApiSecret: secretMatch ? secretMatch[1].trim() : "",
    toggleAction,
    toggleTunTarget,
    toggleModeCycle,
  };
}

function escapeRegExp(input: string): string {
  return input.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function upsertConfigValue(content: string, key: string, value: string): string {
  const line = `${key}=${value}`;
  const regex = new RegExp(`^${escapeRegExp(key)}=.*$`, "m");

  if (regex.test(content)) {
    return content.replace(regex, line);
  }

  const suffix = content.endsWith("\n") || content.length === 0 ? "" : "\n";
  return `${content}${suffix}${line}\n`;
}

export function parseBoxConfig(content: string): BoxConfigValues {
  return parseBoxConfigContent(content);
}

/**
 * 从 box.config 文件读取 Clash API 配置
 */
async function getClashConfig(): Promise<ClashConfig> {
  // 如果有缓存，返回缓存
  if (cachedConfig) {
    return cachedConfig;
  }

  // 从 box.config 读取
  try {
    const result = await readBoxConfigRaw();

    if (result.errno !== 0 || !result.stdout) {
      throw new Error("Failed to read config file");
    }

    const parsed = parseBoxConfigContent(result.stdout);
    cachedConfig = { port: parsed.clashApiPort, secret: parsed.clashApiSecret };
    return cachedConfig;
  } catch (e) {
    console.error("Failed to get clash config:", e);
    // 返回默认值
    return { port: 9090, secret: "" };
  }
}

/**
 * 发送请求到 Clash API
 */
async function clashRequest<T = any>(method: string, path: string, data?: any): Promise<T> {
  const config = await getClashConfig();
  const url = `http://127.0.0.1:${config.port}${path}`;

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };

  if (config.secret) {
    headers["Authorization"] = `Bearer ${config.secret}`;
  }

  const options: RequestInit = {
    method,
    headers,
  };

  if (data) {
    options.body = JSON.stringify(data);
  }

  const response = await fetch(url, options);

  // 如果响应为空，返回 null
  const text = await response.text();

  if (!response.ok) {
    // 尝试解析错误消息
    if (text) {
      try {
        const errorData = JSON.parse(text);
        if (errorData.message) {
          throw new Error(errorData.message);
        }
      } catch (e) {
        // 如果不是 JSON，使用原始文本
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

function buildRequestHeaders(secret?: string): Record<string, string> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };

  if (secret && secret.trim().length > 0) {
    headers.Authorization = `Bearer ${secret.trim()}`;
  }

  return headers;
}

export async function checkVersion(options?: { port?: number; secret?: string }): Promise<ClashVersionCheckResult> {
  const config = await getClashConfig();
  const port = options?.port ?? config.port;
  const secret = options?.secret ?? config.secret;
  const url = `http://127.0.0.1:${port}/version`;

  try {
    const response = await fetch(url, {
      method: "GET",
      headers: buildRequestHeaders(secret),
    });

    const text = await response.text();
    const maybeJson = text ? (JSON.parse(text) as ClashVersionResponse & { message?: string }) : null;

    if (!response.ok) {
      const message = maybeJson?.message || `HTTP ${response.status}`;
      if (response.status === 401 || message.toLowerCase() === "unauthorized") {
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

    if (!maybeJson?.version) {
      return {
        ok: false,
        reason: "invalid-response",
        message: "Clash API 返回格式异常",
      };
    }

    return {
      ok: true,
      version: maybeJson.version,
      meta: Boolean(maybeJson.meta),
    };
  } catch (error) {
    return {
      ok: false,
      reason: "unreachable",
      message: `无法连接 Clash API: ${getErrorMessage(error)}`,
    };
  }
}

/**
 * 获取 Clash 配置
 */
export async function getConfigs(): Promise<any> {
  return clashRequest("GET", "/configs");
}

/**
 * 切换代理模式
 */
export async function setMode(mode: string): Promise<void> {
  await clashRequest("PATCH", "/configs", { mode });
}

/**
 * 切换 TUN 模式
 */
export async function setTun(enable: boolean): Promise<void> {
  await clashRequest("PATCH", "/configs", { tun: { enable } });
}

/**
 * 升级核心
 */
export async function upgradeCore(): Promise<any> {
  return clashRequest("POST", "/upgrade", {});
}

/**
 * 创建内存监控 WebSocket 连接
 */
export async function createMemoryWebSocket(onMessage: (data: MemoryData) => void, onError?: (error: Event) => void): Promise<WebSocket> {
  const config = await getClashConfig();
  const token = config.secret || "1145141919810";
  const url = `ws://127.0.0.1:${config.port}/memory?token=${token}`;

  const ws = new WebSocket(url);

  ws.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data) as MemoryData;
      onMessage(data);
    } catch (e) {
      console.error("Failed to parse memory data:", e);
    }
  };

  ws.onerror = (error) => {
    console.error("Memory WebSocket error:", error);
    if (onError) onError(error);
  };

  return ws;
}

/**
 * 创建流量监控 WebSocket 连接
 */
export async function createTrafficWebSocket(onMessage: (data: TrafficData) => void, onError?: (error: Event) => void): Promise<WebSocket> {
  const config = await getClashConfig();
  const token = config.secret || "1145141919810";
  const url = `ws://127.0.0.1:${config.port}/traffic?token=${token}`;

  const ws = new WebSocket(url);

  ws.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data) as TrafficData;
      onMessage(data);
    } catch (e) {
      console.error("Failed to parse traffic data:", e);
    }
  };

  ws.onerror = (error) => {
    console.error("Traffic WebSocket error:", error);
    if (onError) onError(error);
  };

  return ws;
}

/**
 * 设置出站代理
 */
export async function setOutbound(selector: string, outbound: string): Promise<void> {
  await clashRequest("PUT", `/proxies/${selector}`, { name: outbound });
}

/**
 * 获取所有代理与策略组信息
 */
export async function getProxies(): Promise<ClashProxyMap> {
  const result = await clashRequest<{ proxies: ClashProxyMap }>("GET", "/proxies");
  return result?.proxies || {};
}

/**
 * 测试指定节点延迟
 */
export async function testProxyDelay(name: string, options?: { url?: string; timeout?: number }): Promise<number> {
  const url = options?.url || "http://cp.cloudflare.com/generate_204";
  const timeout = options?.timeout ?? 5000;
  const result = await clashRequest<{ delay?: number }>("GET", `/proxies/${encodeURIComponent(name)}/delay?url=${encodeURIComponent(url)}&timeout=${timeout}`);
  return typeof result?.delay === "number" ? result.delay : 0;
}

/**
 * 获取代理 Provider 列表
 */
export async function getProxyProviders(): Promise<ClashProxyProviderMap> {
  const result = await clashRequest<{ providers: ClashProxyProviderMap }>("GET", "/providers/proxies");
  return result?.providers || {};
}

/**
 * 获取规则 Provider（规则集合）列表
 */
export async function getRuleProviders(): Promise<ClashRuleProviderMap> {
  const result = await clashRequest<any>("GET", "/providers/rules");
  if (result && typeof result === "object") {
    if (result.providers && typeof result.providers === "object") {
      return result.providers as ClashRuleProviderMap;
    }
    return result as ClashRuleProviderMap;
  }
  return {};
}

/**
 * 更新指定规则 Provider（规则集合）
 */
export async function updateRuleProvider(name: string): Promise<void> {
  await clashRequest("PUT", `/providers/rules/${encodeURIComponent(name)}`);
}

/**
 * 更新指定 Provider
 */
export async function updateProxyProvider(name: string): Promise<void> {
  await clashRequest("PUT", `/providers/proxies/${encodeURIComponent(name)}`);
}

/**
 * 对 Provider 执行健康检查
 */
export async function healthCheckProxyProvider(name: string): Promise<void> {
  await clashRequest("GET", `/providers/proxies/${encodeURIComponent(name)}/healthcheck`);
}

/**
 * 获取代理信息
 */
export async function getProxy(name: string): Promise<any> {
  return clashRequest("GET", `/proxies/${name}`);
}

/**
 * 检查 Clash 服务状态
 */
export async function checkStatus(): Promise<boolean> {
  try {
    await getConfigs();
    return true;
  } catch {
    return false;
  }
}

/**
 * 清除缓存的配置（用于重新加载配置）
 */
export function clearConfigCache(): void {
  cachedConfig = null;
}

/**
 * 读取 box.config 文件内容
 */
export async function readBoxConfig(): Promise<string> {
  try {
    const result = await readBoxConfigRaw();

    if (result.errno !== 0) {
      throw new Error(formatExecFailure("Read box.config", result));
    }

    if (!result.stdout) {
      throw new Error("Read box.config failed: empty file content");
    }

    return result.stdout;
  } catch (e) {
    console.error("Failed to read box.config:", e);
    throw new Error(getErrorMessage(e));
  }
}

/**
 * 写入 box.config 文件内容
 */
export async function writeBoxConfig(content: string): Promise<void> {
  try {
    const result = await writeBoxConfigRaw(content);

    if (result.errno !== 0) {
      throw new Error(formatExecFailure("Write box.config", result));
    }

    // 清除缓存，强制重新读取
    clearConfigCache();
  } catch (e) {
    console.error("Failed to write box.config:", e);
    throw new Error(getErrorMessage(e));
  }
}

/**
 * 批量更新 box.config 中的配置项
 */
export async function updateBoxConfigValues(updates: Record<string, string>): Promise<void> {
  try {
    let content = await readBoxConfig();

    for (const [key, value] of Object.entries(updates)) {
      content = upsertConfigValue(content, key, value);
    }

    await writeBoxConfig(content);
  } catch (e) {
    console.error("Failed to update box.config values:", e);
    throw new Error(getErrorMessage(e));
  }
}

export interface ClashConnectionMetadata {
  network: string;
  type: string;
  sourceIP: string;
  destinationIP: string;
  sourcePort: string;
  destinationPort: string;
  host: string;
  dnsMode: string;
}

export interface ClashConnection {
  id: string;
  metadata: ClashConnectionMetadata;
  upload: number;
  download: number;
  start: string;
  chains: string[];
  rule: string;
  rulePayload: string;
}

export interface ClashConnectionsResponse {
  downloadTotal: number;
  uploadTotal: number;
  connections: ClashConnection[];
}

export async function getConnections(): Promise<ClashConnectionsResponse> {
  return await clashRequest("GET", "/connections");
}

export async function deleteConnection(id: string): Promise<void> {
  await clashRequest("DELETE", `/connections/${id}`);
}

export async function closeAllConnections(): Promise<void> {
  await clashRequest("DELETE", "/connections");
}

export interface ClashRule {
  type?: string;
  payload?: string;
  proxy?: string;
  disable?: boolean;
  disabled?: boolean;
  [key: string]: unknown;
}

export interface ClashRulesResponse {
  rules: (ClashRule | string)[];
}

export async function getRules(): Promise<ClashRulesResponse | (ClashRule | string)[]> {
  return await clashRequest("GET", "/rules");
}

export async function patchRulesDisable(payload: Record<string, boolean>): Promise<void> {
  await clashRequest("PATCH", "/rules/disable", payload);
}
