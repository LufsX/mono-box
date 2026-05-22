<script lang="ts">
  import type { Snippet } from "svelte";
  import { cubicOut } from "svelte/easing";
  import { fly, slide } from "svelte/transition";
  import { ChevronDown } from "@lucide/svelte";
  let {
    title,
    controls,
    delay = 20,
    open = $bindable(),
    onchange,
    children,
  } = $props<{
    title: string;
    controls: string;
    delay?: number;
    open: boolean;
    onchange?: (open: boolean) => void;
    children: Snippet;
  }>();

  function toggleOpen() {
    open = !open;
    onchange?.(open);
  }
</script>

<section in:fly={{ y: 12, duration: 260, delay, easing: cubicOut }} class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors rounded-xl">
  <button
    type="button"
    class="w-full px-4 py-3 flex items-center justify-between gap-3 border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 text-left transition-colors hover:bg-slate-100 dark:hover:bg-zinc-900 {open
      ? 'border-b'
      : ''} {open ? 'rounded-t-xl' : 'rounded-xl'}"
    aria-expanded={open}
    aria-controls={controls}
    onclick={toggleOpen}
  >
    <span class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400">{title}</span>
    <ChevronDown size={16} class="shrink-0 text-slate-500 dark:text-zinc-500 transition-transform duration-200 {open ? 'rotate-180' : ''}" aria-hidden="true" />
  </button>

  {#if open}
    <div id={controls} transition:slide={{ duration: 180, easing: cubicOut }}>
      {@render children()}
    </div>
  {/if}
</section>
