<script lang="ts">
  import { roundedStore } from "$lib/settings";

  let { mode = $bindable(), onSwitch }: { mode: string; onSwitch: (mode: string) => void } = $props();

  const r = $derived($roundedStore);
  const modes = ["rule", "global", "direct"];
</script>

<section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors {r ? 'rounded-xl' : ''}">
  <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 {r ? 'rounded-t-xl' : ''}">
    <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">代理模式选择</h2>
  </div>
  <div class="p-4">
    <div class="flex text-sm font-bold w-full text-center">
      {#each modes as m, i}
        <button
          class="flex-1 py-2.5 transition-all duration-300 outline-none border -ml-px first:ml-0 {r ? (i === 0 ? 'rounded-l-lg' : i === modes.length - 1 ? 'rounded-r-lg' : '') : ''} {mode === m
            ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
            : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
          onclick={() => onSwitch(m)}
        >
          {m.toUpperCase()}
        </button>
      {/each}
    </div>
  </div>
</section>
