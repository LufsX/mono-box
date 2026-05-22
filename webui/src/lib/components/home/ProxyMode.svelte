<script lang="ts">
  import { onMount } from "svelte";
  import { stores, actions } from "$lib/api";

  let { onLog }: { onLog?: (msg: string, type?: "info" | "success" | "error" | "cmd") => void } = $props();

  const currentMode = stores.currentMode;
  const modes = ["rule", "global", "direct"];

  async function handleSwitch(newMode: string) {
    if (newMode === $currentMode) return;
    onLog?.(`> Switching proxy mode to ${newMode}...`, "cmd");
    try {
      await actions.switchClashMode(newMode as import("$lib/api").ProxyMode);
      onLog?.(`[Core] Proxy mode switched to ${newMode}`, "success");
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      onLog?.(msg, "error");
    }
  }

  onMount(() => {
    actions.refreshConfigs();
  });
</script>

<section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors rounded-xl">
  <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 rounded-t-xl">
    <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">代理模式选择</h2>
  </div>
  <div class="p-4">
    <div class="flex text-sm font-bold w-full text-center">
      {#each modes as m, i}
        <button
          class="flex-1 py-2.5 transition-all duration-300 outline-none border -ml-px first:ml-0 {i === 0 ? 'rounded-l-lg' : i === modes.length - 1 ? 'rounded-r-lg' : ''} {$currentMode === m
            ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
            : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
          onclick={() => handleSwitch(m)}
        >
          {m.toUpperCase()}
        </button>
      {/each}
    </div>
  </div>
</section>
