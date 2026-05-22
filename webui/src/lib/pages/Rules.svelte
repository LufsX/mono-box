<script lang="ts">
  import { onMount } from "svelte";
  import { fade } from "svelte/transition";
  import { RefreshCw, ChevronRight } from "@lucide/svelte";
  import { clashApi } from "$lib/api";
  import type { ClashRuleProviderMap, ClashProxyMap } from "$lib/api";
  import { classifyConnectionError } from "$lib/api/error-utils";
  import KernelAuthNotice from "$lib/components/shared/KernelAuthNotice.svelte";
  import CheckBox from "$lib/components/common/CheckBox.svelte";
  import { clashPillTagClass } from "$lib/utils";

  type ViewType = "rules" | "providers";
  let currentView = $state<ViewType>("rules");

  let searchInput = $state("");
  let searchQuery = $state("");
  let searchTimer: ReturnType<typeof setTimeout> | null = null;

  function setSearch(value: string) {
    searchInput = value;
    if (searchTimer) clearTimeout(searchTimer);
    searchTimer = setTimeout(() => {
      searchQuery = value.trim().toLowerCase();
    }, 160);
  }

  type RuleRow = {
    index: number;
    type: string;
    payload: string;
    proxy: string;
    search: string;
    disabled: boolean;
    raw: unknown;
  };

  let ruleProviders = $state<ClashRuleProviderMap | null>(null);
  let providerNames = $state<string[]>([]);
  let proxies = $state<ClashProxyMap | null>(null);
  let proxiesEpoch = $state(0);
  const proxyChainCache = new Map<string, string[]>();

  let rules = $state<RuleRow[]>([]);
  let loading = $state(true);
  let error = $state("");
  let errorReason = $state<"unauthorized" | "unreachable" | "">("");
  let busyIndex = $state<number | null>(null);
  let updatingProvider = $state<string | null>(null);
  let updatingAllProviders = $state(false);

  function readRuleFields(item: any): { type: string; payload: string; proxy: string; disabled: boolean } {
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

  function syncRecord(target: Record<string, any> | null, incoming: Record<string, any>): Record<string, any> {
    if (!target) {
      return incoming;
    }

    for (const key of Object.keys(target)) {
      if (!(key in incoming)) delete target[key];
    }
    for (const [key, value] of Object.entries(incoming)) {
      (target as Record<string, any>)[key] = value;
    }
    return target;
  }

  function proxyChainParts(target: string): string[] {
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

  async function fetchRuleProviders() {
    const incoming = await clashApi.getRuleProviders();

    const fixed: ClashRuleProviderMap = {};
    for (const [key, value] of Object.entries(incoming || {})) {
      const item: any = value || {};
      const name = String((item as any).name ?? key).trim() || key;
      fixed[name] = { name, ...(item as any) };
    }

    ruleProviders = syncRecord(ruleProviders as Record<string, any> | null, fixed as Record<string, any>) as ClashRuleProviderMap;
    ruleProviders = ruleProviders;

    providerNames = Object.keys(fixed).sort((a, b) => a.localeCompare(b, "en", { sensitivity: "base" }));
  }

  async function fetchProxies() {
    const incoming = (await clashApi.getProxies()) as ClashProxyMap;
    proxies = syncRecord(proxies as Record<string, any> | null, incoming as Record<string, any>) as ClashProxyMap;
    proxies = proxies;

    proxiesEpoch += 1;
    proxyChainCache.clear();
  }

  async function fetchRules() {
    const res: any = await clashApi.getRules();
    const list = Array.isArray(res) ? res : (res?.rules ?? []);

    const items = Array.isArray(list) ? list : [];
    for (let index = 0; index < items.length; index++) {
      const item = items[index];
      const fields = readRuleFields(item);
      const search = `${fields.type}\n${fields.payload}\n${fields.proxy}`.toLowerCase();
      const row = rules[index];
      if (row) {
        row.index = index;
        row.raw = item;
        row.type = fields.type;
        row.payload = fields.payload;
        row.proxy = fields.proxy;
        row.search = search;
        row.disabled = fields.disabled;
      } else {
        rules.push({
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

    if (rules.length > items.length) {
      rules.splice(items.length, rules.length - items.length);
    }
    rules = rules;
  }

  async function refreshAll() {
    loading = true;
    try {
      error = "";
      errorReason = "";
      await Promise.all([fetchRuleProviders(), fetchProxies(), fetchRules()]);
    } catch (e: any) {
      const classified = classifyConnectionError(e);
      errorReason = classified.reason;
      error = classified.message;
    } finally {
      loading = false;
    }
  }

  async function updateProvider(event: MouseEvent, name: string) {
    event.preventDefault();
    event.stopPropagation();
    if (updatingProvider || updatingAllProviders) return;
    updatingProvider = name;
    try {
      await clashApi.updateRuleProvider(name);
      await Promise.all([fetchRuleProviders(), fetchRules()]);
    } catch (e: any) {
      const message = e?.message || String(e);
      error = `更新规则集合失败: ${message}`;
    } finally {
      updatingProvider = null;
    }
  }

  async function updateAllProviders(event: MouseEvent) {
    event.preventDefault();
    event.stopPropagation();
    if (updatingAllProviders) return;
    if (!providerNames.length) return;

    updatingAllProviders = true;
    try {
      error = "";

      for (const name of providerNames) {
        updatingProvider = name;
        await clashApi.updateRuleProvider(name);
      }

      updatingProvider = null;
      await Promise.all([fetchRuleProviders(), fetchRules()]);
    } catch (e: any) {
      const message = e?.message || String(e);
      error = `更新全部规则集合失败: ${message}`;
    } finally {
      updatingProvider = null;
      updatingAllProviders = false;
    }
  }

  async function toggleRuleEnabled(index: number) {
    const row = rules[index];
    if (!row) return;
    if (busyIndex !== null) return;

    const nextEnabled = row.disabled;
    const nextDisabled = !nextEnabled;

    busyIndex = index;
    try {
      await clashApi.patchRulesDisable({ [String(index)]: nextDisabled });
      row.disabled = nextDisabled;
    } catch (e: any) {
      error = e?.message || String(e) || "Failed to update rule";
    } finally {
      busyIndex = null;
    }
  }

  onMount(() => {
    void refreshAll();
  });

  const filteredProviderNames = $derived.by(() => {
    const q = searchQuery;
    if (!q || currentView !== "providers") return providerNames;
    return providerNames.filter((name) => name.toLowerCase().includes(q));
  });

  const filteredRules = $derived.by(() => {
    const q = searchQuery;
    if (!q || currentView !== "rules") return rules;
    return rules.filter((row) => row.search.includes(q));
  });
</script>

<main class="max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-4">
  <div class="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3">
    <div class="flex font-bold text-sm min-w-0 overflow-x-auto">
      <button
        class="px-4 py-1.5 transition-all duration-300 outline-none border -ml-px first:ml-0 rounded-l-lg {currentView === 'rules'
          ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
          : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
        onclick={() => {
          currentView = "rules";
        }}
      >
        规则列表 {rules.length}
      </button>
      <button
        class="px-4 py-1.5 transition-all duration-300 outline-none border -ml-px rounded-r-lg {currentView === 'providers'
          ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
          : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
        onclick={() => {
          currentView = "providers";
        }}
      >
        规则集合 {providerNames.length}
      </button>
    </div>

    <button
      class="justify-self-end inline-flex items-center justify-center gap-2 px-3 py-1.5 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-600 dark:text-zinc-300 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors disabled:opacity-60 text-sm font-bold whitespace-nowrap
      rounded-lg"
      onclick={updateAllProviders}
      hidden={currentView !== "providers"}
      disabled={updatingAllProviders || providerNames.length === 0}
      title="更新全部规则集合"
    >
      <RefreshCw size={14} class={updatingAllProviders ? "animate-spin" : ""} />
      <span>更新全部</span>
    </button>
  </div>

  <div class="flex justify-end">
    <input
      type="search"
      value={searchInput}
      oninput={(event) => setSearch((event.currentTarget as HTMLInputElement).value)}
      class="w-full px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors text-sm
      rounded-lg"
      placeholder={currentView === "providers" ? "搜索规则集合" : "搜索规则"}
    />
  </div>

  <div class="px-3 py-2 border border-slate-200 dark:border-zinc-800 bg-slate-50 dark:bg-zinc-900/50 text-slate-600 dark:text-zinc-300 text-xs rounded-lg">
    <div>规则禁用为临时操作，重启服务后失效。</div>
  </div>

  {#if loading}
    <div class="py-12 flex items-center justify-center text-slate-500 dark:text-slate-400 text-sm" in:fade={{ duration: 160 }}>正在加载规则...</div>
  {:else if error}
    <div in:fade={{ duration: 160 }}>
      <KernelAuthNotice reason={errorReason || "unreachable"} />
    </div>
  {:else}
    {#key currentView}
      <div in:fade={{ duration: 200 }}>
        {#if currentView === "providers"}
          <section class="space-y-2">
            <div class="space-y-3">
              {#if providerNames.length === 0}
                <div class="px-3 py-2 border border-slate-200 dark:border-zinc-800 bg-slate-50 dark:bg-zinc-900/50 text-slate-600 dark:text-zinc-300 text-xs rounded-lg" in:fade={{ duration: 160 }}>
                  未发现规则集合（Rule Providers）
                </div>
              {:else if filteredProviderNames.length === 0}
                <div class="px-3 py-2 border border-slate-200 dark:border-zinc-800 bg-slate-50 dark:bg-zinc-900/50 text-slate-600 dark:text-zinc-300 text-xs rounded-lg" in:fade={{ duration: 160 }}>
                  无匹配的规则集合
                </div>
              {:else}
                {#each filteredProviderNames as name (name)}
                  {@const provider = ruleProviders?.[name]}
                  <article class="border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-900/90 p-3 transition-colors rounded-xl">
                    <div class="flex items-center justify-between gap-3">
                      <div class="min-w-0">
                        <div class="flex items-center gap-2 min-w-0">
                          <div class="text-base font-extrabold text-slate-900 dark:text-slate-100 truncate">{name}</div>
                          {#if provider?.vehicleType}
                            <span class={clashPillTagClass("type", String(provider.vehicleType))}>{String(provider.vehicleType)}</span>
                          {/if}
                          {#if provider?.behavior}
                            <span class={clashPillTagClass("type", String(provider.behavior))}>{String(provider.behavior)}</span>
                          {/if}
                        </div>
                        <div class="mt-1 text-[11px] text-slate-400 dark:text-zinc-500">
                          {#if provider?.updatedAt}
                            <span>更新时间: <span class="font-mono">{new Date(String(provider.updatedAt)).toLocaleString("zh-CN")}</span></span>
                          {:else}
                            <span>更新时间: <span class="font-mono">-</span></span>
                          {/if}
                          {#if typeof provider?.ruleCount === "number"}
                            <span class="ml-3">规则数: <span class="font-mono">{provider.ruleCount}</span></span>
                          {/if}
                        </div>
                      </div>

                      <button
                        class="inline-flex items-center justify-center w-8 h-8 border border-slate-300 dark:border-zinc-700 text-slate-500 dark:text-zinc-400 hover:text-slate-900 dark:hover:text-zinc-100 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors disabled:opacity-60 rounded-lg"
                        onclick={(event) => updateProvider(event, name)}
                        disabled={updatingProvider === name || updatingAllProviders}
                        title="更新规则集合"
                      >
                        <RefreshCw size={14} class={updatingProvider === name ? "animate-spin" : ""} />
                      </button>
                    </div>
                  </article>
                {/each}
              {/if}
            </div>
          </section>
        {:else}
          <section class="space-y-3">
            <div class="space-y-3">
              {#if rules.length === 0}
                <div class="py-16 text-center text-sm text-slate-500 dark:text-slate-400" in:fade={{ duration: 160 }}>暂无规则</div>
              {:else if filteredRules.length === 0}
                <div class="py-16 text-center text-sm text-slate-500 dark:text-slate-400" in:fade={{ duration: 160 }}>无匹配的规则</div>
              {:else}
                {#each filteredRules as row (row.index)}
                  {@const enabled = !row.disabled}
                  {@const chainParts = proxyChainParts(row.proxy)}
                  <article
                    class="border p-3 transition-colors duration-200 rounded-xl {row.disabled
                      ? 'border-red-300 dark:border-red-900/40 bg-red-50/50 dark:bg-red-950/10'
                      : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-900/90'}"
                  >
                    <div class="flex items-start justify-between gap-3 mb-1">
                      <div class="flex items-center gap-2 min-w-0">
                        <div class="text-[10px] font-semibold text-slate-400 dark:text-zinc-500">#{row.index + 1}</div>
                        <span class={clashPillTagClass("type", row.type)}>{row.type}</span>
                        {#if row.disabled}
                          <span class={clashPillTagClass("status", "Disabled")} in:fade={{ duration: 140 }} out:fade={{ duration: 140 }}>Disabled</span>
                        {/if}
                      </div>

                      <div class={busyIndex === row.index ? "opacity-60 pointer-events-none" : ""}>
                        <CheckBox id={`rule-enabled-${row.index}`} checked={enabled} onchange={() => toggleRuleEnabled(row.index)} bare variant="switch" />
                      </div>
                    </div>

                    <div class="text-sm font-mono text-slate-900 dark:text-slate-200 break-all select-text">
                      {row.payload || "-"}
                    </div>

                    <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-2" title={chainParts.join(" > ")}>
                      {#if chainParts.length === 0}
                        <span class="text-[11px] text-slate-500 dark:text-zinc-400 font-mono">-</span>
                      {:else}
                        {#each chainParts as chain, i (i)}
                          <div class="flex items-center gap-1">
                            <div>
                              <span
                                class="inline-flex items-center px-1.5 py-0.5 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950/30 text-[11px] font-mono text-slate-700 dark:text-zinc-200
                              rounded-lg"
                              >
                                <span class="break-all">{chain}</span>
                              </span>
                            </div>
                            <div>
                              {#if i < chainParts.length - 1}
                                <ChevronRight size={14} class="text-slate-300 dark:text-zinc-600" />
                              {/if}
                            </div>
                          </div>
                        {/each}
                      {/if}
                    </div>
                  </article>
                {/each}
              {/if}
            </div>
          </section>
        {/if}
      </div>
    {/key}
  {/if}
</main>
