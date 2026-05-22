<script lang="ts">
  let {
    reason = "unreachable",
    message,
    onRetry,
    onStart,
    showActions = false,
    retrying = false,
    starting = false,
  }: {
    reason?: "unauthorized" | "unreachable";
    message?: string;
    onRetry?: () => void;
    onStart?: () => void;
    showActions?: boolean;
    retrying?: boolean;
    starting?: boolean;
  } = $props();

  const title = $derived(reason === "unauthorized" ? "核心认证失败" : "核心连接异常");
  const detail = $derived(message ?? (reason === "unauthorized" ? "Clash API 密钥不正确或未配置，请前往设置页面配置正确的密钥" : "未连接到内核，内核可能未运行，请尝试启动服务"));
</script>

<section class="bg-amber-50 dark:bg-amber-950/40 border border-amber-300 dark:border-amber-800 transition-colors rounded-xl">
  <div class="px-4 py-3 border-b border-amber-300 dark:border-amber-800 bg-amber-100/70 dark:bg-amber-900/35 rounded-t-xl">
    <h2 class="text-xs font-bold uppercase tracking-widest text-amber-700 dark:text-amber-300 m-0">{title}</h2>
  </div>
  <div class="p-4 flex flex-col gap-3">
    <p class="text-sm font-medium text-amber-800 dark:text-amber-200 m-0">{detail}</p>
    {#if showActions}
      <div class="grid grid-cols-1 gap-2">
        {#if reason === "unauthorized"}
          <a
            href="#/settings"
            class="inline-flex items-center justify-center border border-amber-400 dark:border-amber-700 py-2.5 text-sm font-bold text-amber-800 dark:text-amber-200 hover:bg-amber-100 dark:hover:bg-amber-900/40 transition-colors outline-none rounded-lg"
          >
            前往设置
          </a>
        {:else if onStart}
          <button
            type="button"
            onclick={onStart}
            disabled={starting}
            class="inline-flex items-center justify-center border border-amber-400 dark:border-amber-700 py-2.5 text-sm font-bold text-amber-800 dark:text-amber-200 hover:bg-amber-100 dark:hover:bg-amber-900/40 transition-colors outline-none disabled:opacity-60 rounded-lg"
          >
            {starting ? "正在启动" : "启动服务"}
          </button>
        {/if}
        <div class="grid grid-cols-2 gap-2">
          {#if onRetry}
            <button
              type="button"
              onclick={onRetry}
              disabled={retrying}
              class="inline-flex items-center justify-center border border-amber-400 dark:border-amber-700 py-2.5 text-sm font-bold text-amber-800 dark:text-amber-200 hover:bg-amber-100 dark:hover:bg-amber-900/40 transition-colors outline-none disabled:opacity-60 rounded-lg"
            >
              {retrying ? "正在重试" : "重试检测"}
            </button>
          {/if}
          {#if reason === "unauthorized"}
            {#if onStart}
              <button
                type="button"
                onclick={onStart}
                disabled={starting}
                class="inline-flex items-center justify-center border border-amber-400 dark:border-amber-700 py-2.5 text-sm font-bold text-amber-800 dark:text-amber-200 hover:bg-amber-100 dark:hover:bg-amber-900/40 transition-colors outline-none disabled:opacity-60 rounded-lg"
              >
                {starting ? "正在启动" : "启动服务"}
              </button>
            {/if}
          {:else}
            <a
              href="#/settings"
              class="inline-flex items-center justify-center border border-amber-400 dark:border-amber-700 py-2.5 text-sm font-bold text-amber-800 dark:text-amber-200 hover:bg-amber-100 dark:hover:bg-amber-900/40 transition-colors outline-none rounded-lg"
            >
              前往设置
            </a>
          {/if}
        </div>
      </div>
    {/if}
  </div>
</section>
