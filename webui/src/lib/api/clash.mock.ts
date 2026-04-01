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

export interface BoxConfigValues {
  clashApiPort: number;
  clashApiSecret: string;
  toggleAction: "service" | "tun" | "mode_cycle";
  toggleTunTarget: "toggle" | "on" | "off";
  toggleModeCycle: ("rule" | "global" | "direct")[];
}

let mockMode = "rule";
let mockTunEnabled = false;
let mockBoxConfig = 'clash_api_port=9090\nclash_api_secret=\ntoggle_action="service"\ntoggle_tun_target="toggle"\ntoggle_mode_cycle="rule,global,direct"\n';

let mockUploadTotal = 128 * 1024 * 1024;
let mockDownloadTotal = 512 * 1024 * 1024;

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

export async function getConfigs(): Promise<any> {
  const parsed = parseBoxConfig(mockBoxConfig);
  return {
    mode: mockMode,
    tun: { enable: mockTunEnabled },
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
  return;
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
