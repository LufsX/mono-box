<script lang="ts">
  import { onMount, untrack } from "svelte";
  import type { BoxConfigFormValues } from "$lib/api";
  import CheckBox from "$lib/components/common/CheckBox.svelte";
  import { getAutomationState, type AutomationState } from "$lib/api/maintenance";
  import { getErrorMessage } from "$lib/api/error-utils";

  let { boxConfig = $bindable<BoxConfigFormValues>(), disabled = false } = $props<{
    boxConfig: BoxConfigFormValues;
    disabled?: boolean;
  }>();
  let container: HTMLDivElement;
  let automationState = $state<AutomationState | null>(null);
  let loading = false;
  let statusError = $state("");
  let mounted = false;
  let inView = $state(false);
  let documentVisible = $state(false);
  const canRefresh = $derived(!disabled && inView && documentVisible);

  const recoveryNote = $derived.by(() => {
    if (!automationState?.settings.recoveryEnabled) return "";
    const recovery = automationState.recovery;
    if (recovery.state === "paused") return `恢复已暂停 · 已尝试 ${recovery.attempts} 次`;
    if (recovery.state === "recovering") return `正在恢复 · 第 ${recovery.attempts} 次`;
    if (recovery.nextAttemptAt > 0) return `下次重试 ${formatTime(recovery.nextAttemptAt)}`;
    return recovery.lastError ? "恢复失败，请检查内核和配置" : "";
  });
  const updateNote = $derived.by(() => {
    if (!automationState) return "";
    const update = automationState.update;
    const parts: string[] = [];
    if (update.state === "updating") parts.push("正在更新");
    else if (update.state === "waiting_wifi") parts.push("等待 Wi-Fi");
    else if (update.lastError) parts.push(/cancelled|interrupted/.test(update.lastError) ? "更新已中断" : "更新失败");
    else if (update.lastSuccessAt > 0) parts.push(`已更新 ${update.updated} 个 · ${formatTime(update.lastSuccessAt)}`);
    if (automationState.settings.updateEnabled && update.nextRunAt > 0) {
      parts.push(`下次 ${formatTime(update.nextRunAt)}`);
    }
    return parts.join(" · ");
  });

  function formatTime(value: number): string {
    const date = new Date(value);
    return value > 0 && Number.isFinite(date.getTime())
      ? date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false })
      : "";
  }

  async function refresh() {
    if (!mounted || !canRefresh || loading) return;
    loading = true;
    try {
      const next = await getAutomationState();
      if (!mounted) return;
      automationState = next;
      statusError = "";
    } catch (error) {
      if (mounted) statusError = getErrorMessage(error);
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    if (canRefresh) untrack(() => void refresh());
  });

  onMount(() => {
    mounted = true;
    documentVisible = document.visibilityState === "visible";
    const onVisibility = () => {
      documentVisible = document.visibilityState === "visible";
    };
    document.addEventListener("visibilitychange", onVisibility);
    const observer = typeof IntersectionObserver === "undefined" ? undefined : new IntersectionObserver(([entry]) => {
      inView = entry.isIntersecting;
    });
    if (observer) observer.observe(container);
    else inView = true;
    const timer = window.setInterval(() => void refresh(), 10_000);
    return () => {
      mounted = false;
      observer?.disconnect();
      clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  });
</script>

<div bind:this={container} class="space-y-4">
  <div class="flex min-w-0 flex-col gap-2">
    <CheckBox id="automation-recovery" checked={boxConfig.autoRecovery} {disabled}
      onchange={() => { boxConfig.autoRecovery = !boxConfig.autoRecovery; }} label="异常退出自动恢复" />
    <p class="flex h-4 items-center gap-2 overflow-hidden text-xs leading-4 text-slate-500 dark:text-slate-400">
      <span class="shrink-0">仅恢复内核异常退出</span>
      <span class="min-w-0 truncate" title={automationState?.recovery.lastError || recoveryNote}
        aria-live="polite">{recoveryNote || "手动停止不恢复"}</span>
    </p>
  </div>

  <div class="flex min-w-0 flex-col gap-2">
    <CheckBox id="automation-update" checked={boxConfig.autoUpdateConfig} {disabled}
      onchange={() => { boxConfig.autoUpdateConfig = !boxConfig.autoUpdateConfig; }} label="定时更新配置" />
    <p class="flex h-4 items-center gap-2 overflow-hidden text-xs leading-4 text-slate-500 dark:text-slate-400">
      <span class="shrink-0">校验后更新远程配置</span>
      <span class="min-w-0 truncate"
        title={statusError || (automationState?.update.lastError ? `${updateNote} · ${automationState.update.lastError}` : updateNote)}
        aria-live="polite">{statusError ? "运行记录读取失败" : updateNote || "下次启动生效"}</span>
    </p>
  </div>

  <div class="flex min-w-0 flex-col gap-2">
    <label for="automation-interval" class="text-sm font-bold text-slate-900 dark:text-slate-200">更新间隔（小时）</label>
    <input id="automation-interval" type="number" min="1" max="168" step="1" required bind:value={boxConfig.configUpdateIntervalHours} {disabled}
      aria-describedby="automation-interval-description"
      class="min-w-0 px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors disabled:opacity-50 rounded-lg" />
    <span id="automation-interval-description" class="text-xs text-slate-500 dark:text-slate-400">每隔指定小时更新一次，可设 1–168 小时</span>
  </div>

  <div class="flex min-w-0 flex-col gap-2">
    <CheckBox id="automation-wifi" checked={boxConfig.configUpdateWifiOnly} {disabled}
      onchange={() => { boxConfig.configUpdateWifiOnly = !boxConfig.configUpdateWifiOnly; }} label="仅在 Wi-Fi 下更新" />
    <span class="text-xs text-slate-500 dark:text-slate-400">到期后等待 Wi-Fi 连接再更新</span>
  </div>
</div>
