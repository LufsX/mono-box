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
  import { ExternalLink } from "@lucide/svelte";
  import { loadHomeLayoutSettings, type HomeModuleId } from "$lib/settings";

  const clashApi = dev ? clashMockApi : clashRealApi;
  const actionApi = dev ? actionMockApi : actionRealApi;

  let proxyMode = $state("rule");
  let tunEnabled = $state(false);
  let coreConfig = $state<any>(null);
  let refreshing = $state(false);
  let homeLayout = $state(loadHomeLayoutSettings());
  let defaultPanelUrl = $state("http://127.0.0.1:9090/ui");

  type LogType = "info" | "success" | "error" | "cmd";
  let logs = $state<{ time: string; msg: string; type: LogType }[]>([]);

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

  async function loadPanelDefaultUrl() {
    try {
      const content = await clashApi.readBoxConfig();
      const parsed = clashApi.parseBoxConfig(content);
      defaultPanelUrl = `http://127.0.0.1:${parsed.clashApiPort}/ui`;
    } catch {
      defaultPanelUrl = "http://127.0.0.1:9090/ui";
    }
  }

  function isModuleVisible(moduleId: HomeModuleId): boolean {
    return !homeLayout.hiddenModules.includes(moduleId);
  }

  function getPanelUrl(): string {
    return homeLayout.panelUrl.trim() || defaultPanelUrl;
  }

  async function openPanel() {
    const url = getPanelUrl();
    addLog(`Opening panel: ${url}`, "cmd");
    try {
      await actionApi.openExternalUrl(url);
    } catch (e: any) {
      addLog(e.message || String(e), "error");
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

  onMount(() => {
    (async () => {
      try {
        homeLayout = loadHomeLayoutSettings();
        await loadPanelDefaultUrl();

        addLog("Initializing WebUI...", "info");
        await execute("status");
        await fetchStatus();
      } catch (e) {
        console.error("Initialization error:", e);
      }
    })();
  });
</script>

<main class="relative max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-6">
  {#each homeLayout.moduleOrder as moduleId, index (moduleId)}
    {#if moduleId === "tun" && isModuleVisible("tun")}
      <div in:fly={{ y: 20, duration: 300, delay: index * 40, easing: cubicOut }} out:fly={{ y: -10, duration: 200, easing: cubicOut }}>
        <TunControl bind:enabled={tunEnabled} onSwitch={handleTunSwitch} onRefresh={handleRefresh} {refreshing} />
      </div>
    {/if}

    {#if moduleId === "proxy" && isModuleVisible("proxy")}
      <div in:fly={{ y: 20, duration: 300, delay: index * 40, easing: cubicOut }} out:fly={{ y: -10, duration: 200, easing: cubicOut }}>
        <ProxyMode bind:mode={proxyMode} onSwitch={switchProxyMode} />
      </div>
    {/if}

    {#if moduleId === "stats" && isModuleVisible("stats")}
      <div in:fly={{ y: 20, duration: 300, delay: index * 40, easing: cubicOut }} out:fly={{ y: -10, duration: 200, easing: cubicOut }}>
        <SystemStats />
      </div>
    {/if}

    {#if moduleId === "service" && isModuleVisible("service")}
      <div in:fly={{ y: 20, duration: 300, delay: index * 40, easing: cubicOut }} out:fly={{ y: -10, duration: 200, easing: cubicOut }}>
        <ServiceActions onAction={runAction} />
      </div>
    {/if}

    {#if moduleId === "panel" && isModuleVisible("panel")}
      <div in:fly={{ y: 20, duration: 300, delay: index * 40, easing: cubicOut }} out:fly={{ y: -10, duration: 200, easing: cubicOut }}>
        <section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors">
          <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950">
            <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">跳转内核面板</h2>
          </div>
          <div class="p-4">
            <button
              class="w-full inline-flex items-center justify-center gap-2 border border-slate-300 dark:border-zinc-700 py-3 text-sm font-bold text-slate-800 dark:text-slate-200 hover:bg-slate-100 dark:hover:bg-zinc-800 active:bg-slate-200 dark:active:bg-zinc-700 transition-colors outline-none"
              onclick={openPanel}
            >
              <ExternalLink size={16} />
              打开内核面板
            </button>
            <div class="mt-2 text-xs text-slate-500 dark:text-slate-400 break-all">{getPanelUrl()}</div>
          </div>
        </section>
      </div>
    {/if}

    {#if moduleId === "core" && isModuleVisible("core") && coreConfig}
      <div in:fly={{ y: 20, duration: 300, delay: index * 40, easing: cubicOut }} out:fly={{ y: -10, duration: 200, easing: cubicOut }}>
        <CoreInfo config={coreConfig} />
      </div>
    {/if}

    {#if moduleId === "log" && isModuleVisible("log")}
      <div in:fly={{ y: 20, duration: 300, delay: index * 40, easing: cubicOut }} out:fly={{ y: -10, duration: 200, easing: cubicOut }}>
        <LogTerminal bind:logs />
      </div>
    {/if}
  {/each}
</main>
