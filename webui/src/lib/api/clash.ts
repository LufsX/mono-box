import { exec } from "kernelsu";

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

let cachedConfig: ClashConfig | null = null;

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
    const result = await exec("su -c 'cat /data/adb/box/scripts/box.config'", {
      cwd: "/data/adb/box"
    });

    if (result.errno !== 0 || !result.stdout) {
      throw new Error("Failed to read config file");
    }

    const content = result.stdout;
    
    // 解析 clash_api_port
    const portMatch = content.match(/clash_api_port=(\d+)/);
    const port = portMatch ? parseInt(portMatch[1], 10) : 9090;

    // 解析 clash_api_secret
    const secretMatch = content.match(/clash_api_secret=(.+)/);
    const secret = secretMatch ? secretMatch[1].trim() : "";

    cachedConfig = { port, secret };
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
async function clashRequest<T = any>(
  method: string,
  path: string,
  data?: any
): Promise<T> {
  const config = await getClashConfig();
  const url = `http://127.0.0.1:${config.port}${path}`;

  const headers: Record<string, string> = {
    "Content-Type": "application/json"
  };

  if (config.secret) {
    headers["Authorization"] = `Bearer ${config.secret}`;
  }

  const options: RequestInit = {
    method,
    headers
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
export async function createMemoryWebSocket(
  onMessage: (data: MemoryData) => void,
  onError?: (error: Event) => void
): Promise<WebSocket> {
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
export async function createTrafficWebSocket(
  onMessage: (data: TrafficData) => void,
  onError?: (error: Event) => void
): Promise<WebSocket> {
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
  } catch (e) {
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
    const result = await exec("su -c 'cat /data/adb/box/scripts/box.config'", {
      cwd: "/data/adb/box"
    });

    if (result.errno !== 0 || !result.stdout) {
      throw new Error("Failed to read box.config file");
    }

    return result.stdout;
  } catch (e) {
    console.error("Failed to read box.config:", e);
    throw e;
  }
}

/**
 * 写入 box.config 文件内容
 */
export async function writeBoxConfig(content: string): Promise<void> {
  try {
    // 转义单引号和特殊字符
    const escapedContent = content.replace(/'/g, "'\\''");
    
    const result = await exec(
      `su -c 'echo '${escapedContent}' > /data/adb/box/scripts/box.config'`,
      { cwd: "/data/adb/box" }
    );

    if (result.errno !== 0) {
      throw new Error("Failed to write box.config file");
    }

    // 清除缓存，强制重新读取
    clearConfigCache();
  } catch (e) {
    console.error("Failed to write box.config:", e);
    throw e;
  }
}

/**
 * 更新 box.config 中的特定配置项
 */
export async function updateBoxConfigValue(key: string, value: string): Promise<void> {
  try {
    const content = await readBoxConfig();
    
    // 使用正则表达式替换配置项
    const regex = new RegExp(`^(${key}=).*$`, 'm');
    const newContent = content.replace(regex, `$1${value}`);
    
    await writeBoxConfig(newContent);
  } catch (e) {
    console.error(`Failed to update ${key}:`, e);
    throw e;
  }
}
