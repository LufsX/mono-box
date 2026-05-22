<script lang="ts">
  import { onMount } from "svelte";
  import { stores, actions } from "$lib/api";

  const configs = stores.configs;
  const versionCheck = stores.versionCheck;

  const safeConfig = $derived($configs ?? {});

  const ports = $derived(
    [
      { label: "HTTP PORT", key: "port" },
      { label: "SOCKS PORT", key: "socks-port" },
      { label: "MIXED PORT", key: "mixed-port" },
      { label: "REDIR PORT", key: "redir-port" },
      { label: "TPROXY PORT", key: "tproxy-port" },
    ].filter((p) => safeConfig[p.key] && safeConfig[p.key] !== 0),
  );

  const version = $derived($versionCheck?.ok ? $versionCheck.version : "-");

  onMount(() => {
    actions.refreshConfigs();
    actions.refreshVersion();
  });
</script>

<section class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors rounded-xl">
  <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 rounded-t-xl">
    <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">核心状态与端口</h2>
  </div>
  <div class="p-4 flex flex-col gap-4">
    {#if ports.length > 0}
      <div class="grid grid-cols-2 sm:grid-cols-3 gap-y-5 gap-x-4">
        {#each ports as port}
          <div class="flex flex-col">
            <span class="text-[10px] font-bold tracking-wider text-slate-400 dark:text-zinc-500">{port.label}</span>
            <span class="text-sm font-mono font-bold text-slate-800 dark:text-slate-200">{safeConfig[port.key]}</span>
          </div>
        {/each}
      </div>
    {/if}
    <div class="flex flex-col">
      <span class="text-[10px] font-bold tracking-wider text-slate-400 dark:text-zinc-500">VERSION</span>
      <span class="text-sm font-mono font-bold text-slate-800 dark:text-slate-200 break-all">{version}</span>
    </div>
  </div>
</section>
