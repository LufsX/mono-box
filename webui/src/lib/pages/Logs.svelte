<script lang="ts">
  import { flip } from "svelte/animate";
  import { onDestroy, onMount, tick } from "svelte";
  import { fade } from "svelte/transition";
  import { cubicOut } from "svelte/easing";
  import { Pause, Play, RefreshCw, ScrollText, Search, Trash2 } from "@lucide/svelte";
  import { actionApi, clashApi } from "$lib/api";
  import type { ClashLogEntry, ClashLogLevel } from "$lib/api";
  import type { LogSizeReport } from "$lib/api/action";
  import Select from "$lib/components/common/Select.svelte";
  import { formatBytes } from "$lib/utils";

  type LogLevelFilter = "all" | ClashLogLevel;
  type StreamState = "connecting" | "connected" | "closed" | "error";
  type LogRow = ClashLogEntry & { id: number; time: string };
  type FileBanner = { tone: "error" | "notice"; message: string };

  const LEVEL_OPTIONS: { value: LogLevelFilter; label: string }[] = [
    { value: "all", label: "全部" },
    { value: "debug", label: "Debug" },
    { value: "info", label: "Info" },
    { value: "warning", label: "Warning" },
    { value: "error", label: "Error" },
  ];

  let selectedLevel = $state<LogLevelFilter>("all");
  let searchQuery = $state("");
  let paused = $state(false);
  let streamState = $state<StreamState>("connecting");
  let streamError = $state("");
  let logRows = $state<LogRow[]>([]);
  let logReport = $state<LogSizeReport>({ files: [], totalBytes: 0, count: 0 });
  let logFileLoading = $state(false);
  let clearingFiles = $state(false);
  let terminalClearing = $state(false);
  let fileError = $state("");
  let fileNotice = $state("");
  let logsContainer: HTMLElement | undefined = $state();
  let logFilesBody: HTMLElement | undefined = $state();
  let logFilesBodyHeight = $state<number | null>(null);
  let stickToTop = $state(true);
  let rowId = 0;
  let socketEpoch = 0;
  let terminalClearToken = 0;
  let fileNoticeTimer: ReturnType<typeof window.setTimeout> | undefined;
  let logSocket: WebSocket | null = null;

  const sortedLogFiles = $derived([...logReport.files].sort((a, b) => b.size - a.size || a.path.localeCompare(b.path)));
  const fileBanner = $derived<FileBanner | null>(fileError ? { tone: "error", message: fileError } : fileNotice ? { tone: "notice", message: fileNotice } : null);
  const filteredLogRows = $derived.by(() => {
    const keyword = searchQuery.trim().toLowerCase();
    if (!keyword) return logRows;

    return logRows.filter((row) => {
      return row.type.toLowerCase().includes(keyword) || row.time.includes(keyword) || row.payload.toLowerCase().includes(keyword);
    });
  });

  function timeNow(): string {
    return new Date().toLocaleTimeString("en-US", { hour12: false });
  }

  function delay(ms: number): Promise<void> {
    return new Promise((resolve) => {
      window.setTimeout(resolve, ms);
    });
  }

  function clearFileNotice() {
    if (fileNoticeTimer) {
      window.clearTimeout(fileNoticeTimer);
      fileNoticeTimer = undefined;
    }
    fileNotice = "";
  }

  function showFileNotice(message: string) {
    clearFileNotice();
    fileNotice = message;
    fileNoticeTimer = window.setTimeout(() => {
      if (fileNotice === message) {
        fileNotice = "";
      }
      fileNoticeTimer = undefined;
    }, 2800);
  }

  function lockLogFilesBodyHeight() {
    if (!logFilesBody || logFilesBodyHeight !== null) return;

    const height = logFilesBody.offsetHeight;
    if (height > 0) {
      logFilesBodyHeight = height;
    }
  }

  async function settleLogFilesBodyHeight() {
    if (logFilesBodyHeight === null) return;

    await tick();
    if (logFilesBody) {
      const nextHeight = logFilesBody.scrollHeight;
      if (nextHeight > 0) {
        logFilesBodyHeight = nextHeight;
        await delay(180);
      }
    }

    logFilesBodyHeight = null;
  }

  function updateStickToTop() {
    if (!logsContainer) return;
    stickToTop = logsContainer.scrollTop < 32;
  }

  function normalizeLogEntry(data: ClashLogEntry): ClashLogEntry {
    if (data && typeof data === "object") {
      return {
        type: typeof data.type === "string" ? data.type : "info",
        payload: typeof data.payload === "string" ? data.payload : JSON.stringify(data),
      };
    }

    return { type: "info", payload: String(data) };
  }

  function appendLog(data: ClashLogEntry) {
    if (paused || terminalClearing) return;

    const entry = normalizeLogEntry(data);
    logRows.unshift({
      ...entry,
      id: rowId,
      time: timeNow(),
    });
    rowId += 1;

    if (logRows.length > 600) {
      logRows.splice(600);
    }
    logRows = logRows;
  }

  function closeLogSocket() {
    if (!logSocket) return;
    const socket = logSocket;
    logSocket = null;
    socket.close();
  }

  async function connectLogStream(level: LogLevelFilter) {
    const token = ++socketEpoch;
    closeLogSocket();
    streamState = "connecting";
    streamError = "";

    try {
      const socket = await clashApi.createLogsWebSocket(
        level === "all" ? undefined : level,
        (entry) => {
          if (token !== socketEpoch) return;
          appendLog(entry);
        },
        () => {
          if (token !== socketEpoch) return;
          streamState = "error";
          streamError = "无法连接 Clash 日志流";
        },
      );

      if (token !== socketEpoch) {
        socket.close();
        return;
      }

      logSocket = socket;
      socket.onopen = () => {
        if (token !== socketEpoch) return;
        streamState = "connected";
      };
      socket.onclose = () => {
        if (token !== socketEpoch) return;
        if (streamState !== "error") {
          streamState = "closed";
        }
      };

      if (socket.readyState === WebSocket.OPEN) {
        streamState = "connected";
      }
    } catch (e) {
      if (token !== socketEpoch) return;
      streamState = "error";
      streamError = e instanceof Error ? e.message : String(e);
    }
  }

  function disconnectLogStream() {
    socketEpoch += 1;
    closeLogSocket();
  }

  async function clearVisibleLogs() {
    if (terminalClearing || logRows.length === 0) return;

    const token = ++terminalClearToken;
    terminalClearing = true;
    logRows = [];

    await delay(120);
    if (token !== terminalClearToken) return;
    terminalClearing = false;
  }

  function typeClass(type: string): string {
    const normalized = type.toLowerCase();
    const base = "inline-flex min-w-[4.25rem] justify-center border px-1.5 py-0.5 text-[10px] font-bold uppercase leading-none rounded-md";

    if (normalized === "error") return `${base} border-red-800 bg-red-950/50 text-red-300`;
    if (normalized === "warning") return `${base} border-amber-800 bg-amber-950/50 text-amber-300`;
    if (normalized === "debug") return `${base} border-sky-800 bg-sky-950/50 text-sky-300`;
    return `${base} border-emerald-800 bg-emerald-950/40 text-emerald-300`;
  }

  function statusClass(): string {
    if (streamState === "connected" && !paused) return "bg-emerald-500";
    if (streamState === "connecting") return "bg-amber-500";
    if (paused) return "bg-slate-400";
    return "bg-red-500";
  }

  function statusLabel(): string {
    if (paused) return "已暂停";
    if (streamState === "connected") return "实时";
    if (streamState === "connecting") return "连接中";
    if (streamState === "closed") return "已断开";
    return "错误";
  }

  function displayPath(path: string): string {
    return path.replace(/^\/data\/adb\/box\//, "box/");
  }

  function fileBannerClass(tone: FileBanner["tone"]): string {
    const base = "flex min-h-9 items-center border px-3 py-2 text-xs shadow-lg backdrop-blur rounded-lg";
    if (tone === "error") return `${base} border-red-300 bg-red-50/95 text-red-700 dark:border-red-900/50 dark:bg-red-950/90 dark:text-red-300`;
    return `${base} border-amber-300 bg-amber-50/95 text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/90 dark:text-amber-300`;
  }

  async function refreshLogFiles() {
    lockLogFilesBodyHeight();

    try {
      logFileLoading = true;
      fileError = "";
      clearFileNotice();
      logReport = await actionApi.getLogSizeReport();
    } catch (e) {
      fileError = e instanceof Error ? e.message : String(e);
    } finally {
      await settleLogFilesBodyHeight();
      logFileLoading = false;
    }
  }

  function clearResultSkippedActiveLog(stdout: string): boolean {
    return stdout.split(/\r?\n/).some((line) => line.startsWith("file\tskipped\t"));
  }

  async function clearLogFiles() {
    if (clearingFiles) return;

    const minimumClearAnimation = delay(180);
    lockLogFilesBodyHeight();

    try {
      clearingFiles = true;
      fileError = "";
      clearFileNotice();
      const result = await actionApi.clearLogFiles();
      const nextReport = await actionApi.getLogSizeReport();
      await minimumClearAnimation;
      logReport = nextReport;
      if (clearResultSkippedActiveLog(result.stdout)) {
        showFileNotice("内核运行中，已保留当前日志文件");
      }
    } catch (e) {
      await minimumClearAnimation;
      fileError = e instanceof Error ? e.message : String(e);
    } finally {
      await settleLogFilesBodyHeight();
      clearingFiles = false;
    }
  }

  $effect(() => {
    const level = selectedLevel;
    void connectLogStream(level);

    return () => {
      disconnectLogStream();
    };
  });

  onMount(() => {
    void refreshLogFiles();
  });

  $effect(() => {
    const visibleCount = filteredLogRows.length;
    if (visibleCount === 0 || !logsContainer || !stickToTop || terminalClearing) return;

    void tick().then(() => {
      if (!logsContainer || !stickToTop) return;
      logsContainer.scrollTop = 0;
    });
  });

  onDestroy(() => {
    terminalClearToken += 1;
    if (fileNoticeTimer) {
      window.clearTimeout(fileNoticeTimer);
    }
  });
</script>

<main class="max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-4">
  <div class="flex items-center justify-between gap-3">
    <h1 class="text-xl font-bold flex items-center gap-2 text-slate-800 dark:text-slate-100 min-w-0">
      <ScrollText size={20} />
      <span class="truncate">内核日志</span>
    </h1>

    <div class="flex items-center gap-2 shrink-0 text-xs font-bold text-slate-500 dark:text-zinc-400">
      <span class={`h-2 w-2 rounded-full ${statusClass()}`}></span>
      <span>{statusLabel()}</span>
    </div>
  </div>

  <section class="border border-zinc-800 bg-zinc-950 overflow-hidden rounded-xl">
    <div class="flex flex-col gap-3 border-b border-zinc-800 bg-zinc-900 px-3 py-3">
      <div class="grid grid-cols-[minmax(0,1fr)_auto] gap-3 items-center">
        <div class="w-36 text-xs font-bold">
          <Select id="log-level" options={LEVEL_OPTIONS} bind:value={selectedLevel} variant="dark" onchange={(value) => (selectedLevel = value as LogLevelFilter)} />
        </div>

        <div class="flex items-center gap-2">
          <button
            type="button"
            class="inline-flex h-8 w-8 items-center justify-center border border-zinc-700 bg-zinc-950 text-zinc-300 hover:bg-zinc-800 transition-colors rounded-lg"
            onclick={() => {
              paused = !paused;
            }}
            title={paused ? "继续" : "暂停"}
            aria-label={paused ? "继续" : "暂停"}
          >
            {#if paused}
              <Play size={15} />
            {:else}
              <Pause size={15} />
            {/if}
          </button>

          <button
            type="button"
            class="inline-flex h-8 w-8 items-center justify-center border border-zinc-700 bg-zinc-950 text-zinc-300 hover:bg-zinc-800 transition-colors rounded-lg"
            onclick={() => connectLogStream(selectedLevel)}
            title="重连"
            aria-label="重连"
          >
            <RefreshCw size={15} class={streamState === "connecting" ? "animate-spin" : ""} />
          </button>

          <button
            type="button"
            class="inline-flex h-8 w-8 items-center justify-center border border-zinc-700 bg-zinc-950 text-zinc-300 hover:bg-zinc-800 transition-colors disabled:cursor-not-allowed disabled:opacity-40 rounded-lg"
            onclick={clearVisibleLogs}
            disabled={terminalClearing || logRows.length === 0}
            title="清屏"
            aria-label="清屏"
          >
            <Trash2 size={15} />
          </button>
        </div>
      </div>

      <label class="relative block">
        <Search size={14} class="absolute left-3 top-1/2 -translate-y-1/2 text-zinc-600" />
        <input
          type="search"
          bind:value={searchQuery}
          class="w-full border border-zinc-700 bg-zinc-950 py-2 pl-9 pr-3 text-sm text-zinc-200 outline-none transition-colors placeholder:text-zinc-600 focus:border-zinc-500 rounded-lg"
          placeholder="搜索日志内容"
        />
      </label>
    </div>

    <div class="relative h-[52vh] min-h-96 overflow-hidden bg-black font-mono text-xs leading-relaxed">
      {#if streamError}
        <div class="pointer-events-none absolute left-3 right-3 top-3 z-20" in:fade={{ duration: 120 }} out:fade={{ duration: 100 }}>
          <div class="border border-red-900/50 bg-red-950/90 px-3 py-2 text-xs text-red-300 shadow-sm backdrop-blur rounded-lg">
            {streamError}
          </div>
        </div>
      {/if}

      <div class="h-full overflow-y-auto p-3 custom-scrollbar" bind:this={logsContainer} onscroll={updateStickToTop}>
        {#if logRows.length === 0}
          <div class="flex h-full items-center justify-center text-zinc-700" in:fade={{ duration: 160 }}>Waiting for logs...</div>
        {:else if filteredLogRows.length === 0}
          <div class="flex h-full items-center justify-center text-zinc-700" in:fade={{ duration: 160 }}>无匹配日志</div>
        {:else}
          <div class="space-y-2">
            {#each filteredLogRows as row (row.id)}
              <div class="border-b border-zinc-900 pb-2 text-zinc-300" animate:flip={{ duration: 120, easing: cubicOut }} in:fade={{ duration: 90 }} out:fade={{ duration: 90 }}>
                <div class="flex items-center justify-between gap-3">
                  <span class={typeClass(row.type)}>{row.type}</span>
                  <time class="shrink-0 select-none text-zinc-600">[{row.time}]</time>
                </div>
                <div class="mt-2 wrap-break-word whitespace-pre-wrap text-zinc-300">{row.payload}</div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  </section>

  <section class="border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-900/90 rounded-xl overflow-hidden">
    <div class="flex items-center justify-between gap-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 px-4 py-3">
      <div class="min-w-0">
        <h2 class="m-0 text-sm font-bold text-slate-900 dark:text-slate-100">日志文件</h2>
        <div class="mt-1 text-xs font-mono text-slate-500 dark:text-zinc-500">{logReport.count} files / {formatBytes(logReport.totalBytes)}</div>
      </div>

      <div class="flex items-center gap-2 shrink-0">
        <button
          type="button"
          class="inline-flex items-center justify-center h-8 w-8 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-600 dark:text-zinc-300 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors disabled:opacity-60 rounded-lg"
          onclick={refreshLogFiles}
          disabled={logFileLoading || clearingFiles}
          title="刷新"
          aria-label="刷新"
        >
          <RefreshCw size={14} class={logFileLoading ? "animate-spin" : ""} />
        </button>
        <button
          type="button"
          class="inline-flex h-8 min-w-22 items-center justify-center gap-2 border border-rose-200 bg-rose-50 px-3 text-xs font-bold text-rose-600 transition-colors hover:bg-rose-100 disabled:cursor-not-allowed disabled:opacity-60 dark:border-rose-900/40 dark:bg-rose-950/30 dark:text-rose-400 dark:hover:bg-rose-900/50 rounded-lg {clearingFiles
            ? 'border-rose-300 bg-rose-100 dark:border-rose-800/70 dark:bg-rose-950/50'
            : ''}"
          onclick={clearLogFiles}
          disabled={clearingFiles || logReport.totalBytes === 0}
          title="清理日志"
        >
          <Trash2 size={14} class={clearingFiles ? "opacity-70" : ""} />
          <span>{clearingFiles ? "正在清理" : "清理日志"}</span>
        </button>
      </div>
    </div>

    <div class="p-4">
      <div
        class="relative min-h-24 overflow-hidden transition-[height] duration-200 ease-out"
        bind:this={logFilesBody}
        style={logFilesBodyHeight === null ? "" : `height: ${logFilesBodyHeight}px`}
      >
        {#if clearingFiles && sortedLogFiles.length > 0}
          <div class="pointer-events-none absolute inset-0 z-10 bg-white/45 dark:bg-zinc-950/35" in:fade={{ duration: 100 }} out:fade={{ duration: 120 }}></div>
        {/if}

        {#if logFileLoading && sortedLogFiles.length === 0}
          <div class="py-8 text-center text-sm text-slate-500 dark:text-zinc-400" in:fade={{ duration: 160 }}>正在加载日志文件...</div>
        {:else if sortedLogFiles.length === 0}
          <div class="py-8 text-center text-sm text-slate-500 dark:text-zinc-400" in:fade={{ duration: 160 }}>暂无日志文件</div>
        {:else}
          <div class="space-y-2">
            {#each sortedLogFiles as file (file.path)}
              <article
                class="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 border border-slate-200 bg-slate-50/70 px-3 py-2 transition-opacity duration-150 dark:border-zinc-800 dark:bg-zinc-950/40 rounded-lg {clearingFiles
                  ? 'opacity-55'
                  : ''}"
                animate:flip={{ duration: 140, easing: cubicOut }}
                in:fade={{ duration: 100 }}
                out:fade={{ duration: 100 }}
              >
                <div class="min-w-0">
                  <div class="truncate font-mono text-xs text-slate-800 dark:text-zinc-200" title={file.path}>{displayPath(file.path)}</div>
                  <div class="mt-0.5 truncate text-[11px] text-slate-400 dark:text-zinc-600">{file.path}</div>
                </div>
                <div class="font-mono text-xs font-bold text-slate-700 dark:text-zinc-300">{formatBytes(file.size)}</div>
              </article>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  </section>

  {#if fileBanner}
    <div class="file-banner-toast" in:fade={{ duration: 120 }} out:fade={{ duration: 100 }}>
      <div class={fileBannerClass(fileBanner.tone)}>
        <span class="block truncate">{fileBanner.message}</span>
      </div>
    </div>
  {/if}
</main>

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

  .wrap-break-word {
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .file-banner-toast {
    position: fixed;
    left: max(1rem, var(--ksu-insets-left, 0px));
    right: max(1rem, var(--ksu-insets-right, 0px));
    bottom: calc(var(--app-bottombar-height, 4rem) + 1rem);
    z-index: 50;
    max-width: 48rem;
    margin-inline: auto;
    pointer-events: none;
  }
</style>
