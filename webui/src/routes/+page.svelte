<script lang="ts">
  import { onMount } from "svelte";
  import { cubicOut } from "svelte/easing";
  import { fly } from "svelte/transition";
  import { dev } from "$app/environment";
  import * as actionRealApi from "$lib/api/action";
  import * as actionMockApi from "$lib/api/action.mock";
  import * as clashRealApi from "$lib/api/clash";
  import * as clashMockApi from "$lib/api/clash.mock";
  import CoreInfo from "$lib/components/CoreInfo.svelte";
  import TunControl from "$lib/components/TunControl.svelte";
  import ServiceActions from "$lib/components/ServiceActions.svelte";
  import SystemStats from "$lib/components/SystemStats.svelte";
  import ProxyMode from "$lib/components/ProxyMode.svelte";
  import LogTerminal from "$lib/components/LogTerminal.svelte";

  const clashApi = dev ? clashMockApi : clashRealApi;
  const actionApi = dev ? actionMockApi : actionRealApi;

  // Keep startup mask one-time per app runtime; returning to this route won't show it again.
  let hasShownInitialLoading = false;

  let proxyMode = $state("rule");
  let tunEnabled = $state(false);
  let coreConfig = $state<any>(null);
  let refreshing = $state(false);

  type LogType = "info" | "success" | "error" | "cmd";
  let logs = $state<{ time: string; msg: string; type: LogType }[]>([]);
  let initialLoading = $state(!dev && !hasShownInitialLoading);

  function addLog(msg: string, type: LogType = "info") {
    if (!msg) return;
    const time = new Date().toLocaleTimeString("en-US", { hour12: false });
    const lines = msg.split("\n").filter((line) => line.trim() !== "");
    lines.forEach((line) => {
      logs.push({ time, msg: line, type });
    });
    if (logs.length > 200) logs = logs.slice(logs.length - 200);
    logs = [...logs];
  }

  async function execute(actionCmd: string) {
    addLog(`> /data/adb/modules/mono_box/action.sh ${actionCmd}`, "cmd");

    try {
      const result = await actionApi.runActionScript(actionCmd);

      const raw = [result.stdout, result.stderr].filter(Boolean).join("\n").trim();
      const isError = result.errno !== 0;

      if (raw) {
        addLog(raw, isError ? "error" : "info");
      }

      return { success: !isError, errno: result.errno, raw };
    } catch (e: any) {
      const errMsg = e.message || String(e);
      addLog(errMsg, "error");
      return { success: false, errno: -1, raw: errMsg };
    }
  }

  async function fetchStatus() {
    try {
      const config = await clashApi.getConfigs();
      coreConfig = config;
      if (config.mode) proxyMode = config.mode;
      if (config.tun && typeof config.tun.enable === "boolean") {
        tunEnabled = config.tun.enable;
      }
    } catch (err) {
      console.error("Failed to fetch status:", err);
      addLog(`Failed to fetch status: ${err}`, "error");
    }
  }

  async function handleRefresh() {
    if (refreshing) return;
    refreshing = true;
    addLog("Refreshing status...", "info");
    try {
      await fetchStatus();
    } finally {
      setTimeout(() => {
        refreshing = false;
      }, 300);
    }
  }

  async function handleTunSwitch(enable: boolean | string) {
    if (enable === "upgrade") {
      await handleUpgradeCore();
      return;
    }

    addLog(`Switching TUN mode to ${enable}...`, "cmd");

    try {
      await clashApi.setTun(enable as boolean);
      tunEnabled = enable as boolean;
      addLog(`TUN mode switched to ${enable}`, "success");
      await fetchStatus();
    } catch (err: any) {
      addLog(err.message || String(err), "error");
    }
  }

  async function switchProxyMode(newMode: string) {
    addLog(`Switching proxy mode to ${newMode}...`, "cmd");

    try {
      await clashApi.setMode(newMode);
      proxyMode = newMode;
      addLog(`Proxy mode switched to ${newMode}`, "success");
      await fetchStatus();
    } catch (err: any) {
      addLog(err.message || String(err), "error");
    }
  }

  async function handleUpgradeCore() {
    addLog("Upgrading core...", "cmd");

    try {
      const result = await clashApi.upgradeCore();

      if (result && result.status === "ok") {
        addLog("更新成功！ (Update OK)", "success");
      } else if (result && result.message) {
        addLog(`更新提示: ${result.message}`, "info");
      } else {
        addLog("Core upgrade completed", "success");
      }

      setTimeout(fetchStatus, 2000);
    } catch (err: any) {
      addLog(err.message || String(err), "info");
    }
  }

  async function runAction(action: string) {
    await execute(action);

    if (["start", "restart"].includes(action)) {
      setTimeout(fetchStatus, 2000);
    }
  }

  function hideInitialLoadingWithDelay() {
    setTimeout(() => {
      initialLoading = false;
      hasShownInitialLoading = true;
    }, 200);
  }

  onMount(() => {
    (async () => {
      try {
        await actionApi.setEdgeToEdge(true);

        addLog("Initializing WebUI...", "info");
        await execute("status");
        await fetchStatus();

        hideInitialLoadingWithDelay();
      } catch (e) {
        console.error("Initialization error:", e);
        hideInitialLoadingWithDelay();
      }
    })();
  });
</script>

<main class="relative max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-6">
  <div in:fly={{ y: 14, duration: 280, delay: 20, easing: cubicOut }}>
    <TunControl bind:enabled={tunEnabled} onSwitch={handleTunSwitch} onRefresh={handleRefresh} {refreshing} />
  </div>

  <div in:fly={{ y: 14, duration: 280, delay: 60, easing: cubicOut }}>
    <ProxyMode bind:mode={proxyMode} onSwitch={switchProxyMode} />
  </div>

  <div in:fly={{ y: 14, duration: 280, delay: 140, easing: cubicOut }}>
    <SystemStats />
  </div>

  {#if coreConfig}
    <div in:fly={{ y: 14, duration: 280, delay: 100, easing: cubicOut }}>
      <CoreInfo config={coreConfig} />
    </div>
  {/if}

  <div in:fly={{ y: 14, duration: 280, delay: 180, easing: cubicOut }}>
    <ServiceActions onAction={runAction} />
  </div>

  <div in:fly={{ y: 14, duration: 280, delay: 220, easing: cubicOut }}>
    <LogTerminal bind:logs />
  </div>
</main>

{#if initialLoading}
  <div class="fixed inset-0 z-100 flex items-center justify-center bg-white/80 dark:bg-zinc-950/80 backdrop-blur-sm transition-opacity duration-300">
    <div class="text-slate-500 dark:text-zinc-400 font-mono text-sm tracking-widest animate-pulse flex flex-col items-center gap-3">
      <div class="animate-spin">⟳</div>
      LOADING CORE STATUS
    </div>
  </div>
{/if}
