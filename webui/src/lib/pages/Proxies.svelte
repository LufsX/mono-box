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
  import NoticeBanner from "$lib/components/common/NoticeBanner.svelte";
  import { loadHomeLayoutSettings } from "$lib/settings";
  import { useModalHistory } from "$lib/modal-history";
  import { formatBytes } from "$lib/utils";
  import {
    buildProxyDetailMap,
    formatProviderExpireDate as formatDate,
    getLatencyStyle,
    groupNames as resolveGroupNames,
    groupNodes as resolveGroupNodes,
    latencyBarClass,
    providerNames as resolveProviderNames,
    resolveProxyTestTarget,
    resolveProxyTestUrl,
    resolveTestUrl,
    sortedGroupNodes as resolveSortedGroupNodes,
    syncRecord,
    type ClashMode,
    type NodeSortType,
    type ProxyNode,
    type ViewType,
  } from "$lib/page-state/proxies";
  import {
    commitProxyTestResult,
    emptyProxyTestState,
    isNodeTestFailed,
    readNodeTestResult,
    readNodeTestLatency,
    seedProxyTestState,
    testTargetNames,
    type ProxyTestState,
    type ProxyTestTarget,
  } from "$lib/page-state/proxy-testing";

  let proxies = $state<ClashProxyMap | null>(null);
  let proxyDetails = $state<ClashProxyMap>({});
  let providers = $state<ClashProxyProviderMap | null>(null);
  const currentMode = stores.currentMode;
  let modeSelectValue = $state<ClashMode>("rule");
  let currentView = $state<ViewType>("proxies");
  let proxyTestUrl = $state(loadHomeLayoutSettings().proxyTestUrl);

  let loading = $state(true);
  let switchingMode = $state(false);
  let selectingGroup = $state<string | null>(null);
  let error = $state("");
  let errorReason = $state<"unauthorized" | "unreachable" | "">("");

  let groupSorts = $state<Record<string, NodeSortType>>({});
  let testState = $state<ProxyTestState>(emptyProxyTestState());
  let testingOwners = $state<Record<string, number>>({});
  let testingNodes = $state<Record<string, number>>({});
  let testingProgress = $state<Record<string, { done: number; total: number }>>({});
  let updatingProvider = $state<string | null>(null);

  let activeGroup = $state<string | null>(null);
  let activeProvider = $state<string | null>(null);

  type NodeTestResult = {
    name: string;
    status: "success" | "failed" | "error";
    delay: number;
    message?: string;
    target: ProxyTestTarget;
  };

  const modalHistory = useModalHistory("proxies", () => {
    activeGroup = null;
    activeProvider = null;
  });

  const modeOptions: { value: ClashMode; label: string }[] = [
    { value: "rule", label: "规则" },
    { value: "global", label: "全局" },
    { value: "direct", label: "直连" },
  ];

  function readNodeLatency(name: string, providerName?: string): number {
    const target = resolveProxyTestTarget(proxies, providers, name, undefined, providerName);
    return readNodeTestLatency(testState, name, target.providerName);
  }

  function nodeLatencyMap(): Record<string, number> {
    const values: Record<string, number> = {};
    for (const name of Object.keys(proxyDetails)) values[name] = readNodeLatency(name);
    return values;
  }

  function nodeFailed(name: string, providerName?: string): boolean {
    const target = resolveProxyTestTarget(proxies, providers, name, undefined, providerName);
    return isNodeTestFailed(testState, name, target.providerName);
  }

  function groupResult(groupName: string) {
    const name = proxies?.[groupName]?.now;
    const target = name ? resolveProxyTestTarget(proxies, providers, name) : undefined;
    return target ? readNodeTestResult(testState, target.name, target.providerName) : undefined;
  }

  function commitNodeResult(target: ProxyTestTarget, delay: number) {
    if (!disposed) testState = commitProxyTestResult(testState, target, delay);
  }

  function groupNodes(groupName: string): ProxyNode[] {
    return resolveGroupNodes(proxies, proxyDetails, nodeLatencyMap(), groupName);
  }

  function sortedGroupNodes(groupName: string): ProxyNode[] {
    return resolveSortedGroupNodes(proxies, proxyDetails, nodeLatencyMap(), groupSorts, groupName);
  }

  function groupNames(): string[] {
    return resolveGroupNames(proxies);
  }

  function providerNames(): string[] {
    return resolveProviderNames(providers);
  }

  function providerUsableNodes(name: string): number {
    return (providers?.[name]?.proxies || []).filter((node) => readNodeLatency(node.name, name) > 0).length;
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

  function isNodeTesting(name: string, providerName?: string): boolean {
    return testTargetNames(resolveProxyTestTarget(proxies, providers, name, undefined, providerName)).some((key) => Boolean(testingNodes[key]));
  }

  function testStart(owner: string, targets: ProxyTestTarget[]): boolean {
    const nodeKeys = [...new Set(targets.flatMap(testTargetNames))];
    if (testingOwners[owner] || nodeKeys.some((key) => testingNodes[key])) return false;

    testingOwners = { ...testingOwners, [owner]: 1 };
    testingProgress = { ...testingProgress, [owner]: { done: 0, total: Math.max(1, targets.length) } };
    const nextNodes = { ...testingNodes };
    for (const key of nodeKeys) {
      nextNodes[key] = (nextNodes[key] || 0) + 1;
    }
    testingNodes = nextNodes;
    return true;
  }

  function testEnd(owner: string, targets: ProxyTestTarget[]) {
    const nextOwners = { ...testingOwners };
    const nextProgress = { ...testingProgress };
    const nextNodes = { ...testingNodes };
    delete nextOwners[owner];
    delete nextProgress[owner];
    for (const key of [...new Set(targets.flatMap(testTargetNames))]) {
      const count = nextNodes[key] || 0;
      if (count <= 1) delete nextNodes[key];
      else nextNodes[key] = count - 1;
    }
    testingOwners = nextOwners;
    testingProgress = nextProgress;
    testingNodes = nextNodes;
  }

  function stepProgress(owner: string) {
    const current = testingProgress[owner];
    if (!current) return;
    testingProgress = {
      ...testingProgress,
      [owner]: { ...current, done: Math.min(current.total, current.done + 1) },
    };
  }

  let loadSequence = 0;
  let disposed = false;

  async function loadData(background = false) {
    const sequence = ++loadSequence;
    const revision = testState.revision;
    try {
      if (!background) { error = ""; errorReason = ""; }
      const [proxyData, providerData, configData] = await Promise.all([clashApi.getProxies(), clashApi.getProxyProviders(), clashApi.getConfigs()]);
      if (disposed || sequence !== loadSequence) return;
      proxies = syncRecord(proxies, proxyData);
      proxyDetails = syncRecord(proxyDetails, buildProxyDetailMap(proxyData, providerData));
      providers = syncRecord(providers, providerData);
      if (configData?.mode && !switchingMode) modeSelectValue = configData.mode as ClashMode;
      testState = seedProxyTestState(testState, proxyData, providerData, revision);
      proxyTestUrl = resolveProxyTestUrl(loadHomeLayoutSettings().proxyTestUrl.trim(), configData);
    } catch (e) {
      if (disposed || sequence !== loadSequence) return;
      const classified = classifyConnectionError(e);
      if (background) {
        // A refresh failure must not replace a test outcome or discard existing data.
        const message = `刷新代理信息失败: ${classified.message}`;
        if (!error.includes(message)) error = error ? `${error}\n${message}` : message;
      } else {
        errorReason = proxies ? "" : classified.reason;
        error = classified.message;
      }
    }
  }

  async function switchMode(mode: ClashMode) {
    if (mode === $currentMode || switchingMode) return;
    switchingMode = true;
    try {
      error = "";
      errorReason = "";
      await actions.switchClashMode(mode as ProxyMode);
      modeSelectValue = mode;
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      errorReason = "";
      error = `切换模式失败: ${message}`;
      modeSelectValue = $currentMode;
    } finally {
      switchingMode = false;
    }
  }

  async function selectNode(groupName: string, nodeName: string) {
    const group = proxies?.[groupName];
    if (!group || group.type !== "Selector" || group.now === nodeName || selectingGroup) return;
    selectingGroup = groupName;
    try {
      error = "";
      errorReason = "";
      await clashApi.setOutbound(groupName, nodeName);
      proxies = proxies ? { ...proxies, [groupName]: { ...group, now: nodeName } } : proxies;
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      errorReason = "";
      error = `切换策略失败: ${message}`;
    } finally {
      selectingGroup = null;
    }
  }

  async function runNodeTest(target: ProxyTestTarget): Promise<NodeTestResult> {
    if (!target.apiName) {
      return {
        name: target.name,
        status: "error",
        delay: 0,
        message: "无法确定节点的测速资源，请刷新代理信息后重试",
        target,
      };
    }

    try {
      const delay = await clashApi.testProxyDelay(target.apiName, {
        url: getTestUrl(target.contextName),
        providerName: target.providerName,
      });
      commitNodeResult(target, delay);
      return { name: target.name, status: delay > 0 ? "success" : "failed", delay, message: delay > 0 ? undefined : "测速失败", target };
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      return { name: target.name, status: "error", delay: 0, message, target };
    }
  }

  async function runTestBatch(owner: string, targets: ProxyTestTarget[]): Promise<NodeTestResult[]> {
    const results: NodeTestResult[] = [];
    let nextIndex = 0;

    async function worker() {
      while (nextIndex < targets.length) {
        const target = targets[nextIndex++];
        const result = await runNodeTest(target);
        results.push(result);
        stepProgress(owner);
      }
    }

    await Promise.all(Array.from({ length: Math.min(5, targets.length) }, () => worker()));
    return results;
  }

  function showTestSummary(label: string, results: NodeTestResult[]) {
    const failed = results.filter((result) => result.status !== "success");
    if (!failed.length) return;
    errorReason = "";
    error = `${label} ${failed.length}/${results.length}: ${failed[0].message || "测速失败"}`;
  }

  function groupTargets(groupName: string): ProxyTestTarget[] {
    return groupNodes(groupName).map((node) => resolveProxyTestTarget(proxies, providers, node.name, groupName));
  }

  function providerTargets(providerName: string): ProxyTestTarget[] {
    return (providers?.[providerName]?.proxies || []).map((node) => resolveProxyTestTarget(proxies, providers, node.name, providerName, providerName));
  }

  function targetsAreTesting(targets: ProxyTestTarget[]): boolean {
    return targets.some((target) => testTargetNames(target).some((name) => Boolean(testingNodes[name])));
  }

  function uniqueTargets(targets: ProxyTestTarget[]): ProxyTestTarget[] {
    return [...new Map(targets.map((target) => [testTargetNames(target)[0], target])).values()];
  }

  function directTestTargets(targets: ProxyTestTarget[]): ProxyTestTarget[] {
    return targets.filter((target): target is ProxyTestTarget & { apiName: string } => Boolean(target.apiName));
  }

  function groupCanTest(groupName: string): boolean {
    return directTestTargets(groupTargets(groupName)).length > 0;
  }

  function providerCanTest(providerName: string): boolean {
    return directTestTargets(providerTargets(providerName)).length > 0;
  }

  function providerIsBusy(providerName: string): boolean {
    return updatingProvider === providerName || targetsAreTesting(providerTargets(providerName));
  }

  function groupIsBusy(groupName: string): boolean {
    return Boolean(testingOwners[`group:${groupName}`]) || targetsAreTesting(groupTargets(groupName));
  }

  async function testSingleNode(event: MouseEvent, owner: string, target: ProxyTestTarget) {
    event.stopPropagation();
    if (!target.apiName) {
      errorReason = "";
      error = "无法确定节点的测速资源，请刷新代理信息后重试";
      return;
    }
    if (!testStart(owner, [target])) return;
    error = "";
    errorReason = "";
    try {
      showTestSummary("节点测速失败", await runTestBatch(owner, [target]));
    } finally {
      await loadData(true);
      testEnd(owner, [target]);
    }
  }

  async function testGroup(event: MouseEvent, groupName: string) {
    event.stopPropagation();
    const owner = `group:${groupName}`;
    const targets = uniqueTargets(groupTargets(groupName));
    if (!targets.length) return;
    if (!testStart(owner, targets)) return;

    error = "";
    errorReason = "";
    try {
      const results = await runTestBatch(owner, targets);
      showTestSummary("策略组测速失败", results);
    } finally {
      await loadData(true);
      testEnd(owner, targets);
    }
  }

  async function updateProvider(event: MouseEvent, name: string) {
    event.stopPropagation();
    if (updatingProvider || targetsAreTesting(providerTargets(name))) return;
    updatingProvider = name;
    try {
      error = "";
      errorReason = "";
      await clashApi.updateProxyProvider(name);
      await loadData();
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      errorReason = "";
      error = `更新 Provider 失败: ${message}`;
    } finally {
      updatingProvider = null;
    }
  }

  async function testProvider(event: MouseEvent, name: string) {
    event.stopPropagation();
    const owner = `provider:${name}`;
    const targets = uniqueTargets(providerTargets(name));
    if (!targets.length) return;
    if (!testStart(owner, targets)) return;
    error = "";
    errorReason = "";
    try {
      showTestSummary("Provider 测速失败", await runTestBatch(owner, targets));
    } finally {
      await loadData(true);
      testEnd(owner, targets);
    }
  }

  async function testProviderNode(event: MouseEvent, providerName: string, nodeName: string) {
    const owner = `provider-node:${providerName}:${nodeName}`;
    await testSingleNode(event, owner, resolveProxyTestTarget(proxies, providers, nodeName, providerName, providerName));
  }

  onMount(() => {
    disposed = false;
    (async () => {
      loading = true;
      try {
        await loadData();
      } finally {
        loading = false;
      }
    })();
    let polling = false;
    const timer = setInterval(async () => {
      if (polling || loading || document.hidden || Object.keys(testingOwners).length || updatingProvider || selectingGroup || switchingMode) return;
      polling = true;
      try { await loadData(true); } finally { polling = false; }
    }, 30000);
    return () => { disposed = true; loadSequence++; clearInterval(timer); };
  });

</script>

<main class="max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-4">
  {#if !loading && error && errorReason}
    <div in:fade={{ duration: 180 }}>
      <KernelAuthNotice reason={errorReason} />
    </div>
  {:else}
    <section in:fly={{ y: 10, duration: 220, easing: cubicOut }} class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 p-3 transition-colors rounded-xl">
      <div class="grid grid-cols-[minmax(0,1fr)_auto] gap-2 items-center">
        <div class="max-w-40">
          <Select id="proxy-mode" options={modeOptions} bind:value={modeSelectValue} disabled={loading || switchingMode} onchange={(value) => switchMode(value as ClashMode)} />
        </div>
        <div class="flex font-bold text-sm">
          <button
            class="px-4 py-1.5 transition-all duration-300 outline-none border -ml-px first:ml-0 rounded-l-lg {currentView === 'proxies'
              ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
              : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
            onclick={() => {
              currentView = "proxies";
            }} aria-pressed={currentView === "proxies"}>策略组</button
          >
          <button
            class="px-4 py-1.5 transition-all duration-300 outline-none border -ml-px rounded-r-lg {currentView === 'providers'
              ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
              : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
            onclick={() => {
              currentView = "providers";
            }} aria-pressed={currentView === "providers"}>代理提供商</button
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
                    {@const currentResult = groupResult(groupName)}
                    {@const nowDelay = currentResult ? currentResult.delay : readNodeLatency(groupName)}
                    {@const nowFailed = Boolean(currentResult?.failed)}
                    {@const nowStyle = nowFailed ? { text: "text-rose-600 dark:text-rose-400" } : getLatencyStyle(nowDelay)}
                    {@const ownerKey = `group:${groupName}`}
                    {@const isTestingGroup = groupIsBusy(groupName)}
                    {@const canTestGroup = groupCanTest(groupName)}
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
                          if (event.target === event.currentTarget && (event.key === "Enter" || event.key === " ")) {
                            event.preventDefault();
                            openGroup(groupName);
                          }
                        }}
                      >
                        <div class="min-w-0">
                          <div class="flex items-center gap-2 min-w-0">
                            <p class="min-w-0 break-words text-sm font-bold text-slate-900 dark:text-slate-200 pl-0.5">{groupName}</p>
                            <span class="text-[8px] border border-slate-300 dark:border-zinc-700 text-slate-500 dark:text-zinc-400 px-1.5 py-0.5 uppercase rounded-md">{group?.type}</span>
                          </div>
                          <div class="mt-1 text-xs text-slate-500 dark:text-zinc-400 min-w-0">
                            <span class="block break-words">当前: <span class="font-mono">{group?.now || "-"}</span></span>
                          </div>
                        </div>

                        <div class="flex items-center gap-1.5 shrink-0">
                          <button
                            class="inline-flex items-center justify-center px-1.5 py-0.5 min-w-14 border border-slate-300 dark:border-zinc-700 font-mono text-[10px] tabular-nums {nowStyle.text} hover:text-slate-800 dark:hover:text-zinc-100 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors disabled:opacity-60
                          rounded-lg"
                            onclick={(event) => testGroup(event, groupName)}
                            disabled={isTestingGroup || !canTestGroup}
                            title={canTestGroup ? "点击重新测速" : "未找到可测速的节点资源"}
                          >
                            {#if isTestingGroup}
                              {progress ? `${Math.round((progress.done / Math.max(progress.total, 1)) * 100)}%` : "测试中"}
                            {:else}
                              {nowFailed ? "失败" : nowDelay ? `${nowDelay} ms` : "测速"}
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
                    {@const canTestProvider = providerCanTest(name)}
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
                          if (event.target === event.currentTarget && (event.key === "Enter" || event.key === " ")) {
                            event.preventDefault();
                            openProvider(name);
                          }
                        }}
                      >
                        <div class="flex items-center justify-between gap-3">
                          <div class="min-w-0">
                            <div class="flex items-center gap-2 min-w-0">
                              <p class="min-w-0 break-words text-base font-extrabold text-slate-900 dark:text-slate-100">{name}</p>
                              <span class="text-[9px] border border-slate-300 dark:border-zinc-700 text-slate-400 dark:text-zinc-500 px-1.5 py-0.5 uppercase rounded-md">{provider?.vehicleType}</span>
                            </div>
                            <div class="mt-1 text-[11px] text-slate-400 dark:text-zinc-500">可用节点: {providerUsableNodes(name)} / {provider?.proxies?.length || 0}</div>
                          </div>

                          <div class="flex items-center gap-1.5 shrink-0 self-center">
                            <button
                              class="inline-flex items-center justify-center w-7 h-7 border border-slate-300 dark:border-zinc-700 text-slate-500 dark:text-zinc-400 hover:text-slate-800 dark:hover:text-zinc-200 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors disabled:opacity-60
                            rounded-lg"
                              onclick={(event) => testProvider(event, name)}
                              disabled={providerIsBusy(name) || !canTestProvider}
                              title={canTestProvider ? "测速 Provider" : "未找到可测速的节点资源"}
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
                              disabled={providerIsBusy(name)}
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
                                {@const delay = readNodeLatency(node.name, name)}
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
    {@const isTestingGroup = groupIsBusy(openedGroup)}
    {@const canTestGroup = groupCanTest(openedGroup)}
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
        if (event.key === "Escape") {
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
          <div class="min-w-0 break-words pr-2 text-sm font-bold text-slate-900 dark:text-slate-100">{activeGroup}</div>
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
              disabled={isTestingGroup || !canTestGroup}
              title={canTestGroup ? "测速当前策略组" : "未找到可测速的节点资源"}
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
        <div class="grid min-w-0 grid-cols-1 gap-2 overflow-y-auto p-4 sm:grid-cols-2">
          {#each sortedGroupNodes(openedGroup) as node (node.key)}
            {@const group = proxies?.[openedGroup]}
            {@const selected = group?.now === node.name}
            {@const nodeOwner = `node:${openedGroup}:${node.name}`}
            {@const nodeTesting = isNodeTesting(node.name)}
            {@const target = resolveProxyTestTarget(proxies, providers, node.name, openedGroup)}

            <div class="min-w-0" animate:flip={{ duration: 280, easing: quintOut }}>
              <ProxyNodeTile
                name={node.name}
                type={node.type}
                latency={readNodeLatency(node.name)}
                selected={!!selected}
                selectable={group?.type === "Selector" && !selectingGroup}
                testing={nodeTesting}
                failed={nodeFailed(node.name)}
                testable={Boolean(target.apiName)}
                onSelect={() => selectNode(openedGroup, node.name)}
                onTest={(event) =>
                  testSingleNode(event, nodeOwner, target)}
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
        if (event.key === "Escape") {
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
          <div class="min-w-0 break-words pr-2 text-sm font-bold text-slate-900 dark:text-slate-100">{activeProvider}</div>
          <button class="p-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-zinc-300 rounded-lg" onclick={closeDetail} aria-label="关闭">
            <X size={14} />
          </button>
        </div>
        <div class="grid min-w-0 grid-cols-1 gap-2 overflow-y-auto p-4 sm:grid-cols-2">
          {#if provider?.subscriptionInfo}
            <div class="min-w-0 border border-slate-300 bg-slate-50 px-3 py-2 text-[11px] text-slate-600 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300 sm:col-span-2 rounded-lg">
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
            {@const nodeTesting = isNodeTesting(item.name, openedProvider)}
            {@const target = resolveProxyTestTarget(proxies, providers, item.name, openedProvider, openedProvider)}

            <ProxyNodeTile
              name={item.name}
              type={item.type}
              latency={readNodeLatency(item.name, openedProvider)}
              testing={nodeTesting}
              failed={nodeFailed(item.name, openedProvider)}
              testable={Boolean(target.apiName)}
              onTest={(event) => testProviderNode(event, openedProvider, item.name)}
            />
          {/each}
        </div>
      </div>
    </div>
  {/if}
</main>

{#if error && !errorReason}
  <NoticeBanner tone="error" message={error} />
{/if}
