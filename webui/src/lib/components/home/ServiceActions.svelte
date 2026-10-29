<script lang="ts">
  import { LoaderCircle } from "@lucide/svelte";

  let { onAction, activeAction = null, disabled = false }: {
    onAction: (action: string) => void | Promise<void>;
    activeAction?: string | null;
    disabled?: boolean;
  } = $props();

  const actions = [
    { id: "start", label: "启动" },
    { id: "stop", label: "停止" },
    { id: "restart", label: "重启" },
    { id: "status", label: "状态" },
  ];

</script>

<section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors rounded-xl">
  <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 rounded-t-xl">
    <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">快速控制指令</h2>
  </div>
  <div class="p-4 grid grid-cols-2 sm:grid-cols-4 gap-3">
    {#each actions as action}
      <button
        class="inline-flex items-center justify-center gap-2 border border-slate-300 dark:border-zinc-700 py-3 text-sm font-bold text-slate-800 dark:text-slate-200 hover:bg-slate-100 dark:hover:bg-zinc-800 active:bg-slate-200 dark:active:bg-zinc-700 transition-[transform,colors] duration-200 active:translate-y-px active:scale-[0.99] outline-none disabled:opacity-55 disabled:cursor-not-allowed
        rounded-lg"
        onclick={() => onAction(action.id)}
        disabled={disabled || activeAction !== null}
        aria-busy={activeAction === action.id}
      >
        {#if activeAction === action.id}<LoaderCircle size={14} class="animate-spin" />{/if}
        {action.label}
      </button>
    {/each}
  </div>
</section>
