<script lang="ts">
  import { roundedStore } from "$lib/settings";

  let {
    message,
    onRetry,
    onStart,
    retrying = false,
    starting = false,
  }: {
    message: string;
    onRetry: () => void;
    onStart: () => void;
    retrying?: boolean;
    starting?: boolean;
  } = $props();

  const r = $derived($roundedStore);
</script>

<section class="bg-amber-50 dark:bg-amber-950/40 border border-amber-300 dark:border-amber-800 transition-colors {r ? 'rounded-xl' : ''}">
  <div class="px-4 py-3 border-b border-amber-300 dark:border-amber-800 bg-amber-100/70 dark:bg-amber-900/35 {r ? 'rounded-t-xl' : ''}">
    <h2 class="text-xs font-bold uppercase tracking-widest text-amber-700 dark:text-amber-300 m-0">核心连接异常</h2>
  </div>
  <div class="p-4 flex flex-col gap-3">
    <p class="text-sm font-medium text-amber-800 dark:text-amber-200 m-0">{message}</p>
    <div class="grid grid-cols-1 gap-2">
      <button
        type="button"
        onclick={onStart}
        disabled={starting}
        class="inline-flex items-center justify-center border border-amber-400 dark:border-amber-700 py-2.5 text-sm font-bold text-amber-800 dark:text-amber-200 hover:bg-amber-100 dark:hover:bg-amber-900/40 transition-colors outline-none disabled:opacity-60
        {r ? 'rounded-lg' : ''}"
      >
        {starting ? "正在启动" : "启动服务"}
      </button>
      <div class="grid grid-cols-2 gap-2">
        <button
          type="button"
          onclick={onRetry}
          disabled={retrying}
          class="inline-flex items-center justify-center border border-amber-400 dark:border-amber-700 py-2.5 text-sm font-bold text-amber-800 dark:text-amber-200 hover:bg-amber-100 dark:hover:bg-amber-900/40 transition-colors outline-none disabled:opacity-60
          {r ? 'rounded-lg' : ''}"
        >
          {retrying ? "正在重试" : "重试检测"}
        </button>
        <a
          href="#/settings"
          class="inline-flex items-center justify-center border border-amber-400 dark:border-amber-700 py-2.5 text-sm font-bold text-amber-800 dark:text-amber-200 hover:bg-amber-100 dark:hover:bg-amber-900/40 transition-colors outline-none
          {r ? 'rounded-lg' : ''}"
        >
          前往设置
        </a>
      </div>
    </div>
  </div>
</section>
