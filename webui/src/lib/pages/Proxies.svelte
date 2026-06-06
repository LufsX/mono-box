<script lang="ts">
  import { onMount } from "svelte";
  import { fade, fly, scale } from "svelte/transition";
  import { cubicOut, quintOut } from "svelte/easing";
  import { flip } from "svelte/animate";
  import { Activity, ArrowUpDown, ChevronRight, Clock3, RefreshCw, ArrowUpNarrowWide, Timer, X } from "@lucide/svelte";
  import Select from "$lib/components/common/Select.svelte";
  import ProxyNodeTile from "$lib/components/shared/ProxyNodeTile.svelte";
  import { clashApi, stores, actions } from "$lib/api";
  import type { ClashProxyMap, ClashProxyProviderMap, ProxyMode } from "$lib/api";
  import { classifyConnectionError } from "$lib/api/error-utils";
  import KernelAuthNotice from "$lib/components/shared/KernelAuthNotice.svelte";
  import { loadHomeLayoutSettings } from "$lib/settings";
  import { useModalHistory } from "$lib/modal-history";
  import { formatBytes } from "$lib/utils";
  import {
    deriveLatencyMap,
    formatProviderExpireDate as formatDate,
    getLatencyStyle,
    groupNames as resolveGroupNames,
    groupNodes as resolveGroupNodes,
    latencyBarClass,
    providerNames as resolveProviderNames,
    providerUsableNodes as resolveProviderUsableNodes,
    readNodeLatency as resolveNodeLatency,
    resolveProxyTestUrl,
    resolveTestUrl,
    sortedGroupNodes as resolveSortedGroupNodes,
    syncRecord,
    type ClashMode,
    type NodeSortType,
    type ProxyNode,
    type ViewType,
  } from "$lib/page-state/proxies";

  let proxies = $state<ClashProxyMap | null>(null);
  let providers = $state<ClashProxyProviderMap | null>(null);
  const currentMode = stores.currentMode;
  let modeSelectValue = $state<ClashMode>("rule");
  let currentView = $state<ViewType>("proxies");
  let proxyTestUrl = $state(loadHomeLayoutSettings().proxyTestUrl);

  let loading = $state(true);
  let error = $state("");
  let errorReason = $state<"unauthorized" | "unreachable" | "">("");

  let groupSorts = $state<Record<string, NodeSortType>>({});
  let latencies = $state<Record<string, number>>({});
  let testingOwners = $state<Record<string, number>>({});
  let testingNodes = $state<Record<string, number>>({});
  let testingProgress = $state<Record<string, { done: number; total: number }>>({});
  let updatingProvider = $state<string | null>(null);

  let activeGroup = $state<string | null>(null);
  let activeProvider = $state<string | null>(null);

  const modalHistory = useModalHistory("proxies", () => {
    activeGroup = null;
    activeProvider = null;
  });

  const modeOptions: { value: ClashMode; label: string }[] = [
    { value: "rule", label: "规则" },
    { value: "global", label: "全局" },
    { value: "direct", label: "直连" },
  ];

  function readNodeLatency(name: string): number {
    return resolveNodeLatency(latencies, name);
  }

  function groupNodes(groupName: string): ProxyNode[] {
    return resolveGroupNodes(proxies, latencies, groupName);
  }

  function sortedGroupNodes(groupName: string): ProxyNode[] {
    return resolveSortedGroupNodes(proxies, latencies, groupSorts, groupName);
  }

  function groupNames(): string[] {
    return resolveGroupNames(proxies);
  }

  function providerNames(): string[] {
    return resolveProviderNames(providers);
  }

  function providerUsableNodes(name: string): number {
    return resolveProviderUsableNodes(providers, latencies, name);
  }

  function getTestUrl(groupName?: string): string {
    return resolveTestUrl(proxies, providers, proxyTestUrl, groupName);
  }

  function openGroup(groupName: string) {
    if (!activeGroup && !activeProvider) modalHistory.push();
    activeProvider = null;
    activeGroup = groupName;
  }

  function openProvider(name: string) {
    if (!activeGroup && !activeProvider) modalHistory.push();
    activeGroup = null;
    activeProvider = name;
  }

  function closeDetail() {
    modalHistory.close();
  }

  function cycleGroupSort(groupName: string) {
    const orders: NodeSortType[] = ["default", "latency", "name"];
    const current = groupSorts[groupName] || "default";
    groupSorts[groupName] = orders[(orders.indexOf(current) + 1) % orders.length];
  }

  function testStart(owner: string, nodes: string[]) {
    testingOwners[owner] = (testingOwners[owner] || 0) + 1;
    testingProgress[owner] = { done: 0, total: Math.max(1, nodes.length) };
    if (!nodes.length) return;
    for (const node of nodes) {
      if (!node) continue;
      testingNodes[node] = (testingNodes[node] || 0) + 1;
    }
  }

  function testEnd(owner: string, nodes: string[]) {
    const ownerCount = testingOwners[owner] || 0;
    if (ownerCount <= 1) {
      delete testingOwners[owner];
    } else {
      testingOwners[owner] = ownerCount - 1;
    }
    if (!nodes.length) return;

    delete testingProgress[owner];

    for (const node of nodes) {
      const count = testingNodes[node] || 0;
      if (count <= 1) delete testingNodes[node];
      else testingNodes[node] = count - 1;
    }
  }

  function stepProgress(owner: string) {
    const current = testingProgress[owner];
    if (!current) return;
    const done = Math.min(current.total, current.done + 1);
    current.done = done;
  }

  async function loadData() {
    try {
      error = "";
      errorReason = "";
      const [proxyData, providerData, configData] = await Promise.all([clashApi.getProxies(), clashApi.getProxyProviders(), clashApi.getConfigs()]);

      proxies = syncRecord(proxies, proxyData);
      providers = syncRecord(providers, providerData);
      if (configData?.mode) {
        modeSelectValue = configData.mode as ClashMode;
      }

      const nextLatency = deriveLatencyMap(proxyData);
      for (const key of Object.keys(latencies)) {
        delete latencies[key];
      }
      for (const [name, delay] of Object.entries(nextLatency)) {
        latencies[name] = delay;
      }
      const savedTestUrl = loadHomeLayoutSettings().proxyTestUrl.trim();
      proxyTestUrl = resolveProxyTestUrl(savedTestUrl, configData);
    } catch (e) {
      const classified = classifyConnectionError(e);
      errorReason = classified.reason;
      error = classified.message;
      proxies = null;
      providers = null;
    }
  }

  async function switchMode(mode: ClashMode) {
    if (mode === $currentMode) return;
    try {
      await actions.switchClashMode(mode as ProxyMode);
      modeSelectValue = mode;
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      error = `切换模式失败: ${message}`;
      modeSelectValue = $currentMode;
    }
  }

  async function selectNode(groupName: string, nodeName: string) {
    const group = proxies?.[groupName];
    if (!group || group.type !== "Selector" || group.now === nodeName) return;
    try {
      await clashApi.setOutbound(groupName, nodeName);
      if (proxies?.[groupName]) {
        proxies[groupName].now = nodeName;
      }
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
      latencies[nodeName] = delay;
    } catch {
      latencies[nodeName] = 0;
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
      for (const [name, delay] of entries) latencies[name] = delay;
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
        current.done = current.total;
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
      latencies[nodeName] = delay;
    } catch {
      latencies[nodeName] = 0;
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
  });

  $effect(() => {
    if (modeSelectValue !== $currentMode) {
      void switchMode(modeSelectValue);
    }
  });
</script>

<main class="max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-4">
  {#if !loading && error}
    <div in:fade={{ duration: 180 }}>
      <KernelAuthNotice reason={errorReason || "unreachable"} />
    </div>
  {:else}
    <section in:fly={{ y: 10, duration: 220, easing: cubicOut }} class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 p-3 transition-colors rounded-xl">
      <div class="grid grid-cols-[minmax(0,1fr)_auto] gap-2 items-center">
        <div class="max-w-40">
          <Select id="proxy-mode" options={modeOptions} bind:value={modeSelectValue} />
        </div>
        <div class="flex font-bold text-sm">
          <button
            class="px-4 py-1.5 transition-all duration-300 outline-none border -ml-px first:ml-0 rounded-l-lg {currentView === 'proxies'
              ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
              : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
            onclick={() => {
              currentView = "proxies";
            }}>策略组</button
          >
          <button
            class="px-4 py-1.5 transition-all duration-300 outline-none border -ml-px rounded-r-lg {currentView === 'providers'
              ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
              : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
            onclick={() => {
              currentView = "providers";
            }}>代理提供商</button
          >
        </div>
      </div>
    </section>

    <section in:fly={{ y: 12, duration: 260, easing: cubicOut }} class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors rounded-xl">
      <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 flex items-center justify-between gap-3 rounded-t-xl">
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

                    <article
                      class="relative border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-900/50 transition-colors overflow-hidden rounded-xl"
                      animate:flip={{ duration: 280, easing: quintOut }}
                    >
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
                            <span class="text-[8px] border border-slate-300 dark:border-zinc-700 text-slate-500 dark:text-zinc-400 px-1.5 py-0.5 uppercase rounded-md">{group?.type}</span>
                          </div>
                          <div class="mt-1 text-xs text-slate-500 dark:text-zinc-400 min-w-0">
                            <span class="truncate block">当前: <span class="font-mono">{group?.now || "-"}</span></span>
                          </div>
                        </div>

                        <div class="flex items-center gap-1.5 shrink-0">
                          <button
                            class="inline-flex items-center justify-center px-1.5 py-0.5 min-w-14 border border-slate-300 dark:border-zinc-700 font-mono text-[10px] tabular-nums {nowStyle.text} hover:text-slate-800 dark:hover:text-zinc-100 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors disabled:opacity-60
                          rounded-lg"
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

                    <article
                      class="border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-900/50 transition-colors overflow-hidden rounded-xl"
                      animate:flip={{ duration: 280, easing: quintOut }}
                    >
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
                              <span class="text-[9px] border border-slate-300 dark:border-zinc-700 text-slate-400 dark:text-zinc-500 px-1.5 py-0.5 uppercase rounded-md">{provider?.vehicleType}</span>
                            </div>
                            <div class="mt-1 text-[11px] text-slate-400 dark:text-zinc-500">可用节点: {providerUsableNodes(name)} / {provider?.proxies?.length || 0}</div>
                          </div>

                          <div class="flex items-center gap-1.5 shrink-0 self-center">
                            <button
                              class="inline-flex items-center justify-center w-7 h-7 border border-slate-300 dark:border-zinc-700 text-slate-500 dark:text-zinc-400 hover:text-slate-800 dark:hover:text-zinc-200 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors disabled:opacity-60
                            rounded-lg"
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
                            rounded-lg"
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

                        <div class="mt-2 h-2 w-full border border-slate-300 dark:border-zinc-700 overflow-hidden rounded-lg">
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
  {/if}

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
        class="mx-auto w-full flex h-[min(72dvh,760px)] max-w-2xl flex-col border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 rounded-xl overflow-hidden"
        in:scale={{ duration: 230, easing: cubicOut, start: 0.97 }}
        out:scale={{ duration: 170, easing: cubicOut, start: 1 }}
      >
        <div class="flex items-center justify-between px-4 py-3 border-b border-slate-300 dark:border-zinc-700">
          <div class="text-sm font-bold text-slate-900 dark:text-slate-100">{activeGroup}</div>
          <div class="flex items-center gap-1.5">
            <button
              class="inline-flex items-center gap-1.5 px-2 py-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-zinc-300 text-xs font-bold leading-none rounded-lg"
              onclick={() => cycleGroupSort(openedGroup)}
              title="切换排序"
            >
              {#if (groupSorts[openedGroup] || "default") === "name"}
                <ArrowUpNarrowWide size={14} />
                <span>名称</span>
              {:else if (groupSorts[openedGroup] || "default") === "latency"}
                <Clock3 size={14} />
                <span>延迟</span>
              {:else}
                <ArrowUpDown size={14} />
                <span>默认</span>
              {/if}
            </button>

            <button
              class="inline-flex items-center justify-center px-2 py-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-zinc-300 hover:text-slate-800 dark:hover:text-zinc-100 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors disabled:opacity-60
              rounded-lg"
              onclick={(event) => testGroup(event, openedGroup)}
              disabled={isTestingGroup}
              title="测速当前策略组"
            >
              <Timer size={14} class={isTestingGroup ? "animate-pulse" : ""} />
            </button>

            <button
              class="inline-flex items-center justify-center px-2 py-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-zinc-300 hover:text-slate-800 dark:hover:text-zinc-100 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors
              rounded-lg"
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

            <div animate:flip={{ duration: 280, easing: quintOut }}>
              <ProxyNodeTile
                name={node.name}
                type={node.type}
                latency={node.latency}
                selected={!!selected}
                selectable={group?.type === "Selector"}
                testing={nodeTesting}
                onSelect={() => selectNode(openedGroup, node.name)}
                onTest={(event) => testSingleNode(event, nodeOwner, node.name, openedGroup)}
              />
            </div>
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
        class="mx-auto w-full flex h-[min(72dvh,760px)] max-w-2xl flex-col border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 rounded-xl overflow-hidden"
        in:scale={{ duration: 230, easing: cubicOut, start: 0.97 }}
        out:scale={{ duration: 170, easing: cubicOut, start: 1 }}
      >
        <div class="flex items-center justify-between px-4 py-3 border-b border-slate-300 dark:border-zinc-700">
          <div class="text-sm font-bold text-slate-900 dark:text-slate-100">{activeProvider}</div>
          <button class="p-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-zinc-300 rounded-lg" onclick={closeDetail}>
            <X size={14} />
          </button>
        </div>
        <div class="p-4 overflow-y-auto grid grid-cols-1 sm:grid-cols-2 gap-2">
          {#if provider?.subscriptionInfo}
            <div class="col-span-2 border border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-900 px-3 py-2 text-[11px] text-slate-600 dark:text-zinc-300 rounded-lg">
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

            <ProxyNodeTile name={item.name} type={item.type} latency={delay} testing={nodeTesting} onTest={(event) => testProviderNode(event, openedProvider, item.name)} />
          {/each}
        </div>
      </div>
    </div>
  {/if}
</main>
