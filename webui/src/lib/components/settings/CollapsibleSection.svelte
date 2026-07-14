<script lang="ts">
  import { onMount } from "svelte";
  import type { Snippet } from "svelte";
  import { cubicOut } from "svelte/easing";
  import { fly, type TransitionConfig } from "svelte/transition";
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
  let mounted = $state(false);

  onMount(() => {
    mounted = true;
  });

  function reveal(node: HTMLElement, { duration }: { duration: number }): TransitionConfig {
    const height = node.scrollHeight;
    return {
      duration,
      easing: cubicOut,
      css: (t, u) => `height: ${t * height}px; border-top-width: ${t}px; opacity: ${t}; transform: translate3d(0, ${-6 * u}px, 0); overflow: hidden;`,
    };
  }

  function toggleOpen() {
    open = !open;
    onchange?.(open);
  }
</script>

<section in:fly={{ y: 8, duration: 240, delay, easing: cubicOut }} class="overflow-hidden bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors rounded-xl">
  <button
    type="button"
    class="w-full px-4 py-3 flex items-center justify-between gap-3 bg-slate-50 dark:bg-zinc-950 text-left transition-colors hover:bg-slate-100 dark:hover:bg-zinc-900"
    aria-expanded={open}
    aria-controls={controls}
    onclick={toggleOpen}
  >
    <span class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400">{title}</span>
    <ChevronDown size={16} class="shrink-0 text-slate-500 dark:text-zinc-500 transition-transform duration-200 {open ? 'rotate-180' : ''}" aria-hidden="true" />
  </button>

  {#if open}
    <div id={controls} class="border-t border-slate-300 dark:border-zinc-700" in:reveal={{ duration: mounted ? 260 : 0 }} out:reveal={{ duration: 190 }}>
      {@render children()}
    </div>
  {/if}
</section>
