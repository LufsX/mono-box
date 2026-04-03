<script lang="ts">
  import { RefreshCw } from "@lucide/svelte";
  import { roundedStore } from "$lib/settings";

  let {
    enabled = $bindable(),
    onSwitch,
    onRefresh,
    refreshing = false,
  }: {
    enabled: boolean;
    onSwitch: (enable: boolean | "upgrade") => void;
    onRefresh?: () => void;
    refreshing?: boolean;
  } = $props();

  const r = $derived($roundedStore);
</script>

<section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors {r ? 'rounded-xl' : ''}">
  <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 flex items-center justify-between gap-3 {r ? 'rounded-t-xl' : ''}">
    <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">网络接管与内核</h2>
    <button
      class="inline-flex items-center justify-center p-0 m-0 leading-none text-slate-700 dark:text-slate-200 hover:text-slate-900 dark:hover:text-white transition-colors duration-200 outline-none disabled:opacity-45 disabled:cursor-not-allowed"
      onclick={() => onRefresh?.()}
      disabled={refreshing}
      aria-label="刷新状态"
      title="刷新状态"
    >
      <RefreshCw size={16} strokeWidth={2} class={refreshing ? "animate-spin" : ""} />
    </button>
  </div>
  <div class="p-4 flex flex-col gap-4">
    <div class="flex items-center justify-between">
      <div class="flex flex-col">
        <span class="text-sm font-bold text-slate-900 dark:text-slate-200">TUN 模式开启</span>
        <span class="text-xs text-slate-500 dark:text-slate-400 mt-1">{enabled ? "底层流量网络全局接管中" : "已停用底层接管"}</span>
      </div>
      <div class="flex font-bold text-sm">
        <button
          class="px-4 py-1.5 transition-[transform,colors] duration-200 outline-none border -ml-px first:ml-0 active:translate-y-px active:scale-[0.99] {r ? 'rounded-l-lg' : ''} {enabled
            ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
            : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
          onclick={() => onSwitch(true)}>ON</button
        >
        <button
          class="px-4 py-1.5 transition-[transform,colors] duration-200 outline-none border -ml-px active:translate-y-px active:scale-[0.99] {r ? 'rounded-r-lg' : ''} {!enabled
            ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
            : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
          onclick={() => onSwitch(false)}>OFF</button
        >
      </div>
    </div>
    <div class="h-px bg-slate-200 dark:bg-zinc-800 w-full"></div>
    <div class="flex items-center justify-between">
      <div class="flex flex-col">
        <span class="text-sm font-bold text-slate-900 dark:text-slate-200">内核在线更新</span>
        <span class="text-xs text-slate-500 dark:text-slate-400 mt-1">调用 Mihomo Core 内置 API 进行更新</span>
      </div>
      <button
        class="border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 px-4 py-1.5 text-sm font-bold hover:bg-slate-100 dark:hover:bg-zinc-800 transition-all duration-300 text-slate-800 dark:text-slate-200 active:bg-slate-200 dark:active:bg-zinc-700 active:translate-y-px outline-none
        {r ? 'rounded-lg' : ''}"
        onclick={() => onSwitch("upgrade")}>立即更新</button
      >
    </div>
  </div>
</section>
