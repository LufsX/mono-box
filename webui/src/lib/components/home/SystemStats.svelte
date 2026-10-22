<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { clashApi } from "$lib/api";
  import { formatBytes } from "$lib/utils";

  let memory = $state<number | null>(null);
  let uploadSpeed = $state<number | null>(null);
  let downloadSpeed = $state<number | null>(null);
  let uploadTotal = $state<number | null>(null);
  let downloadTotal = $state<number | null>(null);

  let memoryWs: WebSocket | null = null;
  let trafficWs: WebSocket | null = null;
  let reconnectTimeout: ReturnType<typeof setTimeout> | undefined;
  let connectionGeneration = 0;
  let mounted = false;

  function formatSpeed(bps: number): string {
    const kbps = bps / 1024;
    if (kbps < 1024) return `${kbps.toFixed(1)} KiB/s`;
    const mbps = kbps / 1024;
    return `${mbps.toFixed(2)} MiB/s`;
  }

  async function connectWebSockets() {
    if (!mounted) return;
    const generation = ++connectionGeneration;

    try {
      const nextMemoryWs = await clashApi.createMemoryWebSocket(
        (data) => {
          if (generation !== connectionGeneration) return;
          memory = data.inuse;
        },
        () => {
          if (generation !== connectionGeneration) return;
          console.error("Memory WebSocket error, reconnecting...");
          scheduleReconnect();
        },
      );
      nextMemoryWs.onerror = () => {
        if (generation === connectionGeneration) scheduleReconnect();
      };
      if (!mounted || generation !== connectionGeneration) {
        nextMemoryWs.close();
        return;
      }
      memoryWs = nextMemoryWs;

      memoryWs.onclose = () => {
        if (generation !== connectionGeneration) return;
        console.log("Memory WebSocket closed, reconnecting...");
        scheduleReconnect();
      };

      const nextTrafficWs = await clashApi.createTrafficWebSocket(
        (data) => {
          if (generation !== connectionGeneration) return;
          uploadSpeed = data.up;
          downloadSpeed = data.down;
          uploadTotal = data.upTotal;
          downloadTotal = data.downTotal;
        },
        () => {
          if (generation !== connectionGeneration) return;
          console.error("Traffic WebSocket error, reconnecting...");
          scheduleReconnect();
        },
      );
      nextTrafficWs.onerror = () => {
        if (generation === connectionGeneration) scheduleReconnect();
      };
      if (!mounted || generation !== connectionGeneration) {
        nextTrafficWs.close();
        return;
      }
      trafficWs = nextTrafficWs;

      trafficWs.onclose = () => {
        if (generation !== connectionGeneration) return;
        console.log("Traffic WebSocket closed, reconnecting...");
        scheduleReconnect();
      };
    } catch (e) {
      if (!mounted || generation !== connectionGeneration) return;
      console.error("Failed to connect WebSockets:", e);
      scheduleReconnect();
    }
  }

  function scheduleReconnect() {
    if (!mounted || reconnectTimeout !== undefined) return;

    reconnectTimeout = window.setTimeout(() => {
      reconnectTimeout = undefined;
      connectionGeneration += 1;
      closeWebSockets();
      void connectWebSockets();
    }, 3000);
  }

  function closeWebSockets() {
    if (memoryWs) {
      memoryWs.onclose = null;
      memoryWs.onerror = null;
      memoryWs.close();
      memoryWs = null;
    }
    if (trafficWs) {
      trafficWs.onclose = null;
      trafficWs.onerror = null;
      trafficWs.close();
      trafficWs = null;
    }
  }

  onMount(() => {
    mounted = true;
    void connectWebSockets();
  });

  onDestroy(() => {
    mounted = false;
    connectionGeneration += 1;
    if (reconnectTimeout !== undefined) {
      clearTimeout(reconnectTimeout);
      reconnectTimeout = undefined;
    }
    closeWebSockets();
  });
</script>

<section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors rounded-xl">
  <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 rounded-t-xl">
    <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">实时内核信息</h2>
  </div>
  <div class="p-4 flex flex-col gap-4">
    <!-- 实时速度 -->
    <div class="grid grid-cols-3 gap-4">
      <div class="flex flex-col">
        <span class="text-[10px] font-bold tracking-wider text-slate-400 dark:text-zinc-500">内存使用</span>
        <span class="text-sm font-mono font-bold text-slate-800 dark:text-slate-200">
          {memory !== null ? formatBytes(memory) : "-"}
        </span>
      </div>
      <div class="flex flex-col">
        <span class="text-[10px] font-bold tracking-wider text-slate-400 dark:text-zinc-500">上传速度</span>
        <span class="text-sm font-mono font-bold text-green-600 dark:text-green-400">
          {uploadSpeed !== null ? formatSpeed(uploadSpeed) : "-"}
        </span>
      </div>
      <div class="flex flex-col">
        <span class="text-[10px] font-bold tracking-wider text-slate-400 dark:text-zinc-500">下载速度</span>
        <span class="text-sm font-mono font-bold text-blue-600 dark:text-blue-400">
          {downloadSpeed !== null ? formatSpeed(downloadSpeed) : "-"}
        </span>
      </div>
    </div>

    <!-- 累计流量 -->
    <div class="h-px bg-slate-200 dark:bg-zinc-800 w-full"></div>
    <div class="grid grid-cols-2 gap-4">
      <div class="flex flex-col">
        <span class="text-[10px] font-bold tracking-wider text-slate-400 dark:text-zinc-500">累计上传</span>
        <span class="text-xs font-mono text-slate-600 dark:text-slate-400">
          {uploadTotal !== null ? formatBytes(uploadTotal) : "-"}
        </span>
      </div>
      <div class="flex flex-col">
        <span class="text-[10px] font-bold tracking-wider text-slate-400 dark:text-zinc-500">累计下载</span>
        <span class="text-xs font-mono text-slate-600 dark:text-slate-400">
          {downloadTotal !== null ? formatBytes(downloadTotal) : "-"}
        </span>
      </div>
    </div>
  </div>
</section>
