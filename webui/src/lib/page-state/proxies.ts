import type { ClashConfigs, ClashProxy, ClashProxyMap, ClashProxyProvider, ClashProxyProviderMap, ProxyMode } from "$lib/api";

export type NodeSortType = "default" | "latency" | "name";
export type GroupType = "Selector" | "URLTest" | "Fallback" | "LoadBalance" | "Smart";
export type ViewType = "proxies" | "providers";
export type ClashMode = ProxyMode;

export interface ProxyNode {
  key: string;
  name: string;
  index: number;
  latency: number;
  type: string;
}

export const GROUP_TYPES: GroupType[] = ["Selector", "URLTest", "Fallback", "LoadBalance", "Smart"];
export const DEFAULT_TEST_URL = "http://cp.cloudflare.com/generate_204";

export function formatProviderExpireDate(timestamp: number): string {
  if (!timestamp) return "长期有效";
  return new Date(timestamp * 1000).toLocaleDateString("zh-CN");
}

export function getLatencyStyle(ms: number) {
  if (!ms || ms === 0) return { text: "text-slate-400 dark:text-slate-500", dot: "bg-slate-300 dark:bg-slate-700" };
  if (ms < 200) return { text: "text-emerald-600 dark:text-emerald-400", dot: "bg-emerald-500" };
  if (ms < 800) return { text: "text-amber-600 dark:text-amber-400", dot: "bg-amber-500" };
  if (ms < 1500) return { text: "text-orange-600 dark:text-orange-400", dot: "bg-orange-500" };
  return { text: "text-rose-600 dark:text-rose-400", dot: "bg-rose-500" };
}

export function latencyBarClass(ms: number): string {
  if (!ms || ms <= 0) return "bg-slate-300 dark:bg-zinc-700";
  if (ms < 200) return "bg-emerald-500";
  if (ms < 800) return "bg-amber-500";
  if (ms < 1500) return "bg-orange-500";
  return "bg-rose-500";
}

function compactProxyName(name: string): string {
  return name.replace(/\s+/g, "");
}

export function readNodeLatency(latencies: Record<string, number>, name: string): number {
  if (Object.prototype.hasOwnProperty.call(latencies, name)) return latencies[name];
  const alias = compactProxyName(name);
  return Object.prototype.hasOwnProperty.call(latencies, alias) ? latencies[alias] : 0;
}

export function buildProxyDetailMap(proxies: ClashProxyMap, providers: ClashProxyProviderMap): ClashProxyMap {
  const details: ClashProxyMap = {};

  for (const provider of Object.values(providers)) {
    for (const proxy of provider.proxies || []) {
      if (!proxy?.name) continue;
      details[proxy.name] = { ...details[proxy.name], ...proxy };
    }
  }

  for (const [name, proxy] of Object.entries(proxies)) {
    details[name] = { ...details[name], ...proxy };
  }

  for (const proxy of Object.values(details)) {
    const alias = compactProxyName(proxy.name);
    if (alias && !details[alias]) details[alias] = proxy;
  }

  return details;
}

export function groupNodes(proxies: ClashProxyMap | null, details: ClashProxyMap, latencies: Record<string, number>, groupName: string): ProxyNode[] {
  const group = proxies?.[groupName];
  if (!group?.all) return [];

  return group.all
    .map((name, index) => {
      const detail = details[name] || details[compactProxyName(name)];
      return {
        key: `${groupName}:${index}:${name}`,
        name,
        index,
        latency: readNodeLatency(latencies, name),
        type: detail?.type || "Unknown",
      } satisfies ProxyNode;
    })
    .filter((item) => item.name.trim().length > 0);
}

export function sortedGroupNodes(
  proxies: ClashProxyMap | null,
  details: ClashProxyMap,
  latencies: Record<string, number>,
  groupSorts: Record<string, NodeSortType>,
  groupName: string,
): ProxyNode[] {
  const nodes = groupNodes(proxies, details, latencies, groupName);
  const sort = groupSorts[groupName] || "default";
  if (sort === "default") return nodes;

  return [...nodes].sort((a, b) => {
    if (sort === "name") {
      const byName = a.name.localeCompare(b.name);
      return byName !== 0 ? byName : a.index - b.index;
    }
    const aLatency = a.latency || Number.MAX_SAFE_INTEGER;
    const bLatency = b.latency || Number.MAX_SAFE_INTEGER;
    return aLatency !== bLatency ? aLatency - bLatency : a.index - b.index;
  });
}

export function isVisibleProxyGroup(proxy: ClashProxy | undefined): boolean {
  if (!proxy || !GROUP_TYPES.includes(proxy.type as GroupType) || proxy.hidden === true) return false;
  return proxy.type !== "Smart" || proxy.now?.trim().toLowerCase() !== "smart - select";
}

export function groupNames(proxies: ClashProxyMap | null): string[] {
  if (!proxies) return [];
  const names = Object.keys(proxies).filter((name) => isVisibleProxyGroup(proxies[name]));
  const globalOrder = proxies.GLOBAL?.all || [];

  return names.sort((a, b) => {
    const idxA = globalOrder.indexOf(a);
    const idxB = globalOrder.indexOf(b);
    return (idxA === -1 ? Number.MAX_SAFE_INTEGER : idxA) - (idxB === -1 ? Number.MAX_SAFE_INTEGER : idxB);
  });
}

export function providerNames(providers: ClashProxyProviderMap | null): string[] {
  return Object.keys(providers || {}).filter((name) => providers?.[name]?.vehicleType.toLowerCase() !== "compatible");
}

export function resolveProxyApiName(providers: ClashProxyProviderMap | null, name: string): string {
  const allProviders = Object.values(providers || {});
  const realProviders = allProviders.filter((provider) => provider.vehicleType.toLowerCase() !== "compatible");

  function findName(candidates: ClashProxyProvider[], compact: boolean): string | undefined {
    const expected = compact ? compactProxyName(name) : name;
    for (const provider of candidates) {
      const match = (provider.proxies || []).find((proxy) => (compact ? compactProxyName(proxy.name) : proxy.name) === expected);
      if (match) return match.name;
    }
    return undefined;
  }

  return findName(realProviders, false) || findName(realProviders, true) || findName(allProviders, false) || findName(allProviders, true) || name;
}

export function resolveProxyProviderName(providers: ClashProxyProviderMap | null, name: string, preferredName?: string): string | undefined {
  if (!providers) return undefined;
  if (preferredName && providers[preferredName]) return preferredName;

  const allProviders = Object.values(providers);
  const realProviders = allProviders.filter((provider) => provider.vehicleType.toLowerCase() !== "compatible");

  function findProvider(candidates: ClashProxyProvider[], compact: boolean): string | undefined {
    const expected = compact ? compactProxyName(name) : name;
    return candidates.find((provider) =>
      (provider.proxies || []).some((proxy) => (compact ? compactProxyName(proxy.name) : proxy.name) === expected),
    )?.name;
  }

  return findProvider(realProviders, false) || findProvider(realProviders, true) || findProvider(allProviders, false) || findProvider(allProviders, true);
}

export function providerUsableNodes(providers: ClashProxyProviderMap | null, latencies: Record<string, number>, name: string): number {
  const nodes = providers?.[name]?.proxies || [];
  return nodes.filter((item) => readNodeLatency(latencies, item.name) > 0).length;
}

export function resolveTestUrl(proxies: ClashProxyMap | null, providers: ClashProxyProviderMap | null, proxyTestUrl: string, groupName?: string): string {
  const globalUrl = proxyTestUrl.trim() || DEFAULT_TEST_URL;
  if (!groupName) return globalUrl;

  const proxyNode = proxies?.[groupName] as (ClashProxy & { testUrl?: string }) | undefined;
  if (proxyNode?.testUrl && proxyNode.testUrl.trim().length > 0) {
    return proxyNode.testUrl.trim();
  }

  const providerNode = providers?.[groupName] as (ClashProxyProvider & { testUrl?: string }) | undefined;
  if (providerNode?.testUrl && providerNode.testUrl.trim().length > 0) {
    return providerNode.testUrl.trim();
  }

  return globalUrl;
}

export function syncRecord<T extends Record<string, unknown>>(target: T | null, incoming: T): T {
  if (!target) {
    return incoming;
  }

  for (const key of Object.keys(target)) {
    if (!(key in incoming)) delete target[key];
  }
  for (const [key, value] of Object.entries(incoming)) {
    target[key as keyof T] = value as T[keyof T];
  }
  return target;
}

export function deriveLatencyMap(proxyData: ClashProxyMap): Record<string, number> {
  const nextLatency: Record<string, number> = {};
  const latestByAlias: Record<string, { delay: number; timestamp: number; order: number }> = {};
  let order = 0;

  for (const [name, item] of Object.entries(proxyData)) {
    const histories = [item.history || [], ...Object.values(item.extra || {}).map((health) => health.history || [])];
    let latest: { delay: number; timestamp: number; order: number } | undefined;

    for (const history of histories) {
      for (const entry of history) {
        if (typeof entry?.delay !== "number" || !Number.isFinite(entry.delay)) continue;
        const parsedTime = Date.parse(entry.time);
        const candidate = { delay: entry.delay, timestamp: Number.isFinite(parsedTime) ? parsedTime : 0, order: order++ };
        if (!latest || candidate.timestamp > latest.timestamp || (candidate.timestamp === latest.timestamp && candidate.order > latest.order)) {
          latest = candidate;
        }
      }
    }

    if (!latest) continue;
    nextLatency[name] = latest.delay;
    const alias = compactProxyName(name);
    const aliasLatest = latestByAlias[alias];
    if (!aliasLatest || latest.timestamp > aliasLatest.timestamp || (latest.timestamp === aliasLatest.timestamp && latest.order > aliasLatest.order)) {
      latestByAlias[alias] = latest;
    }
  }

  for (const name of Object.keys(proxyData)) {
    const alias = compactProxyName(name);
    const latest = latestByAlias[alias];
    if (!latest) continue;
    nextLatency[name] = latest.delay;
    nextLatency[alias] = latest.delay;
  }

  return nextLatency;
}

export function deriveProviderLatencyMap(providers: ClashProxyProviderMap): Record<string, number> {
  const latencies: Record<string, number> = {};

  for (const provider of Object.values(providers)) {
    for (const proxy of provider.proxies || []) {
      Object.assign(latencies, deriveLatencyMap({ [proxy.name]: proxy }));
    }
  }

  return latencies;
}

export function resolveProxyTestUrl(savedTestUrl: string, configData: ClashConfigs): string {
  const configTestUrl = typeof configData?.url === "string" ? configData.url.trim() : "";
  return savedTestUrl.trim() || configTestUrl || DEFAULT_TEST_URL;
}
