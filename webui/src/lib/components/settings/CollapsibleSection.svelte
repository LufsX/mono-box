<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import type { Snippet } from "svelte";
  import { cubicOut } from "svelte/easing";
  import { fly, type TransitionConfig } from "svelte/transition";
  import { ChevronDown, LoaderCircle } from "@lucide/svelte";
  let {
    title,
    controls,
    delay = 20,
    open = $bindable(),
    onchange,
    beforeopen,
    keepMounted = false,
    children,
  } = $props<{
    title: string;
    controls: string;
    delay?: number;
    open: boolean;
    onchange?: (open: boolean) => void;
    beforeopen?: () => Promise<void>;
    keepMounted?: boolean;
    children: Snippet;
  }>();
  let mounted = $state(false);
  let loading = $state(false);
  let openError = $state("");
  let destroyed = false;

  onMount(() => {
    mounted = true;
  });

  onDestroy(() => {
    destroyed = true;
  });

  function reveal(node: HTMLElement, { duration }: { duration: number }): TransitionConfig {
    const height = node.scrollHeight;
    return {
      duration,
      easing: cubicOut,
      css: (t, u) => `height: ${t * height}px; border-top-width: ${t}px; opacity: ${t}; transform: translate3d(0, ${-6 * u}px, 0); overflow: hidden;`,
    };
  }

  export async function expand() {
    if (open || loading || destroyed) return;
    loading = true;
    openError = "";
    try {
      if (beforeopen) await beforeopen();
      if (destroyed) return;
      open = true;
      onchange?.(true);
    } catch (error) {
      if (destroyed) return;
      openError = (error instanceof Error ? error.message : String(error)) || "加载失败，请重试";
    } finally {
      if (!destroyed) loading = false;
    }
  }

  function toggleOpen() {
    if (loading) return;
    if (!open) {
      void expand();
      return;
    }
    open = false;
    openError = "";
    onchange?.(false);
  }
</script>

<section in:fly={{ y: 8, duration: 240, delay, easing: cubicOut }} class="overflow-hidden bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors rounded-xl">
  <button
    type="button"
    class="w-full px-4 py-3 flex items-center justify-between gap-3 bg-slate-50 dark:bg-zinc-950 text-left transition-colors hover:bg-slate-100 dark:hover:bg-zinc-900 disabled:cursor-wait"
    aria-expanded={open}
    aria-controls={controls}
    aria-busy={loading}
    aria-describedby={openError ? `${controls}-error` : undefined}
    disabled={loading}
    onclick={toggleOpen}
  >
    <span class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400">{title}</span>
    <span class="flex shrink-0 items-center gap-2 text-slate-500 dark:text-zinc-500">
      {#if loading}
        <span class="inline-flex items-center gap-1.5 text-xs" role="status">
          <LoaderCircle size={14} class="animate-spin" aria-hidden="true" />
          加载中
        </span>
      {/if}
      <ChevronDown size={16} class="transition-transform duration-200 {open ? 'rotate-180' : ''}" aria-hidden="true" />
    </span>
  </button>

  {#if openError}
    <div id={`${controls}-error`} class="flex items-start justify-between gap-3 border-t border-red-200 bg-red-50 px-4 py-2 text-xs text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
      <span class="min-w-0 break-words [overflow-wrap:anywhere]" role="alert">{openError}</span>
      <button type="button" class="shrink-0 underline underline-offset-2 disabled:cursor-wait disabled:opacity-60" onclick={() => expand()} disabled={loading}>重试</button>
    </div>
  {/if}

  {#if keepMounted}
    <div id={controls} class="persistent-content" class:expanded={open} inert={!open} aria-hidden={!open}>
      <div class="min-h-0 overflow-hidden">
        <div class="border-t border-slate-300 dark:border-zinc-700">
          {@render children()}
        </div>
      </div>
    </div>
  {:else if open}
    <div id={controls} class="border-t border-slate-300 dark:border-zinc-700" in:reveal={{ duration: mounted ? 260 : 0 }} out:reveal={{ duration: 190 }}>
      {@render children()}
    </div>
  {/if}
</section>

<style>
  .persistent-content {
    display: grid;
    grid-template-rows: 0fr;
    opacity: 0;
    transition: grid-template-rows 190ms cubic-bezier(0.215, 0.61, 0.355, 1),
      opacity 190ms cubic-bezier(0.215, 0.61, 0.355, 1);
  }

  .persistent-content.expanded {
    grid-template-rows: 1fr;
    opacity: 1;
    transition-duration: 260ms;
  }

  @media (prefers-reduced-motion: reduce) {
    .persistent-content {
      transition: none;
    }
  }
</style>
