export interface ClashConfig {
  port: number;
  secret: string;
}

export type CoreStatusResult = { ok: true } | { ok: false; reason: "unauthorized" | "unreachable"; message: string };

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

export type ClashLogLevel = "debug" | "info" | "warning" | "error";

export interface ClashLogEntry {
  type: ClashLogLevel | string;
  payload: string;
}

export interface ClashProxyHistory {
  time: string;
  delay: number;
}

export interface ClashProxyHealth {
  alive: boolean;
  history: ClashProxyHistory[];
}

export interface ClashProxy {
  name: string;
  type: string;
  alive?: boolean;
  now?: string;
  all?: string[];
  collectData?: boolean;
  history?: ClashProxyHistory[];
  extra?: Record<string, ClashProxyHealth>;
  id?: string;
  "dialer-proxy"?: string;
  emptyFallback?: string;
  expectedStatus?: string;
  fixed?: string;
  hidden?: boolean;
  icon?: string;
  interface?: string;
  mptcp?: boolean;
  "policy-priority"?: string;
  preferASN?: boolean;
  "provider-name"?: string;
  "routing-mark"?: number;
  sampleRate?: number;
  smux?: boolean;
  testUrl?: string;
  tfo?: boolean;
  udp?: boolean;
  uot?: boolean;
  useLightGBM?: boolean;
  xudp?: boolean;
  [key: string]: unknown;
}

export type ClashProxyMap = Record<string, ClashProxy>;

export interface ClashProxyProvider {
  name: string;
  type: string;
  vehicleType: string;
  updatedAt?: string;
  proxies: ClashProxy[];
  testUrl?: string;
  expectedStatus?: string;
  subscriptionInfo?: {
    Download: number;
    Upload: number;
    Total: number;
    Expire: number;
  };
  [key: string]: unknown;
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
  controlMode: ControlMode;
  selectOutbound: string;
  targetCellular: string;
  targetWifi: string;
  targetWifiList: string;
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

export interface ClashConfigs {
  mode?: string;
  "mixed-port"?: number;
  port?: number;
  "socks-port"?: number;
  "redir-port"?: number;
  "tproxy-port"?: number;
  "allow-lan"?: boolean;
  "bind-address"?: string;
  "log-level"?: string;
  ipv6?: boolean;
  tun?: { enable: boolean; [key: string]: unknown };
  url?: string;
  [key: string]: unknown;
}

export interface UpgradeResult {
  status?: string;
  message?: string;
  [key: string]: unknown;
}

export interface ClashProxyDetail {
  name: string;
  type: string;
  now?: string;
  all?: string[];
  history?: ClashProxyHistory[];
  udp?: boolean;
  testUrl?: string;
  [key: string]: unknown;
}

export interface CommandResult {
  errno: number;
  stdout: string;
  stderr: string;
}

export interface ModuleInfo {
  id: string;
  name: string;
  version: string;
  versionCode: string;
  author: string;
  description: string;
  updateJson: string;
}

export interface ClashApiPort {
  checkVersion(options?: { port?: number; secret?: string }): Promise<ClashVersionCheckResult>;
  getConfigs(): Promise<ClashConfigs>;
  setMode(mode: string): Promise<void>;
  setTun(enable: boolean): Promise<void>;
  upgradeCore(): Promise<UpgradeResult>;
  createMemoryWebSocket(onMessage: (data: MemoryData) => void, onError?: (error: Event) => void): Promise<WebSocket>;
  createTrafficWebSocket(onMessage: (data: TrafficData) => void, onError?: (error: Event) => void): Promise<WebSocket>;
  createLogsWebSocket(level: ClashLogLevel | undefined, onMessage: (data: ClashLogEntry) => void, onError?: (error: Event) => void): Promise<WebSocket>;
  setOutbound(selector: string, outbound: string): Promise<void>;
  getProxies(): Promise<ClashProxyMap>;
  testProxyDelay(name: string, options?: { url?: string; timeout?: number; aliases?: string[] }): Promise<number>;
  getProxyProviders(): Promise<ClashProxyProviderMap>;
  getRuleProviders(): Promise<ClashRuleProviderMap>;
  updateRuleProvider(name: string): Promise<void>;
  updateProxyProvider(name: string): Promise<void>;
  healthCheckProxyProvider(name: string): Promise<void>;
  getProxy(name: string): Promise<ClashProxyDetail>;
  checkStatus(): Promise<CoreStatusResult>;
  getConnections(): Promise<ClashConnectionsResponse>;
  deleteConnection(id: string): Promise<void>;
  closeAllConnections(): Promise<void>;
  getRules(): Promise<ClashRulesResponse>;
  patchRulesDisable(payload: Record<string, boolean>): Promise<void>;
}
