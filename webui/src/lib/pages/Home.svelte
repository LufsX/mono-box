<script lang="ts">
  import { onMount, tick } from "svelte";
  import { cubicOut } from "svelte/easing";
  import { flip } from "svelte/animate";
  import { fly } from "svelte/transition";
  import { clashApi, actionApi, parseBoxConfig, getBoxConfigRaw, actions as clashActions } from "$lib/api";
  import type { CoreStatusResult } from "$lib/api";
  import CoreInfo from "$lib/components/home/CoreInfo.svelte";
  import TunControl from "$lib/components/home/TunControl.svelte";
  import ServiceActions from "$lib/components/home/ServiceActions.svelte";
  import SystemStats from "$lib/components/home/SystemStats.svelte";
  import ProxyMode from "$lib/components/home/ProxyMode.svelte";
  import LogTerminal from "$lib/components/home/LogTerminal.svelte";
  import KernelAuthNotice from "$lib/components/shared/KernelAuthNotice.svelte";
  import { ExternalLink } from "@lucide/svelte";
  import { loadHomeLayoutSettings, type HomeModuleId } from "$lib/settings";
  import { isCoreBackedHomeModule } from "$lib/app-registry";

  let coreStatus = $state<CoreStatusResult | null>(null);
  let startingFromNotice = $state(false);
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
    if (logs.length > 200) {
      logs.splice(0, logs.length - 200);
    }
    logs = logs;
  }

  function moduleEnter(index: number) {
    return {
      y: 14,
      duration: 300,
      delay: Math.min(index * 22, 140),
      easing: cubicOut,
    };
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
    } catch (e: unknown) {
      const elapsedMs = Date.now() - startedAt;
      const errMsg = e instanceof Error ? e.message : String(e);
      addLog(errMsg, "error");
      addLog(`[Info] Execution time: ${elapsedMs} ms`, "info");
      return { success: false, errno: -1, raw: errMsg };
    }
  }

  async function checkCoreStatus() {
    try {
      coreStatus = await clashApi.checkStatus();
    } catch {
      coreStatus = { ok: false, reason: "unreachable", message: "检查核心状态时发生异常" };
    }
  }

  async function loadPanelDefaultUrl() {
    try {
      const content = await getBoxConfigRaw();
      const parsed = parseBoxConfig(content);
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

    if (isCoreBackedHomeModule(moduleId)) {
      return coreStatus?.ok !== false;
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
    } catch (e: unknown) {
      addLog(e instanceof Error ? e.message : String(e), "error");
    }
  }

  async function handleRefresh() {
    addLog("[Core] Refreshing status...", "info");
    await checkCoreStatus();
    addLog("[Core] Status refreshed", "success");
  }

  async function runAction(action: string) {
    await execute(action);

    if (["start", "restart", "stop"].includes(action)) {
      setTimeout(() => {
        checkCoreStatus();
      }, 2000);
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

        await checkCoreStatus();
        clashActions.refreshConfigs();
        const elapsedMs = Date.now() - startTime;
        addLog(`[WebUI] WebUI initialized with ${elapsedMs} ms`, "success");
      } catch (e) {
        console.error("Initialization error:", e);
      }
    })();
  });
</script>

<main class="relative max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-6">
  {#if coreStatus && !coreStatus.ok}
    <div in:fly={{ y: 16, duration: 260, easing: cubicOut }}>
      <KernelAuthNotice reason={coreStatus.reason} onRetry={handleRefresh} onStart={handleStartFromNotice} starting={startingFromNotice} showActions={true} />
    </div>
  {/if}

  {#each homeLayout.moduleOrder.filter(isModuleRenderable) as moduleId, index (moduleId)}
    <div in:fly={moduleEnter(index)} animate:flip={{ duration: 180, easing: cubicOut }}>
      {#if moduleId === "tun"}
        <TunControl onLog={addLog} />
      {:else if moduleId === "proxy"}
        <ProxyMode onLog={addLog} />
      {:else if moduleId === "stats"}
        <SystemStats />
      {:else if moduleId === "service"}
        <ServiceActions onAction={runAction} />
      {:else if moduleId === "panel"}
        <section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors rounded-xl">
          <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 rounded-t-xl">
            <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">面板快捷跳转</h2>
          </div>
          <div class="p-4">
            <button
              class="w-full inline-flex items-center justify-center gap-2 border border-slate-300 dark:border-zinc-700 py-3 text-sm font-bold text-slate-800 dark:text-slate-200 hover:bg-slate-100 dark:hover:bg-zinc-800 active:bg-slate-200 dark:active:bg-zinc-700 transition-colors outline-none
              rounded-lg"
              onclick={openPanel}
            >
              <ExternalLink size={16} />
              打开内核面板
            </button>
          </div>
        </section>
      {:else if moduleId === "core"}
        <CoreInfo />
      {:else if moduleId === "log"}
        <LogTerminal bind:logs />
      {/if}
    </div>
  {/each}
</main>
