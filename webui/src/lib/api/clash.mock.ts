import { parseBoxConfig } from "./config-parser";
import type { ConfigPort } from "./config-port";
import type {
  ClashApiPort,
  ClashProxyMap,
  ClashProxyProviderMap,
  ClashRuleProviderMap,
  ClashVersionCheckResult,
  ClashProxyHistory,
  ClashConnectionsResponse,
  ClashConnection,
  ClashRulesResponse,
  ClashConfigs,
  UpgradeResult,
  ClashProxyDetail,
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
} from "./clash.types";

function randomInRange(min: number, max: number): number {
  return Math.floor(Math.random() * (max - min + 1)) + min;
}

class MockSocket {
  onclose: ((this: WebSocket, ev: CloseEvent) => any) | null = null;
  onerror: ((this: WebSocket, ev: Event) => any) | null = null;
  onmessage: ((this: WebSocket, ev: MessageEvent<any>) => any) | null = null;
  onopen: ((this: WebSocket, ev: Event) => any) | null = null;

  readyState = 1;

  private timer: ReturnType<typeof setInterval> | null = null;
  private closed = false;
  private producer: () => unknown;

  constructor(producer: () => unknown, intervalMs: number) {
    this.producer = producer;
    this.timer = setInterval(() => {
      if (this.closed) {
        return;
      }

      const payload = this.producer();
      this.onmessage?.call(this as unknown as WebSocket, new MessageEvent("message", { data: JSON.stringify(payload) }));
    }, intervalMs);
  }

  close(code?: number, reason?: string): void {
    if (this.closed) {
      return;
    }

    this.closed = true;
    this.readyState = 3;
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = null;
    }

    this.onclose?.call(
      this as unknown as WebSocket,
      new CloseEvent("close", {
        code: typeof code === "number" ? code : 1000,
        reason: reason || "mock closed",
        wasClean: true,
      }),
    );
  }
}

export function createMockClashApi(configPort: ConfigPort): ClashApiPort {
  let mockMode = "rule";
  let mockTunEnabled = false;

  const mockNodeNames = ["HK-A", "JP-A", "US-A", "SG-A", "DIRECT", "REJECT"];

  let mockProxies: ClashProxyMap = {
    GLOBAL: {
      name: "GLOBAL",
      type: "Selector",
      now: "Proxy",
      all: ["Proxy", "DIRECT"],
      udp: true,
    },
    Proxy: {
      name: "Proxy",
      type: "Selector",
      now: "HK-A",
      all: ["HK-A", "JP-A", "US-A", "SG-A", "DIRECT", "REJECT"],
      udp: true,
    },
    Auto: {
      name: "Auto",
      type: "URLTest",
      now: "JP-A",
      all: ["HK-A", "JP-A", "US-A", "SG-A"],
      udp: true,
    },
    Smart: {
      name: "Smart",
      type: "Smart",
      now: "Smart - Select",
      all: ["HK-A", "JP-A", "US-A", "SG-A"],
      collectData: false,
      "policy-priority": "",
      preferASN: false,
      sampleRate: 1,
      useLightGBM: true,
      udp: true,
    },
  };

  let mockProviders: ClashProxyProviderMap = {
    default: {
      name: "default",
      type: "Proxy",
      vehicleType: "Compatible",
      updatedAt: "0001-01-01T00:00:00Z",
      testUrl: "http://cp.cloudflare.com/generate_204",
      expectedStatus: "*",
      proxies: [
        {
          name: "DIRECT",
          type: "Direct",
          alive: true,
          history: [{ time: new Date().toISOString(), delay: 24 }],
          extra: {
            "http://cp.cloudflare.com/generate_204": {
              alive: true,
              history: [{ time: new Date().toISOString(), delay: 24 }],
            },
          },
          udp: true,
        },
        { name: "HK-A", type: "Shadowsocks", alive: true, history: [], extra: {}, udp: true },
        { name: "JP-A", type: "Shadowsocks", alive: true, history: [], extra: {}, udp: true },
      ],
    },
    "Provider-A": {
      name: "Provider-A",
      type: "Proxy",
      vehicleType: "HTTP",
      updatedAt: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
      testUrl: "http://cp.cloudflare.com/generate_204",
      expectedStatus: "*",
      proxies: [
        { name: "HK-A", type: "Shadowsocks", alive: true, history: [], extra: {} },
        { name: "JP-A", type: "Shadowsocks", alive: true, history: [], extra: {} },
        { name: "US-A", type: "Shadowsocks", alive: true, history: [], extra: {} },
        { name: "SG-A", type: "Shadowsocks", alive: true, history: [], extra: {} },
      ],
      subscriptionInfo: {
        Download: 32 * 1024 * 1024 * 1024,
        Upload: 12 * 1024 * 1024 * 1024,
        Total: 200 * 1024 * 1024 * 1024,
        Expire: Math.floor((Date.now() + 15 * 24 * 60 * 60 * 1000) / 1000),
      },
    },
  };

  let mockRuleProviders: ClashRuleProviderMap = {
    "Rules-A": {
      name: "Rules-A",
      type: "Rule",
      vehicleType: "HTTP",
      updatedAt: new Date(Date.now() - 90 * 60 * 1000).toISOString(),
      ruleCount: 128,
    },
    "Rules-B": {
      name: "Rules-B",
      type: "Rule",
      vehicleType: "File",
      updatedAt: new Date(Date.now() - 4 * 60 * 60 * 1000).toISOString(),
      ruleCount: 42,
    },
  };

  for (const node of mockNodeNames) {
    if (!mockProxies[node]) {
      mockProxies[node] = {
        name: node,
        type: node === "DIRECT" || node === "REJECT" ? "Direct" : "Shadowsocks",
        udp: true,
        history: [],
      };
    }
  }

  let mockConnections: ClashConnection[] = [
    {
      id: "1",
      metadata: {
        network: "tcp",
        type: "HTTP",
        sourceIP: "192.168.1.100",
        destinationIP: "1.1.1.1",
        sourcePort: "51234",
        destinationPort: "443",
        host: "example.com",
        dnsMode: "normal",
      },
      upload: 1024,
      download: 2048,
      start: new Date(Date.now() - 10000).toISOString(),
      chains: ["Proxy", "HK-A"],
      rule: "Match",
      rulePayload: "",
    },
    {
      id: "2",
      metadata: {
        network: "udp",
        type: "DNS",
        sourceIP: "192.168.1.100",
        destinationIP: "8.8.8.8",
        sourcePort: "51235",
        destinationPort: "53",
        host: "",
        dnsMode: "normal",
      },
      upload: 512,
      download: 512,
      start: new Date(Date.now() - 5000).toISOString(),
      chains: ["GLOBAL", "DIRECT"],
      rule: "Match",
      rulePayload: "",
    },
  ];

  const mockRulesBase: Array<Omit<import("./clash.types").ClashRule, "disable" | "disabled">> = [
    { type: "DOMAIN-SUFFIX", payload: "google.com", proxy: "Proxy" },
    { type: "DOMAIN-KEYWORD", payload: "github", proxy: "Auto" },
    { type: "IP-CIDR", payload: "10.0.0.0/8", proxy: "DIRECT" },
    { type: "MATCH", payload: "", proxy: "Proxy" },
  ];

  const mockDisabledRuleIndexes = new Set<number>();
  const mockLogSamples: ClashLogEntry[] = [
    {
      type: "info",
      payload: "[TCP] 198.18.0.1:55231(com.android.chrome, uid=10142) --> github.com:443 match DomainSuffix(github.com) using Proxy[HK-A]",
    },
    {
      type: "info",
      payload: "[UDP] 198.18.0.1:44982(com.google.android.gms, uid=10120) --> 8.8.8.8:53 match GeoIP(CN) using DIRECT",
    },
    {
      type: "warning",
      payload: "[Provider] Provider-A health check timeout: JP-A",
    },
    {
      type: "debug",
      payload: "[DNS] cache hit: api.github.com --> 20.205.243.166",
    },
    {
      type: "error",
      payload: "[TCP] dial Proxy[US-A] error: connect timeout",
    },
  ];
  let mockLogIndex = 0;

  async function getConfigs(): Promise<ClashConfigs> {
    const content = await configPort.readBoxConfig();
    const parsed = parseBoxConfig(content);
    return {
      mode: mockMode,
      tun: { enable: mockTunEnabled },
      url: "https://cp.cloudflare.com/generate_204",
      port: 7890,
      "socks-port": 7891,
      "mixed-port": parsed.clashApiPort,
      "redir-port": 0,
      "tproxy-port": 0,
      "global-ua": "Mihomo Mock/1.0.0",
    };
  }

  async function setMode(mode: string): Promise<void> {
    mockMode = mode;
  }

  async function setTun(enable: boolean): Promise<void> {
    mockTunEnabled = enable;
  }

  async function upgradeCore(): Promise<UpgradeResult> {
    return {
      status: "ok",
      message: "Mock core upgrade completed",
    };
  }

  async function createMemoryWebSocket(onMessage: (data: import("./clash.types").MemoryData) => void, _onError?: (error: Event) => void): Promise<WebSocket> {
    const socket = new MockSocket(() => {
      const inuse = randomInRange(20, 100) * 1024 * 1024;
      return {
        inuse,
        oslimit: 2 * 1024 * 1024 * 1024,
      } satisfies import("./clash.types").MemoryData;
    }, 1200);

    socket.onmessage = (event) => {
      const parsed = JSON.parse(event.data) as import("./clash.types").MemoryData;
      onMessage(parsed);
    };

    return socket as unknown as WebSocket;
  }

  async function createTrafficWebSocket(onMessage: (data: import("./clash.types").TrafficData) => void, _onError?: (error: Event) => void): Promise<WebSocket> {
    let mockUploadTotal = 128 * 1024 * 1024;
    let mockDownloadTotal = 512 * 1024 * 1024;

    const socket = new MockSocket(() => {
      const up = randomInRange(20, 1800) * 1024;
      const down = randomInRange(80, 12000) * 1024;
      mockUploadTotal += up;
      mockDownloadTotal += down;

      return {
        up,
        down,
        upTotal: mockUploadTotal,
        downTotal: mockDownloadTotal,
      } satisfies import("./clash.types").TrafficData;
    }, 1000);

    socket.onmessage = (event) => {
      const parsed = JSON.parse(event.data) as import("./clash.types").TrafficData;
      onMessage(parsed);
    };

    return socket as unknown as WebSocket;
  }

  async function createLogsWebSocket(level: ClashLogLevel | undefined, onMessage: (data: ClashLogEntry) => void, _onError?: (error: Event) => void): Promise<WebSocket> {
    const socket = new MockSocket(() => {
      const pool = level ? mockLogSamples.filter((entry) => entry.type === level) : mockLogSamples;
      const source = pool.length > 0 ? pool : mockLogSamples;
      const entry = source[mockLogIndex % source.length];
      mockLogIndex += 1;
      return entry;
    }, 900);

    socket.onmessage = (event) => {
      const parsed = JSON.parse(event.data) as ClashLogEntry;
      onMessage(parsed);
    };

    return socket as unknown as WebSocket;
  }

  async function setOutbound(_selector: string, _outbound: string): Promise<void> {
    const selector = _selector.trim();
    if (!selector || !mockProxies[selector]) {
      throw new Error(`Selector not found: ${selector}`);
    }

    if (!mockProxies[selector].all?.includes(_outbound)) {
      throw new Error(`Outbound not found in group: ${_outbound}`);
    }

    mockProxies = {
      ...mockProxies,
      [selector]: {
        ...mockProxies[selector],
        now: _outbound,
      },
    };
  }

  async function getProxies(): Promise<ClashProxyMap> {
    return mockProxies;
  }

  async function testProxyDelay(name: string, options?: { url?: string; timeout?: number; aliases?: string[] }): Promise<number> {
    const candidates = [...new Set([name, ...(options?.aliases || [])])];
    const resolvedName =
      candidates.find((candidate) => Boolean(mockProxies[candidate])) ||
      candidates.find((candidate) => Object.values(mockProviders).some((provider) => (provider.proxies || []).some((proxy) => proxy.name === candidate)));
    if (!resolvedName) throw new Error("Resource not found");

    const delay = randomInRange(32, 880);
    const entry = { time: new Date().toISOString(), delay };
    const appendHistory = (history: ClashProxyHistory[] | undefined) => [...(history || []), entry].slice(-12);

    if (mockProxies[resolvedName]) {
      mockProxies = {
        ...mockProxies,
        [resolvedName]: {
          ...mockProxies[resolvedName],
          history: appendHistory(mockProxies[resolvedName].history),
        },
      };
    }

    mockProviders = Object.fromEntries(
      Object.entries(mockProviders).map(([providerName, provider]) => [
        providerName,
        {
          ...provider,
          proxies: (provider.proxies || []).map((proxy) =>
            proxy.name === resolvedName ? { ...proxy, history: appendHistory(proxy.history) } : proxy,
          ),
        },
      ]),
    );

    return delay;
  }

  async function getProxyProviders(): Promise<ClashProxyProviderMap> {
    return mockProviders;
  }

  async function getRuleProviders(): Promise<ClashRuleProviderMap> {
    return mockRuleProviders;
  }

  async function updateRuleProvider(name: string): Promise<void> {
    const provider = mockRuleProviders[name];
    if (!provider) {
      throw new Error(`Provider not found: ${name}`);
    }

    mockRuleProviders = {
      ...mockRuleProviders,
      [name]: {
        ...provider,
        updatedAt: new Date().toISOString(),
      },
    };
  }

  async function updateProxyProvider(name: string): Promise<void> {
    if (!mockProviders[name]) {
      throw new Error(`Provider not found: ${name}`);
    }

    mockProviders = {
      ...mockProviders,
      [name]: {
        ...mockProviders[name],
        updatedAt: new Date().toISOString(),
      },
    };
  }

  async function healthCheckProxyProvider(name: string): Promise<void> {
    const provider = mockProviders[name];
    if (!provider) {
      throw new Error(`Provider not found: ${name}`);
    }

    for (const node of provider.proxies || []) {
      await testProxyDelay(node.name);
    }

    const updatedProvider = mockProviders[name];
    mockProviders = {
      ...mockProviders,
      [name]: {
        ...updatedProvider,
        updatedAt: new Date().toISOString(),
      },
    };
  }

  async function getProxy(name: string): Promise<ClashProxyDetail> {
    return {
      name,
      type: "Selector",
      now: "DIRECT",
      all: ["DIRECT", "REJECT"],
    };
  }

  async function checkStatus(): Promise<import("./clash.types").CoreStatusResult> {
    return { ok: true };
  }

  async function checkVersion(options?: { port?: number; secret?: string }): Promise<ClashVersionCheckResult> {
    const content = await configPort.readBoxConfig();
    const parsed = parseBoxConfig(content);
    const incomingSecret = (options?.secret ?? parsed.clashApiSecret).trim();
    await new Promise((resolve) => setTimeout(resolve, 500));

    if (incomingSecret !== "" && incomingSecret !== "1145141919810") {
      return {
        ok: false,
        reason: "unauthorized",
        message: "Clash API 认证失败，请检查 Secret",
      };
    }

    return {
      ok: true,
      version: "v1.19.21",
      meta: true,
    };
  }

  async function getConnections(): Promise<ClashConnectionsResponse> {
    return {
      downloadTotal: 2560,
      uploadTotal: 1536,
      connections: [...mockConnections],
    };
  }

  async function deleteConnection(id: string): Promise<void> {
    mockConnections = mockConnections.filter((c) => c.id !== id);
  }

  async function closeAllConnections(): Promise<void> {
    mockConnections = [];
  }

  async function getRules(): Promise<ClashRulesResponse> {
    return {
      rules: mockRulesBase.map((rule, index) => ({
        ...rule,
        disable: mockDisabledRuleIndexes.has(index),
      })),
    };
  }

  async function patchRulesDisable(payload: Record<string, boolean>): Promise<void> {
    for (const [key, value] of Object.entries(payload || {})) {
      const index = Number.parseInt(key, 10);
      if (!Number.isFinite(index)) continue;

      if (value) {
        mockDisabledRuleIndexes.add(index);
      } else {
        mockDisabledRuleIndexes.delete(index);
      }
    }
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
