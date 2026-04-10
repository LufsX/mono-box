<script lang="ts">
  import { onMount, tick } from "svelte";
  import { cubicOut } from "svelte/easing";
  import { flip } from "svelte/animate";
  import { fly } from "svelte/transition";
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
  import KernelAuthNotice from "$lib/components/KernelAuthNotice.svelte";
  import { ExternalLink } from "@lucide/svelte";
  import { loadHomeLayoutSettings, roundedStore, type HomeModuleId } from "$lib/settings";

  const isProd = import.meta.env.MODE !== "production";
  const clashApi = isProd ? clashMockApi : clashRealApi;
  const r = $derived($roundedStore);
  const actionApi = isProd ? actionMockApi : actionRealApi;

  let proxyMode = $state("rule");
  let tunEnabled = $state(false);
  let coreConfig = $state<any>(null);
  let coreVersion = $state("-");
  let coreApiError = $state("");
  let coreApiConnected = $state<boolean | null>(null);
  let startingFromNotice = $state(false);
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

  function moduleEnter(index: number) {
    return {
      y: 14,
      duration: 300,
      delay: Math.min(index * 22, 140),
      easing: cubicOut,
    };
  }

  function setCoreApiFailure(message: string) {
    coreVersion = "-";
    coreApiError = message;
    coreApiConnected = false;
  }

  async function execute(actionCmd: string) {
    addLog(`> /data/adb/modules/mono_box/action.sh ${actionCmd}`, "cmd");
    addLog("[Executing] Waiting for KernelSU to return results...", "info");

    await tick();
    await new Promise((resolve) => setTimeout(resolve, 10));
    const startedAt = Date.now();

    try {
      const result = await actionApi.runActionScript(actionCmd);
      const elapsedMs = Date.now() - startedAt;

      const raw = [result.stdout, result.stderr].filter(Boolean).join("\n").trim();
      const isError = result.errno !== 0;

      if (raw) {
        addLog(`[Action] ${raw}`, isError ? "error" : "info");
      }
      addLog(`[Info] Execution time: ${elapsedMs} ms`, "info");

      return { success: !isError, errno: result.errno, raw };
    } catch (e: any) {
      const elapsedMs = Date.now() - startedAt;
      const errMsg = e.message || String(e);
      addLog(errMsg, "error");
      addLog(`[Info] Execution time: ${elapsedMs} ms`, "info");
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
      addLog(`[Core] Failed to fetch status: ${err}`, "error");
      coreConfig = null;
    }

    try {
      const versionResult = await clashApi.checkVersion();
      if (versionResult.ok) {
        coreVersion = versionResult.version;
        coreApiError = "";
        coreApiConnected = true;
      } else if (versionResult.reason === "unauthorized") {
        setCoreApiFailure("Clash API 认证失败，请前往设置检查 Clash API Port 与 Secret");
      } else {
        setCoreApiFailure("未连接到内核，内核可能未运行，请尝试启动服务");
      }
    } catch (err) {
      setCoreApiFailure("未连接到内核，未知错误：" + (err instanceof Error ? err.message : String(err)));
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

  function isModuleRenderable(moduleId: HomeModuleId): boolean {
    if (!isModuleVisible(moduleId)) return false;

    // modules that rely on core status
    if (["tun", "proxy", "stats", "core"].includes(moduleId)) {
      return coreApiConnected !== false;
    }

    return true;
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
    addLog("[Core] Refreshing status...", "info");
    try {
      await fetchStatus();
      addLog("[Core] Status refreshed", "success");
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

    addLog(`> Switching TUN mode to ${enable}...`, "cmd");

    try {
      await clashApi.setTun(enable as boolean);
      tunEnabled = enable as boolean;
      addLog(`[Core] TUN mode switched to ${enable}`, "success");
      await fetchStatus();
    } catch (err: any) {
      addLog(err.message || String(err), "error");
    }
  }

  async function switchProxyMode(newMode: string) {
    addLog(`> Switching proxy mode to ${newMode}...`, "cmd");

    try {
      await clashApi.setMode(newMode);
      proxyMode = newMode;
      addLog(`[Core] Proxy mode switched to ${newMode}`, "success");
      await fetchStatus();
    } catch (err: any) {
      addLog(err.message || String(err), "error");
    }
  }

  async function handleUpgradeCore() {
    addLog("> Upgrading core...", "cmd");

    try {
      const result = await clashApi.upgradeCore();

      if (result && result.status === "ok") {
        addLog("[Core] Update OK", "success");
      } else if (result && result.message) {
        addLog(`[Core] Result: ${result.message}`, "info");
      } else {
        addLog("[Core] Unknown result: " + JSON.stringify(result), "success");
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

  async function handleStartFromNotice() {
    if (startingFromNotice) return;
    startingFromNotice = true;
    try {
      await runAction("start");
      await handleRefresh();
    } finally {
      startingFromNotice = false;
    }
  }

  onMount(() => {
    (async () => {
      try {
        addLog("[WebUI] Initializing WebUI...", "info");
        const startTime = Date.now();
        homeLayout = loadHomeLayoutSettings();
        await loadPanelDefaultUrl();

        await fetchStatus();
        const elapsedMs = Date.now() - startTime;
        addLog(`[WebUI] WebUI initialized with ${elapsedMs} ms`, "success");
      } catch (e) {
        console.error("Initialization error:", e);
      }
    })();
  });
</script>

<main class="relative max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-6">
  {#if coreApiConnected === false}
    <div in:fly={{ y: 16, duration: 260, easing: cubicOut }}>
      <KernelAuthNotice message={coreApiError} onRetry={handleRefresh} retrying={refreshing} onStart={handleStartFromNotice} starting={startingFromNotice} />
    </div>
  {/if}

  {#each homeLayout.moduleOrder.filter(isModuleRenderable) as moduleId, index (moduleId)}
    <div in:fly={moduleEnter(index)} animate:flip={{ duration: 180, easing: cubicOut }}>
      {#if moduleId === "tun"}
        <TunControl bind:enabled={tunEnabled} onSwitch={handleTunSwitch} onRefresh={handleRefresh} {refreshing} />
      {:else if moduleId === "proxy"}
        <ProxyMode bind:mode={proxyMode} onSwitch={switchProxyMode} />
      {:else if moduleId === "stats"}
        <SystemStats />
      {:else if moduleId === "service"}
        <ServiceActions onAction={runAction} />
      {:else if moduleId === "panel"}
        <section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors {r ? 'rounded-xl' : ''}">
          <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 {r ? 'rounded-t-xl' : ''}">
            <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">面板快捷跳转</h2>
          </div>
          <div class="p-4">
            <button
              class="w-full inline-flex items-center justify-center gap-2 border border-slate-300 dark:border-zinc-700 py-3 text-sm font-bold text-slate-800 dark:text-slate-200 hover:bg-slate-100 dark:hover:bg-zinc-800 active:bg-slate-200 dark:active:bg-zinc-700 transition-colors outline-none
              {r ? 'rounded-lg' : ''}"
              onclick={openPanel}
            >
              <ExternalLink size={16} />
              打开内核面板
            </button>
            <div class="mt-2 text-xs text-slate-500 dark:text-slate-400 break-all">{getPanelUrl()}</div>
          </div>
        </section>
      {:else if moduleId === "core"}
        <CoreInfo config={coreConfig} version={coreVersion} />
      {:else if moduleId === "log"}
        <LogTerminal bind:logs />
      {/if}
    </div>
  {/each}
</main>
