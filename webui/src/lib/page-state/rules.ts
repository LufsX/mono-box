import type { ClashProxyMap, ClashRuleProviderMap } from "$lib/api";

export type RuleRow = {
  index: number;
  type: string;
  payload: string;
  proxy: string;
  search: string;
  disabled: boolean;
  raw: unknown;
};

export function readRuleFields(item: any): { type: string; payload: string; proxy: string; disabled: boolean } {
  if (typeof item === "string") {
    return { type: "RAW", payload: item, proxy: "", disabled: false };
  }
  if (!item || typeof item !== "object") {
    return { type: "RAW", payload: String(item ?? ""), proxy: "", disabled: false };
  }

  const type = String(item.type ?? item.ruleType ?? "").trim() || "Unknown";
  const payload = String(item.payload ?? item.value ?? item.pattern ?? "").trim();
  const proxy = String(item.proxy ?? item.outbound ?? item.adapter ?? "").trim();
  const disabledValue = item.disable ?? item.disabled;
  const disabled = typeof disabledValue === "boolean" ? disabledValue : false;

  return { type, payload, proxy, disabled };
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

export function normalizeRuleProviders(incoming: ClashRuleProviderMap): { fixed: ClashRuleProviderMap; names: string[] } {
  const fixed: ClashRuleProviderMap = {};
  for (const [key, value] of Object.entries(incoming || {})) {
    const item: any = value || {};
    const name = String(item.name ?? key).trim() || key;
    fixed[name] = { name, ...item };
  }

  return {
    fixed,
    names: Object.keys(fixed).sort((a, b) => a.localeCompare(b, "en", { sensitivity: "base" })),
  };
}

export function syncRules(target: RuleRow[], list: unknown): RuleRow[] {
  const items = Array.isArray(list) ? list : [];
  for (let index = 0; index < items.length; index++) {
    const item = items[index];
    const fields = readRuleFields(item);
    const search = `${fields.type}\n${fields.payload}\n${fields.proxy}`.toLowerCase();
    const row = target[index];
    if (row) {
      row.index = index;
      row.raw = item;
      row.type = fields.type;
      row.payload = fields.payload;
      row.proxy = fields.proxy;
      row.search = search;
      row.disabled = fields.disabled;
    } else {
      target.push({
        index,
        raw: item,
        type: fields.type,
        payload: fields.payload,
        proxy: fields.proxy,
        search,
        disabled: fields.disabled,
      });
    }
  }

  if (target.length > items.length) {
    target.splice(items.length, target.length - items.length);
  }
  return target;
}

export function proxyChainParts(proxies: ClashProxyMap | null, proxyChainCache: Map<string, string[]>, proxiesEpoch: number, target: string): string[] {
  const name = target.trim();
  if (!name) return [];
  if (!proxies) return [name];

  const cacheKey = `${proxiesEpoch}:${name}`;
  const cached = proxyChainCache.get(cacheKey);
  if (cached) return cached;

  const visited = new Set<string>();
  const chain: string[] = [];
  let current = name;
  for (let i = 0; i < 6; i++) {
    if (!current || visited.has(current)) break;
    visited.add(current);
    chain.push(current);

    const node = proxies[current];
    const next = typeof node?.now === "string" ? node.now.trim() : "";
    const canExpand = next && Array.isArray(node?.all);
    if (!canExpand) break;
    current = next;
  }

  proxyChainCache.set(cacheKey, chain);
  return chain;
}
