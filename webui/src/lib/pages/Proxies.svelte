<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { fade, fly, scale } from "svelte/transition";
  import { cubicOut } from "svelte/easing";
  import { Activity, ArrowUpDown, ChevronRight, Clock3, RefreshCw, ArrowUpNarrowWide, Timer, X } from "@lucide/svelte";
  import Select from "$lib/components/Select.svelte";
  import ProxyNodeTile from "$lib/components/ProxyNodeTile.svelte";
  import * as clashRealApi from "$lib/api/clash";
  import * as clashMockApi from "$lib/api/clash.mock";
  import { loadHomeLayoutSettings, roundedStore } from "$lib/settings";

  type NodeSortType = "default" | "latency" | "name";
  type GroupType = "Selector" | "URLTest" | "Fallback" | "LoadBalance";
  type ViewType = "proxies" | "providers";
  type ClashMode = "rule" | "global" | "direct";

  interface ProxyNode {
    key: string;
    name: string;
    index: number;
    latency: number;
    type: string;
  }

  const isProd = import.meta.env.MODE !== "production";
  const clashApi = isProd ? clashMockApi : clashRealApi;
  const r = $derived($roundedStore);
  const GROUP_TYPES: GroupType[] = ["Selector", "URLTest", "Fallback", "LoadBalance"];
  const DEFAULT_TEST_URL = "http://cp.cloudflare.com/generate_204";

  let proxies = $state<Record<string, clashRealApi.ClashProxy> | null>(null);
  let providers = $state<Record<string, clashRealApi.ClashProxyProvider> | null>(null);
  let currentMode = $state<ClashMode>("rule");
  let modeSelectValue = $state<ClashMode>("rule");
  let currentView = $state<ViewType>("proxies");
  let proxyTestUrl = $state(loadHomeLayoutSettings().proxyTestUrl);

  let loading = $state(true);
  let error = $state("");

  let groupSorts = $state<Record<string, NodeSortType>>({});
  let latencies = $state<Record<string, number>>({});
  let testingOwners = $state<Record<string, number>>({});
  let testingNodes = $state<Record<string, number>>({});
  let testingProgress = $state<Record<string, { done: number; total: number }>>({});
  let updatingProvider = $state<string | null>(null);

  let activeGroup = $state<string | null>(null);
  let activeProvider = $state<string | null>(null);
  let modalHistoryPushed = false;

  function pushModalHistory() {
    if (modalHistoryPushed) return;
    if (typeof history === "undefined") return;
    history.pushState({ ...(history.state || {}), __modal: "proxies" }, "");
    modalHistoryPushed = true;
  }

  function handlePopState() {
    if (!activeGroup && !activeProvider) return;
    activeGroup = null;
    activeProvider = null;
    modalHistoryPushed = false;
  }

  const modeOptions: { value: ClashMode; label: string }[] = [
    { value: "rule", label: "规则" },
    { value: "global", label: "全局" },
    { value: "direct", label: "直连" },
  ];

  function formatBytes(value: number): string {
    if (!value || value <= 0) return "0 B";
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    let size = value;
    let idx = 0;
    while (size >= 1024 && idx < units.length - 1) {
      size /= 1024;
      idx += 1;
    }
    return `${size.toFixed(idx <= 1 ? 0 : 1)} ${units[idx]}`;
  }

  function formatDate(timestamp: number): string {
    if (!timestamp) return "长期有效";
    return new Date(timestamp * 1000).toLocaleDateString("zh-CN");
  }

  function getLatencyStyle(ms: number) {
    if (!ms || ms === 0) return { text: "text-slate-400 dark:text-slate-500", dot: "bg-slate-300 dark:bg-slate-700" };
    if (ms < 200) return { text: "text-emerald-600 dark:text-emerald-400", dot: "bg-emerald-500" };
    if (ms < 800) return { text: "text-amber-600 dark:text-amber-400", dot: "bg-amber-500" };
    if (ms < 1500) return { text: "text-orange-600 dark:text-orange-400", dot: "bg-orange-500" };
    return { text: "text-rose-600 dark:text-rose-400", dot: "bg-rose-500" };
  }

  function latencyBarClass(ms: number): string {
    if (!ms || ms <= 0) return "bg-slate-300 dark:bg-zinc-700";
    if (ms < 200) return "bg-emerald-500";
    if (ms < 800) return "bg-amber-500";
    if (ms < 1500) return "bg-orange-500";
    return "bg-rose-500";
  }

  function readNodeLatency(name: string): number {
    return latencies[name] || 0;
  }

  function groupNodes(groupName: string): ProxyNode[] {
    const group = proxies?.[groupName];
    if (!group?.all) return [];

    return group.all
      .map((name, index) => {
        const detail = proxies?.[name];
        return {
          key: `${groupName}:${index}:${name}`,
          name,
          index,
          latency: readNodeLatency(name),
          type: detail?.type || "Unknown",
        } satisfies ProxyNode;
      })
      .filter((item) => item.name.trim().length > 0);
  }

  function sortedGroupNodes(groupName: string): ProxyNode[] {
    const nodes = groupNodes(groupName);
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

  function groupNames(): string[] {
    if (!proxies) return [];
    const names = Object.keys(proxies).filter((name) => GROUP_TYPES.includes((proxies?.[name]?.type as GroupType) || "Selector"));
    const globalOrder = proxies.GLOBAL?.all || [];

    return names.sort((a, b) => {
      const idxA = globalOrder.indexOf(a);
      const idxB = globalOrder.indexOf(b);
      return (idxA === -1 ? Number.MAX_SAFE_INTEGER : idxA) - (idxB === -1 ? Number.MAX_SAFE_INTEGER : idxB);
    });
  }

  function providerNames(): string[] {
    return Object.keys(providers || {}).filter((name) => providers?.[name]?.vehicleType !== "Compatible");
  }

  function providerUsableNodes(name: string): number {
    const nodes = providers?.[name]?.proxies || [];
    return nodes.filter((item) => readNodeLatency(item.name) > 0).length;
  }

  function getTestUrl(groupName?: string): string {
    const globalUrl = proxyTestUrl.trim() || DEFAULT_TEST_URL;
    if (!groupName) return globalUrl;

    const proxyNode = proxies?.[groupName] as { testUrl?: string } | undefined;
    if (proxyNode?.testUrl && proxyNode.testUrl.trim().length > 0) {
      return proxyNode.testUrl.trim();
    }

    const providerNode = providers?.[groupName] as { testUrl?: string } | undefined;
    if (providerNode?.testUrl && providerNode.testUrl.trim().length > 0) {
      return providerNode.testUrl.trim();
    }

    return globalUrl;
  }

  function openGroup(groupName: string) {
    if (!activeGroup && !activeProvider) pushModalHistory();
    activeProvider = null;
    activeGroup = groupName;
  }

  function openProvider(name: string) {
    if (!activeGroup && !activeProvider) pushModalHistory();
    activeGroup = null;
    activeProvider = name;
  }

  function closeDetail() {
    if (modalHistoryPushed && typeof history !== "undefined") {
      history.back();
      return;
    }
    activeGroup = null;
    activeProvider = null;
    modalHistoryPushed = false;
  }

  function cycleGroupSort(groupName: string) {
    const orders: NodeSortType[] = ["default", "latency", "name"];
    const current = groupSorts[groupName] || "default";
    groupSorts = { ...groupSorts, [groupName]: orders[(orders.indexOf(current) + 1) % orders.length] };
  }

  function testStart(owner: string, nodes: string[]) {
    testingOwners = { ...testingOwners, [owner]: (testingOwners[owner] || 0) + 1 };
    testingProgress = { ...testingProgress, [owner]: { done: 0, total: Math.max(1, nodes.length) } };
    if (!nodes.length) return;
    const next = { ...testingNodes };
    for (const node of nodes) {
      if (!node) continue;
      next[node] = (next[node] || 0) + 1;
    }
    testingNodes = next;
  }

  function testEnd(owner: string, nodes: string[]) {
    const ownerCount = testingOwners[owner] || 0;
    if (ownerCount <= 1) {
      const { [owner]: _, ...rest } = testingOwners;
      testingOwners = rest;
    } else {
      testingOwners = { ...testingOwners, [owner]: ownerCount - 1 };
    }
    if (!nodes.length) return;

    const { [owner]: __, ...progressRest } = testingProgress;
    testingProgress = progressRest;

    const next = { ...testingNodes };
    for (const node of nodes) {
      const count = next[node] || 0;
      if (count <= 1) delete next[node];
      else next[node] = count - 1;
    }
    testingNodes = next;
  }

  function stepProgress(owner: string) {
    const current = testingProgress[owner];
    if (!current) return;
    const done = Math.min(current.total, current.done + 1);
    testingProgress = { ...testingProgress, [owner]: { ...current, done } };
  }

  async function loadData() {
    try {
      error = "";
      const [proxyData, providerData, configData] = await Promise.all([clashApi.getProxies(), clashApi.getProxyProviders(), clashApi.getConfigs()]);

      proxies = proxyData;
      providers = providerData;
      if (configData?.mode) {
        currentMode = configData.mode as ClashMode;
        modeSelectValue = currentMode;
      }

      const nextLatency: Record<string, number> = {};
      for (const [name, item] of Object.entries(proxyData)) {
        const history = item.history || [];
        if (!history.length) continue;
        const last = history[history.length - 1];
        if (typeof last?.delay === "number") nextLatency[name] = last.delay;
      }
      latencies = nextLatency;
      const savedTestUrl = loadHomeLayoutSettings().proxyTestUrl.trim();
      const configTestUrl = typeof configData?.url === "string" ? configData.url.trim() : "";
      proxyTestUrl = savedTestUrl || configTestUrl || DEFAULT_TEST_URL;
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      error = `获取策略组失败: ${message}`;
      proxies = null;
      providers = null;
    }
  }

  async function switchMode(mode: ClashMode) {
    if (mode === currentMode) return;
    try {
      await clashApi.setMode(mode);
      currentMode = mode;
      modeSelectValue = mode;
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      error = `切换模式失败: ${message}`;
      modeSelectValue = currentMode;
    }
  }

  async function selectNode(groupName: string, nodeName: string) {
    const group = proxies?.[groupName];
    if (!group || group.type !== "Selector" || group.now === nodeName) return;
    try {
      await clashApi.setOutbound(groupName, nodeName);
      proxies = { ...(proxies || {}), [groupName]: { ...group, now: nodeName } };
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      error = `切换策略失败: ${message}`;
    }
  }

  async function testSingleNode(event: MouseEvent, ownerKey: string, nodeName: string, groupName?: string) {
    event.stopPropagation();
    if (testingOwners[ownerKey]) return;
    testStart(ownerKey, [nodeName]);
    try {
      const delay = await clashApi.testProxyDelay(nodeName, { url: getTestUrl(groupName) });
      latencies = { ...latencies, [nodeName]: delay };
    } catch {
      latencies = { ...latencies, [nodeName]: 0 };
    } finally {
      stepProgress(ownerKey);
      testEnd(ownerKey, [nodeName]);
    }
  }

  async function testGroup(event: MouseEvent, groupName: string) {
    event.stopPropagation();
    const owner = `group:${groupName}`;
    if (testingOwners[owner]) return;
    const nodes = groupNodes(groupName).map((item) => item.name);
    if (!nodes.length) return;

    testStart(owner, nodes);
    try {
      const entries = await Promise.all(
        nodes.map(async (name) => {
          try {
            const delay = await clashApi.testProxyDelay(name, { url: getTestUrl(groupName) });
            return [name, delay] as const;
          } catch {
            return [name, 0] as const;
          } finally {
            stepProgress(owner);
          }
        }),
      );
      const next = { ...latencies };
      for (const [name, delay] of entries) next[name] = delay;
      latencies = next;
    } finally {
      testEnd(owner, nodes);
    }
  }

  async function updateProvider(event: MouseEvent, name: string) {
    event.stopPropagation();
    if (updatingProvider) return;
    updatingProvider = name;
    try {
      await clashApi.updateProxyProvider(name);
      await loadData();
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      error = `更新 Provider 失败: ${message}`;
    } finally {
      updatingProvider = null;
    }
  }

  async function testProvider(event: MouseEvent, name: string) {
    event.stopPropagation();
    const owner = `provider:${name}`;
    if (testingOwners[owner]) return;
    const nodes = (providers?.[name]?.proxies || []).map((item) => item.name);
    testStart(owner, nodes);
    const progressTimer = setInterval(() => {
      const current = testingProgress[owner];
      if (!current) return;
      if (current.done < Math.max(0, current.total - 1)) {
        stepProgress(owner);
      }
    }, 220);

    try {
      await clashApi.healthCheckProxyProvider(name);
      await loadData();
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      error = `Provider 测速失败: ${message}`;
    } finally {
      clearInterval(progressTimer);
      const current = testingProgress[owner];
      if (current) {
        testingProgress = {
          ...testingProgress,
          [owner]: { ...current, done: current.total },
        };
      }
      testEnd(owner, nodes);
    }
  }

  async function testProviderNode(event: MouseEvent, providerName: string, nodeName: string) {
    event.stopPropagation();
    const owner = `provider-node:${providerName}:${nodeName}`;
    if (testingOwners[owner]) return;

    testStart(owner, [nodeName]);
    try {
      const delay = await clashApi.testProxyDelay(nodeName, { url: getTestUrl(providerName) });
      latencies = { ...latencies, [nodeName]: delay };
    } catch {
      latencies = { ...latencies, [nodeName]: 0 };
    } finally {
      stepProgress(owner);
      testEnd(owner, [nodeName]);
    }
  }

  onMount(() => {
    (async () => {
      loading = true;
      try {
        await loadData();
      } finally {
        loading = false;
      }
    })();

    window.addEventListener("popstate", handlePopState);
  });

  onDestroy(() => {
    window.removeEventListener("popstate", handlePopState);
  });

  $effect(() => {
    if (modeSelectValue !== currentMode) {
      void switchMode(modeSelectValue);
    }
  });
</script>

<main class="max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-4">
  <section in:fly={{ y: 10, duration: 220, easing: cubicOut }} class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 p-3 transition-colors {r ? 'rounded-xl' : ''}">
    <div class="grid grid-cols-[minmax(0,1fr)_auto] gap-2 items-center">
      <Select id="proxy-mode" options={modeOptions} bind:value={modeSelectValue} />

      <div class="flex font-bold text-sm">
        <button
          class="px-4 py-1.5 transition-all duration-300 outline-none border -ml-px first:ml-0 {r ? 'rounded-l-lg' : ''} {currentView === 'proxies'
            ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
            : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
          onclick={() => {
            currentView = "proxies";
          }}>策略组</button
        >
        <button
          class="px-4 py-1.5 transition-all duration-300 outline-none border -ml-px {r ? 'rounded-r-lg' : ''} {currentView === 'providers'
            ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
            : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
          onclick={() => {
            currentView = "providers";
          }}>代理提供商</button
        >
      </div>
    </div>
  </section>

  <section in:fly={{ y: 12, duration: 260, easing: cubicOut }} class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors {r ? 'rounded-xl' : ''}">
    <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 flex items-center justify-between gap-3 {r ? 'rounded-t-xl' : ''}">
      <div class="flex items-center gap-2 min-w-0">
        <Activity size={16} class="text-slate-500 dark:text-slate-400" />
        <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0 truncate">策略组控制</h2>
      </div>
    </div>

    <div class="p-3 space-y-3">
      {#if loading}
        <div in:fade={{ duration: 180 }} class="py-12 flex flex-col items-center justify-center gap-2 text-slate-500 dark:text-slate-400">
          <RefreshCw size={18} class="animate-spin" />
          <span class="text-sm">正在获取代理信息...</span>
        </div>
      {:else if error}
        <div in:fade={{ duration: 180 }} class="px-3 py-2 bg-red-50 dark:bg-red-950 border border-red-300 dark:border-red-800 text-red-700 dark:text-red-300 text-sm {r ? 'rounded-lg' : ''}">
          {error}
        </div>
      {:else if currentView === "proxies" && !groupNames().length}
        <div in:fade={{ duration: 180 }} class="py-12 text-center text-sm text-slate-500 dark:text-slate-400">未发现可控制的策略组</div>
      {:else if currentView === "providers" && !providerNames().length}
        <div in:fade={{ duration: 180 }} class="py-12 text-center text-sm text-slate-500 dark:text-slate-400">未发现可用 Provider</div>
      {:else}
        {#key currentView}
          <div class="space-y-3" in:fade={{ duration: 200 }}>
            {#if currentView === "proxies"}
              <div class="space-y-3 overflow-visible">
                {#each groupNames() as groupName (groupName)}
                  {@const group = proxies?.[groupName]}
                  {@const nowDelay = readNodeLatency(group?.now || "")}
                  {@const nowStyle = getLatencyStyle(nowDelay)}
                  {@const ownerKey = `group:${groupName}`}
                  {@const isTestingGroup = !!testingOwners[ownerKey]}
                  {@const progress = testingProgress[ownerKey]}

                  <article class="relative border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-900/50 transition-colors overflow-hidden {r ? 'rounded-xl' : ''}">
                    <div
                      class="w-full px-3 py-3 flex items-center justify-between gap-3 hover:bg-slate-50 dark:hover:bg-zinc-900/70 transition-colors text-left"
                      role="button"
                      tabindex="0"
                      onclick={() => openGroup(groupName)}
                      onkeydown={(event) => {
                        if (event.key === "Enter" || event.key === " ") {
                          event.preventDefault();
                          openGroup(groupName);
                        }
                      }}
                    >
                      <div class="min-w-0">
                        <div class="flex items-center gap-2 min-w-0">
                          <p class="text-sm font-bold text-slate-900 dark:text-slate-200 truncate pl-0.5">{groupName}</p>
                          <span class="text-[8px] border border-slate-300 dark:border-zinc-700 text-slate-500 dark:text-zinc-400 px-1.5 py-0.5 uppercase {r ? 'rounded-md' : ''}">{group?.type}</span>
                        </div>
                        <div class="mt-1 text-xs text-slate-500 dark:text-zinc-400 min-w-0">
                          <span class="truncate block">当前: <span class="font-mono">{group?.now || "-"}</span></span>
                        </div>
                      </div>

                      <div class="flex items-center gap-1.5 shrink-0">
                        <button
                          class="inline-flex items-center justify-center px-1.5 py-0.5 min-w-14 border border-slate-300 dark:border-zinc-700 font-mono text-[10px] tabular-nums {nowStyle.text} hover:text-slate-800 dark:hover:text-zinc-100 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors disabled:opacity-60
                          {r ? 'rounded-lg' : ''}"
                          onclick={(event) => testGroup(event, groupName)}
                          disabled={isTestingGroup}
                          title="点击重新测速"
                        >
                          {#if isTestingGroup}
                            {progress ? `${Math.round((progress.done / Math.max(progress.total, 1)) * 100)}%` : "测试中"}
                          {:else}
                            {nowDelay ? `${nowDelay} ms` : "测速"}
                          {/if}
                        </button>

                        <ChevronRight size={16} class="text-slate-500 dark:text-zinc-400" />
                      </div>
                    </div>
                  </article>
                {/each}
              </div>
            {:else}
              <div class="space-y-3">
                {#each providerNames() as name (name)}
                  {@const provider = providers?.[name]}
                  {@const isTesting = !!testingOwners[`provider:${name}`]}
                  {@const progress = testingProgress[`provider:${name}`]}

                  <article class="border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-900/50 transition-colors overflow-hidden {r ? 'rounded-xl' : ''}">
                    <div
                      class="w-full px-3 py-3 hover:bg-slate-50 dark:hover:bg-zinc-900/70 transition-colors text-left"
                      role="button"
                      tabindex="0"
                      onclick={() => openProvider(name)}
                      onkeydown={(event) => {
                        if (event.key === "Enter" || event.key === " ") {
                          event.preventDefault();
                          openProvider(name);
                        }
                      }}
                    >
                      <div class="flex items-center justify-between gap-3">
                        <div class="min-w-0">
                          <div class="flex items-center gap-2 min-w-0">
                            <p class="text-base font-extrabold text-slate-900 dark:text-slate-100 truncate">{name}</p>
                            <span class="text-[9px] border border-slate-300 dark:border-zinc-700 text-slate-400 dark:text-zinc-500 px-1.5 py-0.5 uppercase {r ? 'rounded-md' : ''}"
                              >{provider?.vehicleType}</span
                            >
                          </div>
                          <div class="mt-1 text-[11px] text-slate-400 dark:text-zinc-500">可用节点: {providerUsableNodes(name)} / {provider?.proxies?.length || 0}</div>
                        </div>

                        <div class="flex items-center gap-1.5 shrink-0 self-center">
                          <button
                            class="inline-flex items-center justify-center w-7 h-7 border border-slate-300 dark:border-zinc-700 text-slate-500 dark:text-zinc-400 hover:text-slate-800 dark:hover:text-zinc-200 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors disabled:opacity-60
                            {r ? 'rounded-lg' : ''}"
                            onclick={(event) => testProvider(event, name)}
                            disabled={isTesting}
                            title="测速 Provider"
                          >
                            {#if isTesting}
                              <span class="font-mono text-[11px]">{progress ? `${Math.round((progress.done / Math.max(progress.total, 1)) * 100)}%` : "..."}</span>
                            {:else}
                              <Timer size={14} />
                            {/if}
                          </button>
                          <button
                            class="inline-flex items-center justify-center w-7 h-7 border border-slate-300 dark:border-zinc-700 text-slate-500 dark:text-zinc-400 hover:text-slate-800 dark:hover:text-zinc-200 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors disabled:opacity-60
                            {r ? 'rounded-lg' : ''}"
                            onclick={(event) => updateProvider(event, name)}
                            disabled={updatingProvider === name}
                            title="更新 Provider"
                          >
                            <RefreshCw size={14} class={updatingProvider === name ? "animate-spin" : ""} />
                          </button>
                          <ChevronRight size={16} class="text-slate-500 dark:text-zinc-400" />
                        </div>
                      </div>

                      {#if provider?.subscriptionInfo}
                        <div class="mt-1 text-[10px] text-slate-400 dark:text-zinc-500 space-y-0.5">
                          <div class="flex items-center gap-2">
                            <span>流量</span>
                            <span class="font-mono"
                              >{formatBytes((provider.subscriptionInfo.Download || 0) + (provider.subscriptionInfo.Upload || 0))} / {formatBytes(provider.subscriptionInfo.Total || 0)}</span
                            >
                          </div>
                          <div class="flex items-center gap-2">
                            <span>到期</span>
                            <span class="font-mono">{formatDate(provider.subscriptionInfo.Expire || 0)}</span>
                          </div>
                        </div>
                      {/if}

                      <div class="mt-2 h-2 w-full border border-slate-300 dark:border-zinc-700 overflow-hidden {r ? 'rounded-full' : ''}">
                        <div class="h-full w-full flex">
                          {#each provider?.proxies || [] as node, idx (`provider-bar:${name}:${node.name}:${idx}`)}
                            {@const delay = readNodeLatency(node.name)}
                            <div class={`h-full ${latencyBarClass(delay)}`} style={`width:${100 / Math.max((provider?.proxies || []).length, 1)}%`}></div>
                          {/each}
                        </div>
                      </div>
                    </div>
                  </article>
                {/each}
              </div>
            {/if}
          </div>
        {/key}
      {/if}
    </div>
  </section>

  {#if activeGroup}
    {@const openedGroup = activeGroup}
    {@const ownerKey = `group:${openedGroup}`}
    {@const isTestingGroup = !!testingOwners[ownerKey]}
    <div
      class="fixed inset-0 z-50 bg-slate-950/55 p-3 md:p-6 flex items-center justify-center"
      role="button"
      tabindex="0"
      in:fade={{ duration: 160 }}
      out:fade={{ duration: 140 }}
      onclick={(event) => {
        if (event.target === event.currentTarget) closeDetail();
      }}
      onkeydown={(event) => {
        if (event.key === "Escape" || event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          closeDetail();
        }
      }}
    >
      <div
        class="mx-auto w-full flex h-[min(72dvh,760px)] max-w-2xl flex-col border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 {r ? 'rounded-xl overflow-hidden' : ''}"
        in:scale={{ duration: 230, easing: cubicOut, start: 0.97 }}
        out:scale={{ duration: 170, easing: cubicOut, start: 1 }}
      >
        <div class="flex items-center justify-between px-4 py-3 border-b border-slate-300 dark:border-zinc-700">
          <div class="text-sm font-bold text-slate-900 dark:text-slate-100">{activeGroup}</div>
          <div class="flex items-center gap-1.5">
            <button
              class="inline-flex items-center gap-1.5 px-2 py-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-zinc-300 text-xs font-bold leading-none {r ? 'rounded-lg' : ''}"
              onclick={() => cycleGroupSort(openedGroup)}
              title="切换排序"
            >
              {#if (groupSorts[openedGroup] || "default") === "name"}
                <ArrowUpNarrowWide size={14} />
                <span>名称排序</span>
              {:else if (groupSorts[openedGroup] || "default") === "latency"}
                <Clock3 size={14} />
                <span>延迟排序</span>
              {:else}
                <ArrowUpDown size={14} />
                <span>默认排序</span>
              {/if}
            </button>

            <button
              class="inline-flex items-center justify-center px-2 py-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-zinc-300 hover:text-slate-800 dark:hover:text-zinc-100 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors disabled:opacity-60 {r ? 'rounded-lg' : ''}"
              onclick={(event) => testGroup(event, openedGroup)}
              disabled={isTestingGroup}
              title="测速当前策略组"
            >
              <Timer size={14} class={isTestingGroup ? "animate-pulse" : ""} />
            </button>

            <button
              class="inline-flex items-center justify-center px-2 py-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-zinc-300 hover:text-slate-800 dark:hover:text-zinc-100 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors {r ? 'rounded-lg' : ''}"
              onclick={closeDetail}
              title="关闭"
            >
              <X size={14} />
            </button>
          </div>
        </div>
        <div class="p-4 overflow-y-auto grid grid-cols-1 sm:grid-cols-2 gap-2">
          {#each sortedGroupNodes(openedGroup) as node (node.key)}
            {@const group = proxies?.[openedGroup]}
            {@const selected = group?.now === node.name}
            {@const nodeOwner = `node:${openedGroup}:${node.name}`}
            {@const nodeTesting = !!testingNodes[node.name]}

            <ProxyNodeTile
              name={node.name}
              type={node.type}
              latency={node.latency}
              selected={!!selected}
              selectable={group?.type === "Selector"}
              testing={nodeTesting}
              rounded={r}
              onSelect={() => selectNode(openedGroup, node.name)}
              onTest={(event) => testSingleNode(event, nodeOwner, node.name, openedGroup)}
            />
          {/each}
        </div>
      </div>
    </div>
  {/if}

  {#if activeProvider}
    {@const openedProvider = activeProvider}
    {@const provider = providers?.[openedProvider]}
    <div
      class="fixed inset-0 z-50 bg-slate-950/55 p-3 md:p-6 flex items-center justify-center"
      role="button"
      tabindex="0"
      in:fade={{ duration: 160 }}
      out:fade={{ duration: 140 }}
      onclick={(event) => {
        if (event.target === event.currentTarget) closeDetail();
      }}
      onkeydown={(event) => {
        if (event.key === "Escape" || event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          closeDetail();
        }
      }}
    >
      <div
        class="mx-auto w-full flex h-[min(72dvh,760px)] max-w-2xl flex-col border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 {r ? 'rounded-xl overflow-hidden' : ''}"
        in:scale={{ duration: 230, easing: cubicOut, start: 0.97 }}
        out:scale={{ duration: 170, easing: cubicOut, start: 1 }}
      >
        <div class="flex items-center justify-between px-4 py-3 border-b border-slate-300 dark:border-zinc-700">
          <div class="text-sm font-bold text-slate-900 dark:text-slate-100">{activeProvider}</div>
          <button class="p-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-zinc-300 {r ? 'rounded-lg' : ''}" onclick={closeDetail}>
            <X size={14} />
          </button>
        </div>
        <div class="p-4 overflow-y-auto grid grid-cols-1 sm:grid-cols-2 gap-2">
          {#if provider?.subscriptionInfo}
            <div class="col-span-2 border border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-900 px-3 py-2 text-[11px] text-slate-600 dark:text-zinc-300 {r ? 'rounded-lg' : ''}">
              <div class="flex items-center justify-between gap-2">
                <span>已用流量</span>
                <span class="font-mono">{formatBytes((provider.subscriptionInfo.Download || 0) + (provider.subscriptionInfo.Upload || 0))} / {formatBytes(provider.subscriptionInfo.Total || 0)}</span>
              </div>
              <div class="mt-1 flex items-center justify-between gap-2">
                <span>到期时间</span>
                <span class="font-mono">{formatDate(provider.subscriptionInfo.Expire || 0)}</span>
              </div>
            </div>
          {/if}

          {#each provider?.proxies || [] as item, index (`${openedProvider}:${item.name}:${index}`)}
            {@const delay = readNodeLatency(item.name)}
            {@const nodeTesting = !!testingNodes[item.name]}

            <ProxyNodeTile name={item.name} type={item.type} latency={delay} testing={nodeTesting} rounded={r} onTest={(event) => testProviderNode(event, openedProvider, item.name)} />
          {/each}
        </div>
      </div>
    </div>
  {/if}
</main>
