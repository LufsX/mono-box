<script lang="ts">
  import { tick } from "svelte";

  type LogType = "info" | "success" | "error" | "cmd";
  type LogEntry = { time: string; msg: string; type: LogType };

  let { logs = $bindable() }: { logs: LogEntry[] } = $props();
  let logsContainer: HTMLElement;

  // 自动滚动到底部
  $effect(() => {
    if (logs.length > 0 && logsContainer) {
      tick().then(() => {
        logsContainer.scrollTop = logsContainer.scrollHeight;
      });
    }
  });

  function clearLogs() {
    logs = [];
  }
</script>

<section class="bg-zinc-950 border border-zinc-800 flex flex-col h-100 w-full mt-4">
  <div class="px-4 py-2 bg-zinc-900 border-b border-zinc-800 flex justify-between items-center shrink-0">
    <h2 class="m-0 text-xs font-mono font-bold tracking-widest text-zinc-500 uppercase">Terminal Logs</h2>
    <button class="text-xs font-mono text-zinc-500 hover:text-zinc-300 transition-colors outline-none" onclick={clearLogs}>CLEAR</button>
  </div>
  <div class="p-4 flex-1 overflow-y-auto font-mono text-xs leading-relaxed custom-scrollbar bg-black" bind:this={logsContainer}>
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
