<script lang="ts">
  import { tick } from "svelte";
  import { roundedStore } from "$lib/settings";

  type LogType = "info" | "success" | "error" | "cmd";
  type LogEntry = { time: string; msg: string; type: LogType };

  let { logs = $bindable() }: { logs: LogEntry[] } = $props();
  let logsContainer: HTMLElement;
  let stickToBottom = true;

  const r = $derived($roundedStore);

  function updateStickToBottom() {
    if (!logsContainer) return;
    const distanceFromBottom = logsContainer.scrollHeight - (logsContainer.scrollTop + logsContainer.clientHeight);
    stickToBottom = distanceFromBottom < 24;
  }

  // 自动滚动到底部：仅在用户选择“粘底”时才跟随
  $effect(() => {
    if (logs.length > 0 && logsContainer) {
      if (!stickToBottom) return;

      tick().then(() => {
        logsContainer.scrollTop = logsContainer.scrollHeight;
      });
    }
  });

  function clearLogs() {
    logs = [];
  }
</script>

<section class="bg-zinc-950 border border-zinc-800 flex flex-col w-full mt-4 min-h-55 h-[34vh] max-h-105 {r ? 'rounded-xl overflow-hidden' : ''}">
  <div class="px-4 py-2 bg-zinc-900 border-b border-zinc-800 flex justify-between items-center shrink-0 {r ? 'rounded-t-xl' : ''}">
    <h2 class="m-0 text-xs font-mono font-bold tracking-widest text-zinc-500 uppercase">Terminal Logs</h2>
    <button
      class="text-xs font-mono text-zinc-400 hover:text-zinc-200 border border-zinc-700 bg-zinc-900 px-2 py-1 transition-all duration-300 outline-none hover:border-zinc-500 {r ? 'rounded-md' : ''}"
      onclick={clearLogs}
    >
      CLEAR
    </button>
  </div>
  <div class="p-4 flex-1 overflow-y-auto font-mono text-xs leading-relaxed custom-scrollbar bg-black" bind:this={logsContainer} onscroll={updateStickToBottom}>
    {#if logs.length === 0}
      <div class="text-zinc-700 text-base text-center mt-10">Waiting for output...</div>
    {:else}
      {#each logs as log}
        <div class="flex gap-4 mb-1 border-b border-zinc-900 pb-1 break-all">
          <span class="text-zinc-600 shrink-0 select-none">[{log.time}]</span>
          <span
            class={log.type === "error"
              ? "text-red-400 whitespace-pre-wrap"
              : log.type === "cmd"
                ? "text-sky-400 font-bold whitespace-pre-wrap"
                : log.type === "success"
                  ? "text-green-400 whitespace-pre-wrap"
                  : "text-zinc-300 whitespace-pre-wrap"}>{log.msg}</span
          >
        </div>
      {/each}
    {/if}
  </div>
</section>

<style>
  .custom-scrollbar::-webkit-scrollbar {
    width: 6px;
    height: 6px;
  }
  .custom-scrollbar::-webkit-scrollbar-track {
    background: transparent;
  }
  .custom-scrollbar::-webkit-scrollbar-thumb {
    background: #334155;
    border-radius: 10px;
  }
  .custom-scrollbar::-webkit-scrollbar-thumb:hover {
    background: #475569;
  }
</style>
