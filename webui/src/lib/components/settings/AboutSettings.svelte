<script lang="ts">
  import { ChevronRight } from "@lucide/svelte";

  let {
    currentVersion,
    currentVersionCode,
    onopen,
  } = $props<{
    currentVersion: string;
    currentVersionCode: string;
    onopen: (url: string) => void | Promise<void>;
  }>();
</script>

<button
  type="button"
  class="w-full p-4 text-left text-sm text-slate-600 dark:text-slate-400 cursor-pointer hover:bg-slate-50 dark:hover:bg-zinc-800/50 transition-colors"
  onclick={() => onopen("https://github.com/LufsX/mono-box")}
>
  <div class="flex items-center justify-between gap-3">
    <div class="space-y-2">
      <p class="font-bold text-slate-800 dark:text-slate-200">Mono Box {currentVersion}{currentVersionCode ? ` (${currentVersionCode})` : ""}</p>
      <p class="text-xs">基于 Mihomo 核心的代理工具</p>
    </div>
    <ChevronRight size={18} class="text-slate-400 dark:text-zinc-500" aria-hidden="true" />
  </div>
</button>
