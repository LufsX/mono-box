<script lang="ts">
  import { onMount, tick } from "svelte";
  import { ChevronDown, Copy, RefreshCw, Stethoscope } from "@lucide/svelte";
  import { fade, slide } from "svelte/transition";
  import { flip } from "svelte/animate";
  import { cubicOut } from "svelte/easing";
  import { runDiagnostics, type DiagnosticCheck, type DiagnosticReport } from "$lib/api/maintenance";
  import { getErrorMessage } from "$lib/api/error-utils";
  import { copyText } from "$lib/clipboard";

  let { disabled = false } = $props<{ disabled?: boolean }>();
  let report = $state<DiagnosticReport | null>(null);
  let expanded = $state(false);
  let running = $state(false);
  let copying = $state(false);
  let error = $state("");
  let copyStatus = $state("");
  let manualCopy = $state(false);
  let documentVisible = $state(false);
  let reportField: HTMLTextAreaElement | undefined = $state();
  let contentHeight = $state(0);
  let mounted = false;
  let generation = 0;

  const labels = { ok: "正常", warning: "注意", error: "异常", skipped: "已跳过" };
  const summary = $derived.by(() => {
    const checks = report?.checks;
    return checks ? (["ok", "warning", "error", "skipped"] as const)
      .map((status) => `${labels[status]} ${checks.filter((check) => check.status === status).length}`)
      .join(" · ") : "";
  });

  function resultClass(status: DiagnosticCheck["status"]): string {
    switch (status) {
      case "ok": return "border-emerald-200 dark:border-emerald-900/60 bg-emerald-50 dark:bg-emerald-950/20 text-emerald-700 dark:text-emerald-400";
      case "warning": return "border-amber-200 dark:border-amber-900/60 bg-amber-50 dark:bg-amber-950/20 text-amber-700 dark:text-amber-400";
      case "error": return "border-red-200 dark:border-red-900/60 bg-red-50 dark:bg-red-950/20 text-red-700 dark:text-red-400";
      default: return "border-slate-200 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950/50 text-slate-500 dark:text-zinc-400";
    }
  }

  function formatTime(value: number): string {
    const date = new Date(value);
    return value > 0 && Number.isFinite(date.getTime()) ? date.toLocaleString("zh-CN", { hour12: false }) : "时间未知";
  }

  async function run() {
    if (!mounted || !documentVisible || disabled || running || copying) return;
    expanded = true;
    const currentGeneration = ++generation;
    running = true;
    error = "";
    try {
      const next = await runDiagnostics();
      if (mounted && currentGeneration === generation) {
        report = next;
        copyStatus = "";
      }
    } catch (cause) {
      if (mounted && currentGeneration === generation) error = getErrorMessage(cause);
    } finally {
      if (mounted && currentGeneration === generation) running = false;
    }
  }

  async function copyReport() {
    if (!mounted || !report || running || copying || !documentVisible) return;
    const currentGeneration = generation;
    copying = true;
    copyStatus = "";
    try {
      await copyText(report.text);
      if (mounted && currentGeneration === generation) {
        copyStatus = "脱敏报告已复制";
        manualCopy = false;
      }
    } catch {
      if (!mounted || currentGeneration !== generation) return;
      manualCopy = true;
      copyStatus = "无法自动复制，请选择下方报告并手动复制。";
      await tick();
      if (mounted && expanded && currentGeneration === generation) {
        reportField?.focus();
        reportField?.select();
      }
    } finally {
      if (mounted && currentGeneration === generation) copying = false;
    }
  }

  onMount(() => {
    mounted = true;
    const onVisibility = () => {
      documentVisible = document.visibilityState === "visible";
    };
    onVisibility();
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      mounted = false;
      generation += 1;
      document.removeEventListener("visibilitychange", onVisibility);
    };
  });
</script>

<svelte:window onkeydown={(event) => {
  if (event.key === "Escape" && expanded) {
    event.preventDefault();
    expanded = false;
  }
}} />

<section class="rounded-xl border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-900 transition-colors">
  <div class="flex items-center justify-between gap-3 p-4">
    <div class="min-w-0">
      <h2 class="m-0 flex items-center gap-2 text-sm font-bold text-slate-800 dark:text-slate-200">
        <Stethoscope size={16} class="shrink-0" />运行诊断
      </h2>
      <p class="mt-1 text-xs text-slate-500 dark:text-zinc-400">检查模块、内核与网络状态。</p>
    </div>
    <button type="button" onclick={run} disabled={disabled || running || copying || !documentVisible}
      class="inline-flex shrink-0 items-center justify-center gap-1.5 rounded-lg border border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 px-3 py-2 text-sm font-bold text-white dark:text-zinc-900 transition-colors hover:bg-slate-700 dark:hover:bg-slate-300 disabled:opacity-50">
      <RefreshCw size={14} class={running ? "animate-spin" : ""} />
      {running ? "正在诊断" : error ? "重试诊断" : "一键诊断"}
    </button>
  </div>

  {#if running || report || error}
    <button type="button" class="flex w-full items-center justify-between gap-2 border-t border-slate-200 dark:border-zinc-800 px-4 py-2 text-left text-xs text-slate-600 dark:text-zinc-400 hover:bg-slate-50 dark:hover:bg-zinc-950/50"
      aria-expanded={expanded} aria-controls="home-diagnostic-report" onclick={() => { expanded = !expanded; }}>
      <span>{expanded ? "收起报告" : "展开报告"}{report ? ` · ${summary}` : ""}</span>
      <ChevronDown size={14} class="shrink-0 transition-transform {expanded ? 'rotate-180' : ''}" />
    </button>
  {/if}

  {#if expanded}
    <div id="home-diagnostic-report" class="border-t border-slate-200 dark:border-zinc-800 p-4" transition:slide={{ duration: 200, easing: cubicOut }} aria-busy={running}>
      <div class="overflow-hidden transition-[height] duration-250 ease-out motion-reduce:transition-none"
        style:height={contentHeight > 0 ? `${contentHeight}px` : undefined}>
        <div bind:offsetHeight={contentHeight} class="grid items-start">
          {#if report}
            <div class="col-start-1 row-start-1 min-w-0 space-y-3" in:fade={{ duration: 180 }} out:fade={{ duration: 100 }}>
              <div class="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2">
                <p class="truncate text-xs text-slate-500 dark:text-zinc-400"
                  title={`生成时间：${formatTime(report.generatedAt)}`} aria-live="polite">
                  {running ? "正在重新诊断…" : `生成时间：${formatTime(report.generatedAt)}`}
                </p>
                <button type="button" class="inline-flex items-center gap-1.5 whitespace-nowrap rounded-lg border border-slate-300 dark:border-zinc-700 px-3 py-2 text-xs font-bold text-slate-700 dark:text-zinc-300 hover:bg-slate-100 dark:hover:bg-zinc-800 disabled:opacity-50"
                  onclick={copyReport} disabled={running || copying || !report.text || !documentVisible}>
                  <Copy size={14} />{copying ? "正在复制" : "复制脱敏报告"}
                </button>
              </div>
              {#if error}
                <p role="alert" class="break-words rounded-lg border border-red-300 dark:border-red-900/50 bg-red-50 dark:bg-red-950/30 p-3 text-xs text-red-700 dark:text-red-300 [overflow-wrap:anywhere]"
                  transition:slide={{ duration: 180, easing: cubicOut }}>本次诊断失败，已保留上次结果：{error}</p>
              {/if}
              <ul class="m-0 max-h-[55dvh] list-none space-y-2 overflow-y-auto p-0">
                {#each report.checks as check (check.id)}
                  <li class="rounded-lg border p-3 transition-colors duration-200 motion-reduce:transition-none {resultClass(check.status)}"
                    animate:flip={{ duration: 200, easing: cubicOut }}
                    transition:slide={{ duration: 180, easing: cubicOut }}>
                    <div class="flex items-start justify-between gap-3 text-xs font-bold">
                      <span class="min-w-0 break-words [overflow-wrap:anywhere]">{check.title}</span>
                      <span class="grid shrink-0">
                        {#key check.status}
                          {@const status = check.status}
                          <span class="col-start-1 row-start-1" aria-hidden={status !== check.status}
                            in:fade={{ duration: 180 }} out:fade={{ duration: 100 }}>{labels[status]}</span>
                        {/key}
                      </span>
                    </div>
                    <div class="mt-1 grid">
                      {#key check.message}
                        {@const message = check.message}
                        <p class="col-start-1 row-start-1 break-words text-xs leading-relaxed text-slate-600 dark:text-zinc-300 [overflow-wrap:anywhere]"
                          aria-hidden={message !== check.message}
                          in:fade={{ duration: 180 }} out:fade={{ duration: 100 }}>{message}</p>
                      {/key}
                    </div>
                  </li>
                {:else}
                  <li class="py-2 text-xs text-slate-500 dark:text-zinc-400" transition:fade={{ duration: 140 }}>本次报告没有检查项目。</li>
                {/each}
              </ul>
              {#if copyStatus}
                <p role="status" class="text-xs text-slate-600 dark:text-zinc-400"
                  transition:slide={{ duration: 160, easing: cubicOut }}>{copyStatus}</p>
              {/if}
              {#if manualCopy}
                <div class="space-y-2" transition:slide={{ duration: 180, easing: cubicOut }}>
                  <label for="diagnostic-report-copy" class="text-xs font-bold text-slate-700 dark:text-zinc-300">脱敏报告（可选择复制）</label>
                  <textarea id="diagnostic-report-copy" bind:this={reportField} readonly value={report.text} rows="8"
                    class="block w-full resize-y rounded-lg border border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 p-3 font-mono text-xs text-slate-700 dark:text-zinc-300 outline-none focus:border-slate-800 dark:focus:border-slate-400"></textarea>
                  <button type="button" onclick={() => { reportField?.focus(); reportField?.select(); }} class="text-xs text-slate-600 dark:text-zinc-400 underline underline-offset-2">全选报告</button>
                </div>
              {/if}
            </div>
          {:else if running}
            <p role="status" class="col-start-1 row-start-1 text-sm text-slate-500 dark:text-zinc-400"
              in:fade={{ duration: 140 }} out:fade={{ duration: 100 }}>正在执行检查，请稍候…</p>
          {:else if error}
            <p role="alert" class="col-start-1 row-start-1 break-words rounded-lg border border-red-300 dark:border-red-900/50 bg-red-50 dark:bg-red-950/30 p-3 text-xs text-red-700 dark:text-red-300 [overflow-wrap:anywhere]"
              in:fade={{ duration: 180 }} out:fade={{ duration: 100 }}>运行诊断失败：{error}</p>
          {/if}
        </div>
      </div>
    </div>
  {/if}
</section>
