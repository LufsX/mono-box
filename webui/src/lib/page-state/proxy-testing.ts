import type { ClashProxy, ClashProxyMap, ClashProxyProviderMap } from "$lib/api";

export interface ProxyTestTarget {
  name: string;
  apiName?: string;
  providerName?: string;
  contextName?: string;
}

export interface ProxyLatencyResult {
  delay: number;
  failed: boolean;
  source: "history" | "test";
  /** Core clock only: never compare device history to the WebView clock. */
  historyTime: number;
  revision: number;
}

export interface ProxyTestState {
  nodes: Record<string, ProxyLatencyResult>;
  revision: number;
}

export function proxyTestKey(name: string, providerName?: string): string {
  return JSON.stringify([providerName ?? null, name]);
}

export function testTargetNames(target: ProxyTestTarget): string[] {
  return [proxyTestKey(target.name, target.providerName)];
}

export function emptyProxyTestState(): ProxyTestState {
  return { nodes: {}, revision: 0 };
}

export function readNodeTestResult(state: ProxyTestState, name: string, providerName?: string): ProxyLatencyResult | undefined {
  return state.nodes[proxyTestKey(name, providerName)];
}

export function readNodeTestLatency(state: ProxyTestState, name: string, providerName?: string): number {
  return readNodeTestResult(state, name, providerName)?.delay || 0;
}

export function isNodeTestFailed(state: ProxyTestState, name: string, providerName?: string): boolean {
  return Boolean(readNodeTestResult(state, name, providerName)?.failed);
}

function latestHistory(proxy: ClashProxy): { delay: number; time: number } | undefined {
  let latest: { delay: number; time: number } | undefined;
  for (const entry of [proxy.history || [], ...Object.values(proxy.extra || {}).map((health) => health.history || [])].flat()) {
    const time = Date.parse(entry.time);
    if (!Number.isFinite(time) || !Number.isFinite(entry.delay) || entry.delay < 0) continue;
    if (!latest || time > latest.time) latest = { delay: entry.delay, time };
  }
  return latest;
}

export function seedProxyTestState(
  previous: ProxyTestState,
  proxies: ClashProxyMap,
  providers: ClashProxyProviderMap,
  requestRevision = previous.revision,
): ProxyTestState {
  const nodes: Record<string, ProxyLatencyResult> = {};
  function seed(key: string, proxy: ClashProxy) {
    const existing = previous.nodes[key];
    const history = latestHistory(proxy);
    // A refresh started before a test completed cannot overwrite that result.
    if (existing && (existing.revision > requestRevision || (!history && existing.source === "test") || (history && history.time <= existing.historyTime))) {
      nodes[key] = existing;
    } else if (history) {
      nodes[key] = { delay: history.delay, failed: history.delay === 0, source: "history", historyTime: history.time, revision: previous.revision };
    }
  }
  for (const [name, proxy] of Object.entries(proxies)) seed(proxyTestKey(name), proxy);
  for (const [providerName, provider] of Object.entries(providers)) {
    for (const proxy of provider.proxies || []) seed(proxyTestKey(proxy.name, providerName), proxy);
  }
  // Retain newly completed tests if this snapshot predates them; otherwise prune removed resources.
  for (const [key, result] of Object.entries(previous.nodes)) {
    if (result.revision > requestRevision) nodes[key] = result;
  }
  return { nodes, revision: previous.revision };
}

export function commitProxyTestResult(state: ProxyTestState, target: ProxyTestTarget, delay: number): ProxyTestState {
  const key = proxyTestKey(target.name, target.providerName);
  const revision = state.revision + 1;
  const result: ProxyLatencyResult = {
    delay: Number.isFinite(delay) && delay > 0 ? Math.trunc(delay) : 0,
    failed: !Number.isFinite(delay) || delay <= 0,
    source: "test",
    historyTime: state.nodes[key]?.historyTime ?? -Infinity,
    revision,
  };
  return { nodes: { ...state.nodes, [key]: result }, revision };
}
