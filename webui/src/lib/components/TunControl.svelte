<script lang="ts">
  let { enabled = $bindable(), onSwitch }: { enabled: boolean; onSwitch: (enable: boolean) => void } = $props();
</script>

<section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors">
  <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950">
    <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">网络接管与内核</h2>
  </div>
  <div class="p-4 flex flex-col gap-4">
    <div class="flex items-center justify-between">
      <div class="flex flex-col">
        <span class="text-sm font-bold text-slate-900 dark:text-slate-200">TUN 模式开启</span>
        <span class="text-xs text-slate-500 dark:text-slate-400 mt-1">{enabled ? "底层流量网络全局接管中" : "已停用底层接管"}</span>
      </div>
      <div class="flex font-bold text-sm">
        <button
          class="px-4 py-1.5 transition-all outline-none border -ml-px first:ml-0 {enabled
            ? 'border-slate-800 dark:border-slate-400 bg-slate-800 text-white dark:bg-slate-200 dark:text-zinc-900 z-10'
            : 'border-slate-300 dark:border-zinc-700 text-slate-500 hover:bg-slate-50 dark:hover:bg-zinc-800 z-0'}"
          onclick={() => onSwitch(true)}>ON</button
        >
        <button
          class="px-4 py-1.5 transition-all outline-none border -ml-px {!enabled
            ? 'border-slate-800 dark:border-slate-400 bg-slate-800 text-white dark:bg-slate-200 dark:text-zinc-900 z-10'
            : 'border-slate-300 dark:border-zinc-700 text-slate-500 hover:bg-slate-50 dark:hover:bg-zinc-800 z-0'}"
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
        class="border border-slate-300 dark:border-zinc-700 px-4 py-1.5 text-sm font-bold hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors text-slate-800 dark:text-slate-200 active:bg-slate-200"
        onclick={() => onSwitch("upgrade")}>立即拉取</button
      >
    </div>
  </div>
</section>
