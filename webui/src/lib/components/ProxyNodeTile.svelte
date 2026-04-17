<script lang="ts">
  import { Check } from "@lucide/svelte";

  let {
    name,
    type,
    latency = 0,
    selected = false,
    selectable = false,
    testing = false,
    rounded = false,
    onSelect,
    onTest,
  }: {
    name: string;
    type: string;
    latency?: number;
    selected?: boolean;
    selectable?: boolean;
    testing?: boolean;
    rounded?: boolean;
    onSelect?: () => void;
    onTest?: (event: MouseEvent) => void;
  } = $props();

  function latencyStyle(ms: number) {
    if (!ms || ms === 0) return { text: "text-slate-400 dark:text-slate-500", dot: "bg-slate-300 dark:bg-slate-700" };
    if (ms < 200) return { text: "text-emerald-600 dark:text-emerald-400", dot: "bg-emerald-500" };
    if (ms < 800) return { text: "text-amber-600 dark:text-amber-400", dot: "bg-amber-500" };
    if (ms < 1500) return { text: "text-orange-600 dark:text-orange-400", dot: "bg-orange-500" };
    return { text: "text-rose-600 dark:text-rose-400", dot: "bg-rose-500" };
  }

  const style = $derived(latencyStyle(latency));
</script>

<div
  class="relative border px-2.5 py-2 text-left transition-colors outline-none {selected
    ? 'border-emerald-500/60 bg-emerald-50 dark:bg-emerald-900/20'
    : 'border-slate-300 dark:border-zinc-700 bg-slate-50/60 dark:bg-zinc-900/50 hover:bg-slate-100 dark:hover:bg-zinc-800'} {rounded ? 'rounded-lg' : ''}"
  role="button"
  tabindex={selectable ? 0 : -1}
  onclick={() => {
    if (!selectable) return;
    onSelect?.();
  }}
  onkeydown={(event) => {
    if (!selectable) return;
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onSelect?.();
    }
  }}
>
  <div class="flex items-center gap-1.5 min-w-0">
    <span class={`w-1.5 h-1.5 shrink-0 ${style.dot} ${rounded ? "rounded-lg" : ""}`}></span>
    <span class="text-sm font-semibold text-slate-800 dark:text-slate-200 truncate pl-0.5">{name}</span>
    {#if selected}
      <Check size={13} class="text-emerald-600 dark:text-emerald-400 shrink-0" />
    {/if}
  </div>
  <div class="mt-1.5 pt-1 border-t border-slate-200 dark:border-zinc-700 border-dashed flex items-center justify-between gap-1.5">
    <span class="text-[10px] font-mono uppercase text-slate-400 dark:text-zinc-500 truncate">{type}</span>
    <button
      class={`text-[10px] font-mono font-bold px-1 border border-slate-300 dark:border-zinc-700 bg-white/70 dark:bg-zinc-900/80 hover:bg-slate-200 dark:hover:bg-zinc-700 transition-colors ${style.text} ${rounded ? "rounded" : ""}`}
      onclick={onTest}
      title="点击测速"
    >
      <span class="inline-block min-w-10 text-center tabular-nums transition-opacity duration-150 {testing ? 'opacity-85' : 'opacity-100'}">
        {#if testing}
          测试中
        {:else}
          {latency ? `${latency} ms` : "-"}
        {/if}
      </span>
    </button>
  </div>
</div>
