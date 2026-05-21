<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { ChevronDown, Check } from "@lucide/svelte";
  import { fade, fly } from "svelte/transition";
  import { cubicOut } from "svelte/easing";
  import { roundedStore } from "$lib/settings";

  let {
    id = "",
    options = [],
    value = $bindable(),
    disabled = false,
    onchange,
  } = $props<{
    id?: string;
    options: { value: string; label: string }[];
    value: string;
    disabled?: boolean;
    onchange?: (value: string) => void;
  }>();

  let open = $state(false);
  let containerRef: HTMLDivElement | undefined = $state();

  const r = $derived($roundedStore);

  function handleWindowClick(e: MouseEvent) {
    if (open && containerRef && !containerRef.contains(e.target as Node)) {
      open = false;
    }
  }

  onMount(() => {
    window.addEventListener("click", handleWindowClick);
  });

  onDestroy(() => {
    if (typeof window !== "undefined") {
      window.removeEventListener("click", handleWindowClick);
    }
  });

  function selectOption(v: string) {
    value = v;
    open = false;
    onchange?.(v);
  }

  let selectedLabel = $derived(options.find((o: { value: string; label: string }) => o.value === value)?.label || value);
</script>

<div class="relative" bind:this={containerRef}>
  <button
    {id}
    type="button"
    {disabled}
    onclick={() => (open = !open)}
    class="w-full flex items-center justify-between px-3 py-1.5 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors disabled:opacity-50 disabled:cursor-not-allowed
    {r ? 'rounded-lg' : ''}"
  >
    <span class="truncate text-sm">{selectedLabel}</span>
    <ChevronDown size={16} class="text-slate-500 transition-transform duration-200 {open ? 'rotate-180' : ''}" />
  </button>

  {#if open}
    <div
      in:fly={{ y: -5, duration: 150, easing: cubicOut }}
      out:fade={{ duration: 100 }}
      class="absolute left-0 right-0 top-[calc(100%+4px)] z-50 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-900 shadow-xl {r ? 'rounded-lg overflow-hidden' : ''}"
    >
      <div class="max-h-60 overflow-y-auto flex flex-col">
        {#each options as option (option.value)}
          <button
            type="button"
            class="flex items-center justify-between px-3 py-2 text-left text-sm transition-colors {value === option.value
              ? 'bg-slate-100 dark:bg-zinc-800 text-slate-900 dark:text-slate-100 font-bold'
              : 'text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-zinc-800'}"
            onclick={() => selectOption(option.value)}
          >
            <span>{option.label}</span>
            {#if value === option.value}
              <Check size={14} class="text-slate-600 dark:text-slate-400" />
            {/if}
          </button>
        {/each}
      </div>
    </div>
  {/if}
</div>
