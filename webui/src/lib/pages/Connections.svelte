<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { useModalHistory } from "$lib/modal-history";
  import { fade, scale, slide } from "svelte/transition";
  import { flip } from "svelte/animate";
  import { cubicOut } from "svelte/easing";
  import { Activity, X, Trash2, ArrowDown, ArrowUp, Globe, ArrowUpDown, ChevronDown, ArrowUpNarrowWide, ArrowDownWideNarrow } from "@lucide/svelte";
  import { clashApi } from "$lib/api";
  import type { ClashConnection } from "$lib/api";
  import { classifyConnectionError } from "$lib/api/error-utils";
  import KernelAuthNotice from "$lib/components/shared/KernelAuthNotice.svelte";
  import { clashConnectionTagClass, formatBytes } from "$lib/utils";

  type EnrichedConnection = ClashConnection & {
    upSpeed: number;
    downSpeed: number;
  };

  type SortMode = "default" | "host-asc" | "host-desc";

  let connections = $state<EnrichedConnection[]>([]);
  let downloadTotal = $state(0);
  let uploadTotal = $state(0);
  let globalUpSpeed = $state(0);
  let globalDownSpeed = $state(0);
  let loading = $state(true);
  let error = $state("");
  let errorReason = $state<"unauthorized" | "unreachable" | "">("");
  let timer: ReturnType<typeof setInterval>;

  let prevTraffic = new Map<string, { upload: number; download: number; timestamp: number }>();
  let connectionsById = new Map<string, EnrichedConnection>();
  let defaultOrderIndex = new Map<string, number>();
  let defaultOrderCounter = 0;

  let sortMode = $state<SortMode>("default");
  let activeConnectionId = $state<string | null>(null);
  let rawOpen = $state(false);

  const modalHistory = useModalHistory("connections", () => {
    activeConnectionId = null;
  });

  function applySort() {
    if (connections.length <= 1) return;
    if (sortMode === "default") {
      connections.sort((a, b) => (defaultOrderIndex.get(a.id) ?? 0) - (defaultOrderIndex.get(b.id) ?? 0));
      return;
    }

    const dir = sortMode === "host-asc" ? 1 : -1;
    connections.sort((a, b) => {
      const aHost = (a.metadata.host || a.metadata.destinationIP || "").toLowerCase();
      const bHost = (b.metadata.host || b.metadata.destinationIP || "").toLowerCase();
      const byHost = aHost.localeCompare(bHost, "en", { sensitivity: "base" });
      if (byHost !== 0) return byHost * dir;
      return (defaultOrderIndex.get(a.id) ?? 0) - (defaultOrderIndex.get(b.id) ?? 0);
    });
  }

  function formatDuration(startTime: string): string {
    const start = new Date(startTime).getTime();
    const MathFloor = Math.floor;
    const ageSeconds = MathFloor((Date.now() - start) / 1000);
    if (ageSeconds < 60) return `${ageSeconds}s`;
    const m = MathFloor(ageSeconds / 60);
    const s = ageSeconds % 60;
    if (m < 60) return `${m}m ${s}s`;
    const h = MathFloor(m / 60);
    const m2 = m % 60;
    return `${h}h ${m2}m`;
  }

  function escapeHtml(input: string): string {
    return input.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }

  function highlightJson(value: unknown): string {
    const raw = JSON.stringify(value, null, 2) || "";
    const escaped = escapeHtml(raw);
    return escaped.replace(/("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"\s*:?)|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+\-]?\d+)?/g, (match) => {
      const isKey = match.endsWith(":");
      if (isKey) return `<span class="text-slate-800 dark:text-zinc-100 font-semibold">${match}</span>`;
      if (match === "true" || match === "false") return `<span class="text-emerald-600 dark:text-emerald-400">${match}</span>`;
      if (match === "null") return `<span class="text-rose-600 dark:text-rose-400">${match}</span>`;
      if (match.startsWith('"')) return `<span class="text-blue-700 dark:text-blue-300">${match}</span>`;
      return `<span class="text-amber-700 dark:text-amber-300">${match}</span>`;
    });
  }

  function pop(
    node: Element,
    opts: {
      delay?: number;
      duration?: number;
      easing?: (t: number) => number;
      start?: number;
      y?: number;
    } = {},
  ) {
    const { delay = 0, duration = 180, easing = cubicOut, start = 0.98, y = 4 } = opts;

    return {
      delay,
      duration,
      easing,
      css: (t: number) => {
        const scaleV = start + (1 - start) * t;
        const yV = (1 - t) * y;
        return `opacity: ${t}; transform: translate3d(0, ${yV}px, 0) scale(${scaleV});`;
      },
    };
  }

  async function fetchConnections() {
    try {
      const res = await clashApi.getConnections();
      const now = Date.now();

      const incoming = res.connections || [];
      const incomingIds = new Set(incoming.map((conn) => conn.id));

      // 移除不存在的连接
      for (let i = connections.length - 1; i >= 0; i--) {
        const id = connections[i].id;
        if (!incomingIds.has(id)) {
          connectionsById.delete(id);
          prevTraffic.delete(id);
          defaultOrderIndex.delete(id);
          connections.splice(i, 1);
        }
      }

      connectionsById.clear();
      for (const existing of connections) {
        connectionsById.set(existing.id, existing);
      }

      let currentUpSpeed = 0;
      let currentDownSpeed = 0;

      for (const conn of incoming) {
        const prev = prevTraffic.get(conn.id);
        let upSpeed = 0;
        let downSpeed = 0;

        if (prev) {
          const dt = (now - prev.timestamp) / 1000;
          if (dt > 0) {
            upSpeed = Math.max(0, (conn.upload - prev.upload) / dt);
            downSpeed = Math.max(0, (conn.download - prev.download) / dt);
          }
        }

        currentUpSpeed += upSpeed;
        currentDownSpeed += downSpeed;

        prevTraffic.set(conn.id, { upload: conn.upload, download: conn.download, timestamp: now });

        const existing = connectionsById.get(conn.id);
        if (existing) {
          existing.metadata = conn.metadata;
          existing.upload = conn.upload;
          existing.download = conn.download;
          existing.start = conn.start;
          existing.chains = conn.chains;
          existing.rule = conn.rule;
          existing.rulePayload = conn.rulePayload;
          existing.upSpeed = upSpeed;
          existing.downSpeed = downSpeed;
        } else {
          const enriched: EnrichedConnection = { ...conn, upSpeed, downSpeed };
          connections.push(enriched);
          connectionsById.set(conn.id, connections[connections.length - 1]);
          defaultOrderIndex.set(conn.id, defaultOrderCounter++);
        }
      }

      applySort();

      downloadTotal = res.downloadTotal || 0;
      uploadTotal = res.uploadTotal || 0;
      globalUpSpeed = currentUpSpeed;
      globalDownSpeed = currentDownSpeed;

      if (loading) loading = false;
      error = "";
      errorReason = "";
    } catch (err: any) {
      const classified = classifyConnectionError(err);
      errorReason = classified.reason;
      error = classified.message;
      if (loading) loading = false;
    }
  }

  async function closeConnection(id: string, event?: Event) {
    if (event) event.stopPropagation();
    try {
      await clashApi.deleteConnection(id);
      if (activeConnectionId === id) closeDetail();
      fetchConnections();
    } catch (err: any) {
      alert("Failed to close connection: " + err.message);
    }
  }

  async function closeAll() {
    try {
      await clashApi.closeAllConnections();
      if (activeConnectionId) closeDetail();
      fetchConnections();
    } catch (err: any) {
      alert("Failed to close all connections: " + err.message);
    }
  }

  function openDetail(id: string) {
    if (!activeConnectionId) modalHistory.push();
    activeConnectionId = id;
    rawOpen = false;
  }

  function closeDetail() {
    modalHistory.close();
  }

  onMount(() => {
    fetchConnections();
    timer = setInterval(fetchConnections, 1000);
  });

  onDestroy(() => {
    if (timer) clearInterval(timer);
  });
</script>

<main class="max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-4">
  <div class="flex items-center justify-between gap-3">
    <h1 class="text-xl font-bold flex items-center gap-2 text-slate-800 dark:text-slate-100 min-w-0">
      <Activity size={20} />
      <span class="truncate">活动连接 ({connections.length})</span>
    </h1>

    <div class="flex items-center gap-2 shrink-0">
      <button
        class="inline-flex items-center gap-2 h-8 px-3 py-1.5 border border-slate-300 bg-white hover:bg-slate-50 text-slate-700 dark:border-zinc-700 dark:bg-zinc-900/90 dark:hover:bg-zinc-900/70 dark:text-zinc-200 text-xs font-bold leading-none transition-colors
        rounded-lg"
        onclick={() => {
          sortMode = sortMode === "default" ? "host-asc" : sortMode === "host-asc" ? "host-desc" : "default";
          applySort();
        }}
        title="排序"
      >
        {#if sortMode === "default"}
          <ArrowUpDown size={14} class="text-slate-400 dark:text-zinc-500" />
        {:else if sortMode === "host-asc"}
          <ArrowUpNarrowWide size={14} class="text-slate-400 dark:text-zinc-500" />
        {:else}
          <ArrowDownWideNarrow size={14} class="text-slate-400 dark:text-zinc-500" />
        {/if}
        <span class="hidden sm:inline">排序 {sortMode === "default" ? "默认" : "主机"}</span>
        <span class="sm:hidden">{sortMode === "default" ? "默认" : "主机"}</span>
      </button>

      <button
        class="inline-flex items-center gap-2 h-8 px-3 py-1.5 border border-rose-200 bg-rose-50 hover:bg-rose-100 text-rose-600 dark:border-rose-900/40 dark:bg-rose-950/30 dark:hover:bg-rose-900/50 dark:text-rose-400 text-xs font-bold leading-none transition-colors disabled:opacity-60
        rounded-lg"
        onclick={closeAll}
        disabled={connections.length === 0}
        aria-label="全部关闭"
        title="全部关闭"
      >
        <Trash2 size={14} />
        <span class="hidden sm:inline">全部关闭</span>
      </button>
    </div>
  </div>

  {#if loading}
    <div class="py-12 flex items-center justify-center text-slate-500 dark:text-slate-400 text-sm">正在加载连接...</div>
  {:else if error}
    <KernelAuthNotice reason={errorReason || "unreachable"} />
  {:else}
    <div class="grid grid-cols-2 gap-3">
      <div class="border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-900/90 p-3 transition-colors rounded-xl">
        <div class="text-xs font-semibold text-slate-400 dark:text-zinc-500 mb-1 flex items-center gap-1.5">
          <ArrowUp size={14} class="text-emerald-500" />
          <span>总上传</span>
        </div>
        <div class="flex flex-col gap-1">
          <div class="text-lg font-mono text-emerald-600 dark:text-emerald-400">
            {formatBytes(globalUpSpeed)}<span class="text-xs text-slate-400 dark:text-zinc-500">/s</span>
          </div>
          <div class="text-xs font-mono text-slate-400 dark:text-zinc-500">累计: {formatBytes(uploadTotal)}</div>
        </div>
      </div>
      <div class="border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-900/90 p-3 transition-colors rounded-xl">
        <div class="text-xs font-semibold text-slate-400 dark:text-zinc-500 mb-1 flex items-center gap-1.5">
          <ArrowDown size={14} class="text-blue-500" />
          <span>总下载</span>
        </div>
        <div class="flex flex-col gap-1">
          <div class="text-lg font-mono text-blue-600 dark:text-blue-400">
            {formatBytes(globalDownSpeed)}<span class="text-xs text-slate-400 dark:text-zinc-500">/s</span>
          </div>
          <div class="text-xs font-mono text-slate-400 dark:text-zinc-500">累计: {formatBytes(downloadTotal)}</div>
        </div>
      </div>
    </div>

    <div class="space-y-3">
      {#each connections as conn (conn.id)}
        <article
          class="relative border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-900/90 transition-colors overflow-hidden will-change-transform rounded-xl"
          transition:pop={{ duration: 180 }}
          animate:flip={{ duration: 220, easing: cubicOut }}
        >
          <div
            class="w-full px-3 py-3 hover:bg-slate-50 dark:hover:bg-zinc-900/70 transition-colors text-left"
            role="button"
            tabindex="0"
            onclick={() => openDetail(conn.id)}
            onkeydown={(event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                openDetail(conn.id);
              }
            }}
          >
            <div class="grid grid-cols-[minmax(0,1fr)_auto] gap-2 items-start">
              <div class="font-mono text-sm font-bold text-slate-900 dark:text-slate-200 truncate" title={conn.metadata.host || conn.metadata.destinationIP}>
                {conn.metadata.host || conn.metadata.destinationIP}
              </div>
              <div class={clashConnectionTagClass("time", conn.start, undefined, "compact")}>
                {formatDuration(conn.start)}
              </div>
            </div>

            <div class="mt-1 grid grid-cols-[minmax(0,1fr)_auto] gap-2 items-center text-xs text-slate-500 dark:text-zinc-500">
              <div class="flex items-center gap-2 min-w-0">
                <span class={clashConnectionTagClass("network", conn.metadata.network, undefined, "compact")}>{conn.metadata.network.toUpperCase()}</span>
                <span class={clashConnectionTagClass("type", conn.metadata.type, undefined, "compact")}>{conn.metadata.type}</span>
                {#if conn.rule}
                  <span class={clashConnectionTagClass("rule", conn.rule, undefined, "compact")}>{conn.rule}</span>
                {/if}
              </div>

              <div class="font-mono text-[10px] sm:text-xs whitespace-nowrap">
                <span class="text-emerald-600 dark:text-emerald-400 font-medium">
                  <ArrowUp size={10} class="inline-block align-[-1px] text-emerald-600 dark:text-emerald-400" />
                  {formatBytes(conn.upload)}
                </span>
                <span class="text-slate-300 dark:text-zinc-600 mx-0.5">|</span>
                <span class="text-blue-600 dark:text-blue-400 font-medium">
                  <ArrowDown size={10} class="inline-block align-[-1px] text-blue-600 dark:text-blue-400" />
                  {formatBytes(conn.download)}
                </span>
              </div>
            </div>

            <div class="mt-1 grid grid-cols-[minmax(0,1fr)_auto] gap-2 items-center text-xs">
              <div class="min-w-0" title={conn.chains?.join(" > ")}>
                <span class={`${clashConnectionTagClass("chain", conn.chains?.[0] || "DIRECT", undefined, "compact")} max-w-full`}>
                  <span class="truncate">{conn.chains?.[0] || "DIRECT"}</span>
                </span>
              </div>

              <div class="flex items-center gap-2 shrink-0">
                <span class="font-mono text-[10px] sm:text-xs whitespace-nowrap text-slate-700 dark:text-zinc-200 font-semibold leading-none">
                  <ArrowUpDown size={10} class="inline-block align-[-1px] text-slate-400 dark:text-zinc-500" />
                  {formatBytes(conn.upSpeed + conn.downSpeed)}/s
                </span>
                <button
                  class="inline-flex items-center justify-center w-4 h-4 border border-rose-200 bg-rose-50 text-rose-600 hover:bg-rose-100 dark:border-rose-900/40 dark:bg-rose-950/30 dark:text-rose-400 dark:hover:bg-rose-900/50 transition-colors rounded-lg"
                  onclick={(event) => closeConnection(conn.id, event)}
                  title="关闭连接"
                >
                  <X size={10} />
                </button>
              </div>
            </div>
          </div>
        </article>
      {:else}
        <div class="flex flex-col items-center justify-center py-16 text-slate-500 dark:text-slate-400 gap-3" in:fade={{ duration: 140 }} out:fade={{ duration: 120 }}>
          <Globe size={32} class="opacity-20" />
          <span class="text-sm font-medium">暂无活动连接</span>
        </div>
      {/each}
    </div>
  {/if}

  {#if activeConnectionId && connections.find((c) => c.id === activeConnectionId)}
    {@const activeConn = connections.find((c) => c.id === activeConnectionId)!}
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
        class="mx-auto w-full flex max-h-[72dvh] max-w-2xl flex-col border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 rounded-xl overflow-hidden"
        in:scale={{ duration: 230, easing: cubicOut, start: 0.97 }}
        out:scale={{ duration: 170, easing: cubicOut, start: 1 }}
      >
        <div class="flex items-center justify-between px-4 py-3 border-b border-slate-300 dark:border-zinc-700 shrink-0">
          <div class="text-sm font-bold text-slate-900 dark:text-slate-100 truncate pr-4">连接详情</div>
          <button class="p-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-zinc-300 rounded-lg" onclick={closeDetail}>
            <X size={14} />
          </button>
        </div>
        <div class="p-4 overflow-y-auto flex-1 flex flex-col gap-4">
          <div class="flex flex-col gap-2">
            <div class="text-lg font-mono font-bold text-slate-800 dark:text-slate-200 break-all select-text">
              {activeConn.metadata.host || activeConn.metadata.destinationIP}
            </div>
            <div class="flex gap-2 items-center flex-wrap">
              <span class={clashConnectionTagClass("network", activeConn.metadata.network, undefined, "normal")}>{activeConn.metadata.network.toUpperCase()}</span>
              <span class={clashConnectionTagClass("type", activeConn.metadata.type, undefined, "normal")}>{activeConn.metadata.type}</span>
              {#if activeConn.rule}
                <span class={clashConnectionTagClass("rule", activeConn.rule, undefined, "normal")}>{activeConn.rule} {activeConn.rulePayload ? `(${activeConn.rulePayload})` : ""}</span>
              {/if}
            </div>
          </div>

          {#if activeConn.chains && activeConn.chains.length > 0}
            <div>
              <span class="text-xs font-bold uppercase tracking-wider text-slate-500 dark:text-zinc-400">代理链</span>
              <div class="mt-1.5 bg-slate-50 dark:bg-zinc-900/50 p-2.5 border border-slate-200 dark:border-zinc-800 rounded-lg">
                <div class="flex flex-wrap items-center gap-x-2 gap-y-2">
                  {#each activeConn.chains as chain, i (i)}
                    <div class="flex items-center gap-2">
                      <span class={`${clashConnectionTagClass("chain", chain, undefined, "normal")} gap-2`}>
                        <span class="text-slate-400 dark:text-zinc-500">{(i + 1).toString().padStart(2, "0")}</span>
                        <span class="break-all">{chain}</span>
                      </span>

                      {#if i < activeConn.chains.length - 1}
                        <span class="text-slate-300 dark:text-zinc-600 font-mono">→</span>
                      {/if}
                    </div>
                  {/each}
                </div>
              </div>
            </div>
          {/if}

          <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div class="border border-slate-200 dark:border-zinc-800 p-3 bg-slate-50 dark:bg-zinc-900/50 rounded-lg">
              <div class="text-[10px] font-bold uppercase tracking-wider text-slate-500 dark:text-zinc-400">源地址</div>
              <div class="font-mono text-sm mt-1 whitespace-nowrap">
                <span class="text-slate-700 dark:text-zinc-200">{activeConn.metadata.sourceIP}</span><span class="text-slate-400 dark:text-zinc-600">:</span><span
                  class="text-amber-700 dark:text-amber-300">{activeConn.metadata.sourcePort}</span
                >
              </div>
            </div>
            <div class="border border-slate-200 dark:border-zinc-800 p-3 bg-slate-50 dark:bg-zinc-900/50 rounded-lg">
              <div class="text-[10px] font-bold uppercase tracking-wider text-slate-500 dark:text-zinc-400">目标地址</div>
              <div class="font-mono text-sm mt-1 whitespace-nowrap">
                <span class="text-slate-700 dark:text-zinc-200">{activeConn.metadata.destinationIP}</span><span class="text-slate-400 dark:text-zinc-600">:</span><span
                  class="text-amber-700 dark:text-amber-300">{activeConn.metadata.destinationPort}</span
                >
              </div>
            </div>
          </div>

          <div class="grid grid-cols-2 gap-3 mt-1">
            <div class="flex flex-col gap-1 border border-emerald-100 dark:border-emerald-900/30 bg-emerald-50/50 dark:bg-emerald-950/20 p-2.5 rounded-lg">
              <span class="text-[10px] font-bold text-emerald-600 dark:text-emerald-500">上传</span>
              <div class="font-mono text-sm text-emerald-700 dark:text-emerald-400 flex flex-col">
                <span>{formatBytes(activeConn.upSpeed)}/s</span>
                <span class="text-[10px] opacity-70 mt-0.5">累计: {formatBytes(activeConn.upload)}</span>
              </div>
            </div>
            <div class="flex flex-col gap-1 border border-blue-100 dark:border-blue-900/30 bg-blue-50/50 dark:bg-blue-950/20 p-2.5 rounded-lg">
              <span class="text-[10px] font-bold text-blue-600 dark:text-blue-500">下载</span>
              <div class="font-mono text-sm text-blue-700 dark:text-blue-400 flex flex-col">
                <span>{formatBytes(activeConn.downSpeed)}/s</span>
                <span class="text-[10px] opacity-70 mt-0.5">累计: {formatBytes(activeConn.download)}</span>
              </div>
            </div>
          </div>

          <div class="flex flex-col gap-1 text-xs text-slate-500 dark:text-zinc-400 mt-2">
            <div><span class="font-semibold">连接时长:</span> {formatDuration(activeConn.start)}</div>
            <div><span class="font-semibold">连接ID:</span> <span class="font-mono">{activeConn.id}</span></div>
          </div>

          <div class="border border-slate-200 dark:border-zinc-800 bg-slate-50 dark:bg-zinc-900/50 px-3 py-2 text-[11px] text-slate-600 dark:text-zinc-300 rounded-lg">
            <button type="button" class="w-full flex items-center justify-between gap-2 text-left cursor-pointer select-none font-bold" aria-expanded={rawOpen} onclick={() => (rawOpen = !rawOpen)}>
              <span>原始数据</span>
              <ChevronDown size={14} class="shrink-0 text-slate-400 dark:text-zinc-500 transition-transform duration-150 {rawOpen ? 'rotate-180' : ''}" />
            </button>
            {#if rawOpen}
              <div transition:slide={{ duration: 160, easing: cubicOut }}>
                <pre class="mt-2 overflow-auto whitespace-pre font-mono text-[10px] text-slate-700 dark:text-zinc-200">{@html highlightJson(activeConn)}</pre>
              </div>
            {/if}
          </div>
        </div>

        <div class="px-4 py-3 border-t border-slate-200 dark:border-zinc-800 bg-slate-50/50 dark:bg-zinc-900/20 flex justify-end shrink-0">
          <button
            class="flex items-center justify-center gap-2 px-4 py-2 border border-rose-200 bg-rose-50 hover:bg-rose-100 text-rose-600 dark:border-rose-900/40 dark:bg-rose-950/30 dark:hover:bg-rose-900/50 dark:text-rose-400 font-medium transition-colors w-full sm:w-auto
            rounded-lg"
            onclick={() => closeConnection(activeConn.id)}
          >
            <Trash2 size={16} />
            <span>断开此连接</span>
          </button>
        </div>
      </div>
    </div>
  {/if}
</main>
