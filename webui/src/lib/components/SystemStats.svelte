<script lang="ts">
  import { dev } from "$app/environment";
  import { onMount, onDestroy } from "svelte";
  import * as clashRealApi from "$lib/api/clash";
  import * as clashMockApi from "$lib/api/clash.mock";

  const clashApi = dev ? clashMockApi : clashRealApi;

  let memory = $state<number | null>(null);
  let uploadSpeed = $state<number | null>(null);
  let downloadSpeed = $state<number | null>(null);
  let uploadTotal = $state<number | null>(null);
  let downloadTotal = $state<number | null>(null);

  let memoryWs: WebSocket | null = null;
  let trafficWs: WebSocket | null = null;
  let reconnectTimeout: number | null = null;

  function formatBytes(bytes: number): string {
    const kb = bytes / 1024;
    if (kb < 1024) return `${kb.toFixed(1)} KiB`;
    const mb = kb / 1024;
    if (mb < 1024) return `${mb.toFixed(1)} MiB`;
    const gb = mb / 1024;
    return `${gb.toFixed(2)} GiB`;
  }

  function formatSpeed(bps: number): string {
    const kbps = bps / 1024;
    if (kbps < 1024) return `${kbps.toFixed(1)} KiB/s`;
    const mbps = kbps / 1024;
    return `${mbps.toFixed(2)} MiB/s`;
  }

  async function connectWebSockets() {
    try {
      // 连接内存监控 WebSocket
      memoryWs = await clashApi.createMemoryWebSocket(
        (data) => {
          memory = data.inuse;
        },
        () => {
          console.error("Memory WebSocket error, reconnecting...");
          scheduleReconnect();
        },
      );

      memoryWs.onclose = () => {
        console.log("Memory WebSocket closed, reconnecting...");
        scheduleReconnect();
      };

      // 连接流量监控 WebSocket
      trafficWs = await clashApi.createTrafficWebSocket(
        (data) => {
          uploadSpeed = data.up;
          downloadSpeed = data.down;
          uploadTotal = data.upTotal;
          downloadTotal = data.downTotal;
        },
        () => {
          console.error("Traffic WebSocket error, reconnecting...");
          scheduleReconnect();
        },
      );

      trafficWs.onclose = () => {
        console.log("Traffic WebSocket closed, reconnecting...");
        scheduleReconnect();
      };
    } catch (e) {
      console.error("Failed to connect WebSockets:", e);
      scheduleReconnect();
    }
  }

  function scheduleReconnect() {
    if (reconnectTimeout) return;

    reconnectTimeout = window.setTimeout(() => {
      reconnectTimeout = null;
      closeWebSockets();
      connectWebSockets();
    }, 3000);
  }

  function closeWebSockets() {
    if (memoryWs) {
      memoryWs.close();
      memoryWs = null;
    }
    if (trafficWs) {
      trafficWs.close();
      trafficWs = null;
    }
  }

  onMount(() => {
    connectWebSockets();
  });

  onDestroy(() => {
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout);
      reconnectTimeout = null;
    }
    closeWebSockets();
  });
</script>

<section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors">
  <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950">
    <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">系统资源监控</h2>
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
    {#if uploadTotal !== null || downloadTotal !== null}
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
    {/if}
  </div>
</section>
