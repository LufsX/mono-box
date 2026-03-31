<script lang="ts">
  let { mode = $bindable(), onSwitch }: { mode: string; onSwitch: (mode: string) => void } = $props();
</script>

<section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors">
  <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950">
    <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">代理模式选择</h2>
  </div>
  <div class="p-4">
    <div class="flex text-sm font-bold w-full text-center">
      {#each ["rule", "global", "direct"] as m}
        <button
          class="flex-1 py-2.5 transition-all outline-none border -ml-px first:ml-0 {mode === m
            ? 'border-slate-800 dark:border-slate-400 bg-slate-800 text-white dark:bg-slate-200 dark:text-zinc-900 z-10'
            : 'border-slate-300 dark:border-zinc-700 text-slate-500 hover:bg-slate-50 dark:hover:bg-zinc-800 z-0'}"
          onclick={() => onSwitch(m)}
        >
          {m.toUpperCase()}
        </button>
      {/each}
    </div>
  </div>
</section>
