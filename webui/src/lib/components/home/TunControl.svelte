<script lang="ts">
  import { actions, clashApi, stores } from "$lib/api";
  import type { LogType } from "$lib/terminal";

  let { onLog, disabled = false }: { onLog?: (msg: string, type?: LogType) => void; disabled?: boolean } = $props();

  const configs = stores.configs;
  const enabled = $derived($configs?.tun?.enable === true);
  let upgrading = $state(false);
  let refreshBusy = $state(false);
  let loadError = $state("");
  let switching = $state(false);
  const busy = $derived(disabled || refreshBusy || switching || upgrading);

  async function refresh() {
    if (refreshBusy) return;
    refreshBusy = true;
    try {
      const status = await actions.refreshConfigs();
      loadError = status.ok ? "" : status.message || "Unable to refresh TUN status";
      return status.ok;
    } finally {
      refreshBusy = false;
    }
  }

  async function handleSwitch(value: boolean | "upgrade") {
    if (busy) return;
    if (value === "upgrade") {
      upgrading = true;
      onLog?.("> Upgrading core...", "cmd");
      try {
        const result = await clashApi.upgradeCore();
        if (result && result.status === "ok") {
          onLog?.(result.message || "[Core] Update OK", "success");
          await actions.refreshVersion();
        } else if (result && result.status === "unchanged") {
          onLog?.(result.message || "Already using the latest version", "info");
        } else if (result && result.message) {
          onLog?.(result.message, "info");
        } else {
          onLog?.("[Core] Unknown update result; refresh core status", "warning");
        }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        onLog?.(msg, "error");
      } finally {
        upgrading = false;
      }
      return;
    }

    switching = true;
    onLog?.(`> Switching TUN mode to ${value}...`, "cmd");
    try {
      await clashApi.setTun(value);
      const refreshed = await refresh();
      const message = `[Core] TUN mode switched to ${value}`;
      onLog?.(refreshed ? message : `${message}, but status refresh failed`, refreshed ? "success" : "warning");
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      onLog?.(msg, "error");
    } finally {
      switching = false;
    }
  }

</script>

<section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors rounded-xl">
  <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 rounded-t-xl">
    <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">网络接管与内核</h2>
  </div>
  <div class="p-4 flex flex-col gap-4">
    <div class="flex items-center justify-between">
      <div class="flex flex-col">
        <span class="text-sm font-bold text-slate-900 dark:text-slate-200">TUN 模式开启</span>
        <span class="text-xs text-slate-500 dark:text-zinc-400 mt-1">{enabled ? "底层流量网络全局接管中" : "已停用底层接管"}</span>
      </div>
      <div class="flex font-bold text-sm">
        <button
          class="px-4 py-1.5 transition-all duration-300 outline-none border -ml-px first:ml-0 rounded-l-lg {enabled
            ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
            : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
          onclick={() => handleSwitch(true)} disabled={busy}>ON</button
        >
        <button
          class="px-4 py-1.5 transition-all duration-300 outline-none border -ml-px rounded-r-lg {!enabled
            ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
            : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
          onclick={() => handleSwitch(false)} disabled={busy}>OFF</button
        >
      </div>
    </div>
    {#if loadError}
      <div class="flex items-center justify-between gap-3 border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-300 rounded-lg" role="status">
        <span>无法读取 TUN 状态：{loadError}</span>
        <button type="button" class="shrink-0 underline underline-offset-2" onclick={() => refresh()} disabled={busy}>重试</button>
      </div>
    {/if}
    <div class="h-px bg-slate-200 dark:bg-zinc-800 w-full"></div>
    <div class="flex items-center justify-between">
      <div class="flex flex-col">
        <span class="text-sm font-bold text-slate-900 dark:text-slate-200">内核在线更新</span>
        <span class="text-xs text-slate-500 dark:text-zinc-400 mt-1">调用 Mihomo Core 内置 API 进行更新</span>
      </div>
      <button
        class="border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 px-4 py-1.5 text-sm font-bold hover:bg-slate-100 dark:hover:bg-zinc-800 transition-all duration-300 text-slate-800 dark:text-slate-200 active:bg-slate-200 dark:active:bg-zinc-700 active:translate-y-px outline-none disabled:opacity-60
        rounded-lg"
        onclick={() => handleSwitch("upgrade")}
        disabled={busy}>{upgrading ? "正在更新" : "立即更新"}</button
      >
    </div>
  </div>
</section>
