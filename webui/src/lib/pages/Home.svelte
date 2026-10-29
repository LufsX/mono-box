<script lang="ts">
  import { onMount } from "svelte";
  import { cubicOut } from "svelte/easing";
  import { flip } from "svelte/animate";
  import { fly } from "svelte/transition";
  import { actionApi, parseBoxConfig, getBoxConfigRaw, stores, actions as clashActions } from "$lib/api";
  import CoreInfo from "$lib/components/home/CoreInfo.svelte";
  import Diagnostics from "$lib/components/home/Diagnostics.svelte";
  import TunControl from "$lib/components/home/TunControl.svelte";
  import ServiceActions from "$lib/components/home/ServiceActions.svelte";
  import SystemStats from "$lib/components/home/SystemStats.svelte";
  import ProxyMode from "$lib/components/home/ProxyMode.svelte";
  import LogTerminal from "$lib/components/home/LogTerminal.svelte";
  import KernelAuthNotice from "$lib/components/shared/KernelAuthNotice.svelte";
  import { ExternalLink } from "@lucide/svelte";
  import { loadHomeLayoutSettings, type HomeModuleId } from "$lib/settings";
  import { isCoreBackedHomeModule } from "$lib/app-registry";
  import { formatTerminalLine, type LogEntry, type LogType } from "$lib/terminal";

  const coreStatus = stores.coreStatus;
  let activeAction = $state<string | null>(null);
  let refreshing = $state(true);
  const busy = $derived(activeAction !== null || refreshing);
  let homeLayout = $state(loadHomeLayoutSettings());
  let defaultPanelUrl = $state("http://127.0.0.1:9090/ui");

  let logs = $state<LogEntry[]>([]);

  function addLog(msg: string, type: LogType = "info") {
    if (!msg) return;
    const time = new Date().toLocaleTimeString("en-US", { hour12: false });
    const lines = msg.split("\n").filter((line) => line.trim() !== "");
    lines.forEach((line) => {
      logs.push({ time, ...formatTerminalLine(line, type) });
    });
    if (logs.length > 200) {
      logs.splice(0, logs.length - 200);
    }
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
    addLog(`> ${actionCmd}`, "cmd");

    const startedAt = Date.now();

    try {
      const streamed = new Set<"stdout" | "stderr">();
      const result = await actionApi.runActionScript(actionCmd, (line, stream) => {
        streamed.add(stream);
        addLog(line, stream === "stderr" ? "warning" : "info");
      });
      if (!streamed.has("stdout")) addLog(result.stdout);
      if (!streamed.has("stderr")) addLog(result.stderr, "warning");
      const isError = result.errno !== 0;

      addLog(isError ? `[Action] Failed (errno=${result.errno})` : "[Action] Completed", isError ? "error" : "success");
    } catch (e: unknown) {
      const errMsg = e instanceof Error ? e.message : String(e);
      addLog(errMsg, "error");
    } finally {
      addLog(`[Info] Execution time: ${Date.now() - startedAt} ms`, "info");
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

  function isModuleRenderable(moduleId: HomeModuleId): boolean {
    if (homeLayout.hiddenModules.includes(moduleId)) return false;

    if (isCoreBackedHomeModule(moduleId)) {
      return $coreStatus?.ok === true;
    }

    return true;
  }

  async function openPanel() {
    const url = homeLayout.panelUrl.trim() || defaultPanelUrl;
    addLog(`> Opening panel: ${url}`, "cmd");
    try {
      await actionApi.openExternalUrl(url);
    } catch (e: unknown) {
      addLog(e instanceof Error ? e.message : String(e), "error");
    }
  }

  async function refreshCore() {
    const status = await clashActions.refreshConfigs();
    if (status.ok) await clashActions.refreshVersion();
  }

  async function handleRefresh() {
    if (busy) return;
    refreshing = true;
    addLog("[Core] Refreshing status...", "info");
    try {
      await refreshCore();
      addLog($coreStatus?.ok ? "[Core] Status refreshed" : $coreStatus?.message || "Status check failed", $coreStatus?.ok ? "success" : "warning");
    } finally {
      refreshing = false;
    }
  }

  async function runAction(action: string) {
    if (busy) return;
    activeAction = action;
    try {
      await execute(action);
      await refreshCore();
    } finally {
      activeAction = null;
    }
  }

  onMount(() => {
    (async () => {
      try {
        addLog("[WebUI] Initializing WebUI...", "info");
        const startTime = Date.now();
        homeLayout = loadHomeLayoutSettings();
        await Promise.all([loadPanelDefaultUrl(), refreshCore()]);
        const elapsedMs = Date.now() - startTime;
        addLog(`[WebUI] WebUI initialized with ${elapsedMs} ms`, "success");
      } catch (e) {
        addLog(`[WebUI] Initialization failed: ${e instanceof Error ? e.message : String(e)}`, "error");
      } finally {
        refreshing = false;
      }
    })();
  });
</script>

<main class="relative max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-6">
  {#if $coreStatus && !$coreStatus.ok}
    <div in:fly={{ y: 16, duration: 260, easing: cubicOut }}>
      <KernelAuthNotice reason={$coreStatus.reason} message={$coreStatus.message} onRetry={handleRefresh} onStart={() => runAction("start")} starting={activeAction === "start"} retrying={refreshing} disabled={busy} showActions={true} />
    </div>
  {/if}

  {#each homeLayout.moduleOrder.filter(isModuleRenderable) as moduleId, index (moduleId)}
    <div in:fly={moduleEnter(index)} animate:flip={{ duration: 180, easing: cubicOut }}>
      {#if moduleId === "tun"}
        <TunControl onLog={addLog} disabled={busy} />
      {:else if moduleId === "proxy"}
        <ProxyMode onLog={addLog} disabled={busy} />
      {:else if moduleId === "stats"}
        <SystemStats />
      {:else if moduleId === "service"}
        <ServiceActions onAction={runAction} {activeAction} disabled={refreshing} />
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

  <Diagnostics disabled={busy} />
</main>
