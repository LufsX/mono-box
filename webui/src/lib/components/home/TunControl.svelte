<script lang="ts">
  import { onMount } from "svelte";
  import { clashApi } from "$lib/api";

  let { onLog }: { onLog?: (msg: string, type?: "info" | "success" | "error" | "cmd") => void } = $props();

  let enabled = $state(false);
  let upgrading = $state(false);
  let refreshBusy = $state(false);
  let loadError = $state("");
  let switching = $state(false);

  async function refresh() {
    if (refreshBusy) return;
    refreshBusy = true;
    try {
      const config = await clashApi.getConfigs();
      if (config.tun && typeof config.tun.enable === "boolean") {
        enabled = config.tun.enable;
      }
      loadError = "";
      return true;
    } catch (err) {
      loadError = err instanceof Error ? err.message : String(err);
      return false;
    } finally {
      refreshBusy = false;
    }
  }

  async function handleSwitch(value: boolean | "upgrade") {
    if (value === "upgrade") {
      if (upgrading || switching) return;
      upgrading = true;
      onLog?.("> Upgrading core...", "cmd");
      try {
        const result = await clashApi.upgradeCore();
        if (result && result.status === "ok") {
          onLog?.("[Core] Update OK", "success");
        } else if (result && result.message) {
          onLog?.(`[Core] Result: ${result.message}`, "info");
        } else {
          onLog?.("[Core] Unknown result: " + JSON.stringify(result), "info");
        }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        onLog?.(msg, "error");
      } finally {
        upgrading = false;
      }
      return;
    }

    if (switching || upgrading) return;
    switching = true;
    onLog?.(`> Switching TUN mode to ${value}...`, "cmd");
    try {
      await clashApi.setTun(value);
      enabled = value;
      const refreshed = await refresh();
      onLog?.(refreshed ? `[Core] TUN mode switched to ${value}` : `[Core] TUN mode switched to ${value}, but status refresh failed`, refreshed ? "success" : "info");
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      onLog?.(msg, "error");
    } finally {
      switching = false;
    }
  }

  onMount(() => {
    void refresh();
  });
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
          onclick={() => handleSwitch(true)} disabled={refreshBusy || switching || upgrading}>ON</button
        >
        <button
          class="px-4 py-1.5 transition-all duration-300 outline-none border -ml-px rounded-r-lg {!enabled
            ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
            : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
          onclick={() => handleSwitch(false)} disabled={refreshBusy || switching || upgrading}>OFF</button
        >
      </div>
    </div>
    {#if loadError}
      <div class="flex items-center justify-between gap-3 border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-300 rounded-lg" role="status">
        <span>无法读取 TUN 状态：{loadError}</span>
        <button type="button" class="shrink-0 underline underline-offset-2" onclick={() => refresh()} disabled={refreshBusy}>重试</button>
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
        disabled={upgrading || switching}>{upgrading ? "正在更新" : "立即更新"}</button
      >
    </div>
  </div>
</section>
