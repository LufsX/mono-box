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
  rules?: unknown[];
  ruleCount?: number;
  [key: string]: unknown;
}

export type ClashRuleProviderMap = Record<string, ClashRuleProvider>;

export type ControlMode = "disable" | "switch" | "tun" | "selector" | "mode";
export type ProxyMode = "rule" | "global" | "direct";

export interface BoxConfigValues {
  clashApiPort: number;
  clashApiSecret: string;
  toggleAction: "service" | "tun" | "mode_cycle";
  toggleTunTarget: "toggle" | "on" | "off";
  toggleModeCycle: ProxyMode[];
  useCustomDirect: boolean;
  controlMode: ControlMode;
  defaultTunEnable: string;
  directTunEnable: string;
  proxyTunEnable: string;
  directTunEnableList: string;
  selectOutbound: string;
  defaultOutbound: string;
  directOutbound: string;
  proxyOutbound: string;
  directOutboundList: string;
  defaultClashMode: string;
  directClashMode: string;
  proxyClashMode: string;
  directClashModeList: string;
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

let mockMode = "rule";
let mockTunEnabled = false;
let mockBoxConfig = `clash_api_port=9090
clash_api_secret=
toggle_action="service"
toggle_tun_target="toggle"
toggle_mode_cycle="rule,global,direct"
use_custom_direct=false
ctr_mode=disable
default_tun_enable="true"
direct_tun_enable="false"
proxy_tun_enable=
direct_tun_enable_list=""
select_outbound=""
default_outbound=""
direct_outbound=""
proxy_outbound=
direct_outbound_list=""
default_clash_mode=""
direct_clash_mode=""
proxy_clash_mode=
direct_clash_mode_list=""
`;

let mockUploadTotal = 128 * 1024 * 1024;
let mockDownloadTotal = 512 * 1024 * 1024;

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
};

let mockProviders: ClashProxyProviderMap = {
  "Provider-A": {
    name: "Provider-A",
    type: "Proxy",
    vehicleType: "HTTP",
    updatedAt: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
    proxies: [
      { name: "HK-A", type: "Shadowsocks" },
      { name: "JP-A", type: "Shadowsocks" },
      { name: "US-A", type: "Shadowsocks" },
      { name: "SG-A", type: "Shadowsocks" },
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

function randomInRange(min: number, max: number): number {
  return Math.floor(Math.random() * (max - min + 1)) + min;
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

function stripConfigValue(value: string): string {
  const trimmed = value.trim();
  if (trimmed.length >= 2 && trimmed.startsWith('"') && trimmed.endsWith('"')) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

function readConfigValue(content: string, key: string, fallback = ""): string {
  const match = content.match(new RegExp(`^${escapeRegExp(key)}=(.*)$`, "m"));
  return match ? stripConfigValue(match[1]) : fallback;
}

function normalizeControlMode(value: string): ControlMode {
  return value === "switch" || value === "tun" || value === "selector" || value === "mode" ? value : "disable";
}

function normalizeProxyMode(value: string): ProxyMode | null {
  const mode = value.trim().toLowerCase();
  return mode === "rule" || mode === "global" || mode === "direct" ? mode : null;
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

export function parseBoxConfig(content: string): BoxConfigValues {
  const portMatch = content.match(/^clash_api_port=(\d+)\s*$/m);
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

  return {
    clashApiPort: portMatch ? parseInt(portMatch[1], 10) : 9090,
    clashApiSecret: readConfigValue(content, "clash_api_secret"),
    toggleAction,
    toggleTunTarget,
    toggleModeCycle,
    useCustomDirect: readConfigValue(content, "use_custom_direct", "false") === "true",
    controlMode: normalizeControlMode(readConfigValue(content, "ctr_mode", "disable")),
    defaultTunEnable: readConfigValue(content, "default_tun_enable", "true"),
    directTunEnable: readConfigValue(content, "direct_tun_enable", "false"),
    proxyTunEnable: readConfigValue(content, "proxy_tun_enable"),
    directTunEnableList: readConfigValue(content, "direct_tun_enable_list"),
    selectOutbound: readConfigValue(content, "select_outbound"),
    defaultOutbound: readConfigValue(content, "default_outbound"),
    directOutbound: readConfigValue(content, "direct_outbound"),
    proxyOutbound: readConfigValue(content, "proxy_outbound"),
    directOutboundList: readConfigValue(content, "direct_outbound_list"),
    defaultClashMode: readConfigValue(content, "default_clash_mode"),
    directClashMode: readConfigValue(content, "direct_clash_mode"),
    proxyClashMode: readConfigValue(content, "proxy_clash_mode"),
    directClashModeList: readConfigValue(content, "direct_clash_mode_list"),
  };
}

export async function getConfigs(): Promise<any> {
  const parsed = parseBoxConfig(mockBoxConfig);
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

export async function setMode(mode: string): Promise<void> {
  mockMode = mode;
}

export async function setTun(enable: boolean): Promise<void> {
  mockTunEnabled = enable;
}

export async function upgradeCore(): Promise<any> {
  return {
    status: "ok",
    message: "Mock core upgrade completed",
  };
}

export async function createMemoryWebSocket(onMessage: (data: MemoryData) => void, _onError?: (error: Event) => void): Promise<WebSocket> {
  const socket = new MockSocket(() => {
    const inuse = randomInRange(20, 100) * 1024 * 1024;
    return {
      inuse,
      oslimit: 2 * 1024 * 1024 * 1024,
    } satisfies MemoryData;
  }, 1200);

  socket.onmessage = (event) => {
    const parsed = JSON.parse(event.data) as MemoryData;
    onMessage(parsed);
  };

  return socket as unknown as WebSocket;
}

export async function createTrafficWebSocket(onMessage: (data: TrafficData) => void, _onError?: (error: Event) => void): Promise<WebSocket> {
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
    } satisfies TrafficData;
  }, 1000);

  socket.onmessage = (event) => {
    const parsed = JSON.parse(event.data) as TrafficData;
    onMessage(parsed);
  };

  return socket as unknown as WebSocket;
}

export async function setOutbound(_selector: string, _outbound: string): Promise<void> {
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

export async function getProxies(): Promise<ClashProxyMap> {
  return mockProxies;
}

export async function testProxyDelay(name: string): Promise<number> {
  if (!mockProxies[name]) {
    return 0;
  }

  const delay = randomInRange(32, 880);
  const history = mockProxies[name].history ? [...(mockProxies[name].history as ClashProxyHistory[])] : [];
  history.push({ time: new Date().toISOString(), delay });

  if (history.length > 12) {
    history.splice(0, history.length - 12);
  }

  mockProxies = {
    ...mockProxies,
    [name]: {
      ...mockProxies[name],
      history,
    },
  };

  return delay;
}

export async function getProxyProviders(): Promise<ClashProxyProviderMap> {
  return mockProviders;
}

export async function getRuleProviders(): Promise<{ providers: ClashRuleProviderMap }> {
  return { providers: mockRuleProviders };
}

export async function updateRuleProvider(name: string): Promise<void> {
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

export async function updateProxyProvider(name: string): Promise<void> {
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

export async function healthCheckProxyProvider(name: string): Promise<void> {
  const provider = mockProviders[name];
  if (!provider) {
    throw new Error(`Provider not found: ${name}`);
  }

  for (const node of provider.proxies || []) {
    await testProxyDelay(node.name);
  }

  mockProviders = {
    ...mockProviders,
    [name]: {
      ...provider,
      updatedAt: new Date().toISOString(),
    },
  };
}

export async function getProxy(name: string): Promise<any> {
  return {
    name,
    type: "Selector",
    now: "DIRECT",
    all: ["DIRECT", "REJECT"],
  };
}

export async function checkStatus(): Promise<boolean> {
  return true;
}

export async function checkVersion(options?: { port?: number; secret?: string }): Promise<ClashVersionCheckResult> {
  const parsed = parseBoxConfig(mockBoxConfig);
  const incomingSecret = (options?.secret ?? parsed.clashApiSecret).trim();
  await new Promise((resolve) => setTimeout(resolve, 500));

  if (incomingSecret !== "") {
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

export function clearConfigCache(): void {
  return;
}

export async function readBoxConfig(): Promise<string> {
  return mockBoxConfig;
}

export async function writeBoxConfig(content: string): Promise<void> {
  mockBoxConfig = content.replace(/\r\n/g, "\n");
}

export async function updateBoxConfigValues(updates: Record<string, string>): Promise<void> {
  let content = mockBoxConfig;

  for (const [key, value] of Object.entries(updates)) {
    content = upsertConfigValue(content, key, value);
  }

  mockBoxConfig = content;
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

export async function getConnections(): Promise<ClashConnectionsResponse> {
  return {
    downloadTotal: 2560,
    uploadTotal: 1536,
    connections: [...mockConnections],
  };
}

export async function deleteConnection(id: string): Promise<void> {
  mockConnections = mockConnections.filter((c) => c.id !== id);
}

export async function closeAllConnections(): Promise<void> {
  mockConnections = [];
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
  rules: ClashRule[];
}

const mockRulesBase: Array<Omit<ClashRule, "disable" | "disabled">> = [
  { type: "DOMAIN-SUFFIX", payload: "google.com", proxy: "Proxy" },
  { type: "DOMAIN-KEYWORD", payload: "github", proxy: "Auto" },
  { type: "IP-CIDR", payload: "10.0.0.0/8", proxy: "DIRECT" },
  { type: "MATCH", payload: "", proxy: "Proxy" },
];

const mockDisabledRuleIndexes = new Set<number>();

export async function getRules(): Promise<ClashRulesResponse> {
  return {
    rules: mockRulesBase.map((rule, index) => ({
      ...rule,
      disable: mockDisabledRuleIndexes.has(index),
    })),
  };
}

export async function patchRulesDisable(payload: Record<string, boolean>): Promise<void> {
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
