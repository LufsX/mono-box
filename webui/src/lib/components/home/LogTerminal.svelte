<script lang="ts">
  import { onDestroy, tick } from "svelte";
  import { copyText } from "$lib/clipboard";
  import type { LogEntry, LogType } from "$lib/terminal";

  let { logs = $bindable() }: { logs: LogEntry[] } = $props();
  let logsContainer = $state<HTMLElement>();
  let stickToBottom = true;
  let copying = $state(false);
  let copied = $state(false);
  let copyError = $state("");
  let feedbackTimer: ReturnType<typeof setTimeout> | undefined;

  onDestroy(() => clearTimeout(feedbackTimer));

  async function copyLogs() {
    if (copying || !logs.length) return;
    copying = true;
    copied = false;
    copyError = "";
    clearTimeout(feedbackTimer);
    try {
      await copyText(logs.map((log) => `[${log.time}] ${log.msg}`).join("\n"));
      copied = true;
      feedbackTimer = setTimeout(() => { copied = false; }, 2000);
    } catch {
      copyError = "Copy failed. Select the logs to copy manually.";
    } finally {
      copying = false;
    }
  }

  const colors: Record<LogType, string> = {
    info: "text-slate-700 dark:text-zinc-300",
    cmd: "text-cyan-700 dark:text-cyan-300",
    success: "text-emerald-700 dark:text-emerald-400",
    warning: "text-amber-700 dark:text-amber-400",
    error: "text-rose-700 dark:text-rose-400",
  };

  function updateStickToBottom() {
    if (!logsContainer) return;
    const distance = logsContainer.scrollHeight - logsContainer.scrollTop - logsContainer.clientHeight;
    stickToBottom = distance < 24;
  }

  $effect(() => {
    // The last entry changes even when Home keeps the list capped at 200 entries.
    const lastLog = logs.at(-1);
    const container = logsContainer;
    if (!lastLog) {
      stickToBottom = true;
      return;
    }
    if (!container || !stickToBottom) return;

    let cancelled = false;
    void tick().then(() => {
      if (!cancelled && stickToBottom && logsContainer === container) {
        container.scrollTop = container.scrollHeight;
      }
    });
    return () => { cancelled = true; };
  });

  function clearLogs() {
    stickToBottom = true;
    copied = false;
    copyError = "";
    clearTimeout(feedbackTimer);
    logs = [];
  }
</script>

<section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 flex flex-col w-full min-w-0 mt-4 min-h-55 h-[34vh] max-h-105 rounded-xl overflow-hidden">
  <div class="px-4 py-2 bg-slate-50 dark:bg-zinc-950 border-b border-slate-200 dark:border-zinc-800 flex justify-between items-center shrink-0">
    <h2 class="m-0 text-xs font-medium text-slate-600 dark:text-zinc-400">Terminal Logs</h2>
    <div class="flex items-center gap-2">
      <button
        type="button"
        aria-label="Copy logs"
        disabled={copying || logs.length === 0}
        class="text-xs text-slate-500 dark:text-zinc-400 hover:text-slate-800 dark:hover:text-zinc-200 border border-slate-300 dark:border-zinc-700 px-2 py-1 transition-colors focus-visible:outline-2 focus-visible:outline-cyan-500 disabled:opacity-40 disabled:cursor-not-allowed rounded-md"
        onclick={copyLogs}
      >
        {copying ? "COPYING..." : copied ? "COPIED" : "COPY"}
      </button>
      <button
        type="button"
        class="text-xs text-slate-500 dark:text-zinc-400 hover:text-slate-800 dark:hover:text-zinc-200 border border-slate-300 dark:border-zinc-700 px-2 py-1 transition-colors focus-visible:outline-2 focus-visible:outline-cyan-500 rounded-md"
        onclick={clearLogs}
        disabled={copying || logs.length === 0}
      >
        CLEAR
      </button>
    </div>
  </div>
  {#if copyError}<div role="status" class="px-3 py-2 text-xs text-rose-700 dark:text-rose-400">{copyError}</div>{/if}
  <div class="terminal-output p-3 sm:p-4 flex-1 min-h-0 min-w-0 overflow-y-auto overflow-x-hidden font-mono text-xs leading-relaxed custom-scrollbar" bind:this={logsContainer} onscroll={updateStickToBottom}>
    {#if logs.length === 0}
      <div class="text-slate-400 dark:text-zinc-500 text-center mt-10">Waiting for output...</div>
    {:else}
      {#each logs as log}
        <div class="flex min-w-0 gap-2 sm:gap-3 mb-1">
          <span class="text-slate-400 dark:text-zinc-600 shrink-0 select-none tabular-nums">[{log.time}]</span>
          <span class="min-w-0 flex-1 whitespace-pre-wrap [overflow-wrap:anywhere] select-text {colors[log.type]}">{#each log.segments ?? [{ text: log.msg }] as segment}<span style:color={segment.color} style:font-weight={segment.bold ? "700" : undefined}>{segment.text}</span>{/each}</span>
        </div>
      {/each}
    {/if}
  </div>
</section>

<style>
  .terminal-output {
    --terminal-ansi-0: #334155;
    --terminal-ansi-1: #be123c;
    --terminal-ansi-2: #047857;
    --terminal-ansi-3: #a16207;
    --terminal-ansi-4: #1d4ed8;
    --terminal-ansi-5: #a21caf;
    --terminal-ansi-6: #0e7490;
    --terminal-ansi-7: #64748b;
    --terminal-ansi-8: #64748b;
    --terminal-ansi-9: #e11d48;
    --terminal-ansi-10: #059669;
    --terminal-ansi-11: #b45309;
    --terminal-ansi-12: #2563eb;
    --terminal-ansi-13: #c026d3;
    --terminal-ansi-14: #0891b2;
    --terminal-ansi-15: #475569;
    scrollbar-width: thin;
    scrollbar-color: #94a3b855 transparent;
  }
  :global(.dark) .terminal-output {
    --terminal-ansi-0: #cbd5e1;
    --terminal-ansi-1: #fb7185;
    --terminal-ansi-2: #34d399;
    --terminal-ansi-3: #fbbf24;
    --terminal-ansi-4: #60a5fa;
    --terminal-ansi-5: #e879f9;
    --terminal-ansi-6: #67e8f9;
    --terminal-ansi-7: #e2e8f0;
    --terminal-ansi-8: #94a3b8;
    --terminal-ansi-9: #fda4af;
    --terminal-ansi-10: #6ee7b7;
    --terminal-ansi-11: #fde68a;
    --terminal-ansi-12: #93c5fd;
    --terminal-ansi-13: #f0abfc;
    --terminal-ansi-14: #a5f3fc;
    --terminal-ansi-15: #f8fafc;
  }
  .custom-scrollbar::-webkit-scrollbar {
    width: 6px;
    height: 6px;
  }
  .custom-scrollbar::-webkit-scrollbar-track {
    background: transparent;
  }
  .custom-scrollbar::-webkit-scrollbar-thumb {
    background: #94a3b855;
    border-radius: 10px;
  }
  .custom-scrollbar::-webkit-scrollbar-thumb:hover {
    background: #94a3b888;
  }
</style>
