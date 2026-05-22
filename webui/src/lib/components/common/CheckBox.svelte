<script lang="ts">
  import { Check } from "@lucide/svelte";

  interface Props {
    id: string;
    checked: boolean;
    onchange: () => void;
    label?: string;
    bare?: boolean;
    variant?: "checkbox" | "switch";
  }

  let { id, checked, onchange, label, bare = false, variant = "checkbox" }: Props = $props();
</script>

<label
  for={id}
  class="group flex items-center gap-3 cursor-pointer select-none transition-colors {bare
    ? ''
    : 'border border-slate-300 dark:border-zinc-700 px-3 py-2.5 bg-white dark:bg-zinc-950/50 hover:bg-slate-50 dark:hover:bg-zinc-800'} rounded-xl"
>
  <div class="relative flex items-center justify-center">
    {#if variant === "switch"}
      <div class="relative w-10 h-6 group-active:scale-[0.98]">
        <input {id} type="checkbox" {checked} {onchange} class="peer sr-only" />
        <div
          class="absolute inset-0 border border-slate-300 dark:border-zinc-700 bg-slate-100 dark:bg-zinc-800 transition-colors
          peer-checked:bg-slate-800 peer-checked:border-slate-800
          dark:peer-checked:bg-slate-200 dark:peer-checked:border-slate-200
          rounded-xl"
        ></div>
        <div
          class="absolute top-0.5 left-0.5 w-5 h-5 bg-white dark:bg-zinc-950 transition-transform
          peer-checked:translate-x-4 dark:peer-checked:bg-zinc-900 rounded-xl"
        ></div>
      </div>
    {:else}
      <input {id} type="checkbox" {checked} {onchange} class="peer sr-only" />
      <div
        class="h-5 w-5 border-2 border-slate-300 dark:border-zinc-600 bg-transparent transition-all
        peer-checked:bg-slate-800 peer-checked:border-slate-800
        dark:peer-checked:bg-slate-200 dark:peer-checked:border-slate-200 rounded-sm"
      ></div>
      <div class="absolute text-white dark:text-zinc-900 scale-0 peer-checked:scale-100 transition-transform duration-200 ease-out pointer-events-none">
        <Check size={14} strokeWidth={4} />
      </div>
    {/if}
  </div>
  {#if label}
    <span class="text-sm font-bold text-slate-800 dark:text-slate-200">{label}</span>
  {/if}
</label>
