<script lang="ts">
  import { onMount, untrack } from "svelte";
  import { Check, RefreshCw, Save } from "@lucide/svelte";
  import CheckBox from "$lib/components/common/CheckBox.svelte";
  import {
    defaultAutomationSettings,
    getAutomationState,
    setAutomationSettings,
    validateAutomationSettings,
    type AutomationSettings,
    type AutomationState,
  } from "$lib/api/maintenance";
  import { getErrorMessage } from "$lib/api/error-utils";

  let { visible = true } = $props<{ visible?: boolean }>();
  let container: HTMLFormElement;
  let automationState = $state<AutomationState | null>(null);
  let draft = $state<AutomationSettings>(defaultAutomationSettings());
  let intervalHours = $state<number | undefined>(24);
  let loading = $state(false);
  let saving = $state(false);
  let loadError = $state("");
  let saveError = $state("");
  let saved = $state(false);
  let mounted = false;
  let inView = $state(false);
  let documentVisible = $state(false);
  const canRefresh = $derived(visible && inView && documentVisible);
  const dirty = $derived(automationState !== null && (
    draft.recoveryEnabled !== automationState.settings.recoveryEnabled ||
    draft.updateEnabled !== automationState.settings.updateEnabled ||
    draft.updateWifiOnly !== automationState.settings.updateWifiOnly ||
    intervalHours !== automationState.settings.updateIntervalHours
  ));

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
  const feedback = $derived(saveError || loadError);
  const retryRead = $derived(!!loadError && !saveError);

  function formatTime(value: number): string {
    const date = new Date(value);
    return value > 0 && Number.isFinite(date.getTime())
      ? date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false })
      : "";
  }

  function markEdited() {
    saved = false;
    saveError = "";
  }

  function applySettings(settings: AutomationSettings) {
    draft = { ...settings };
    intervalHours = settings.updateIntervalHours;
  }

  async function refresh() {
    if (!mounted || !canRefresh || loading || saving) return;
    loading = true;
    try {
      const next = await getAutomationState();
      if (!mounted) return;
      // Check at completion so edits made during this request are preserved.
      if (!automationState || !dirty) applySettings(next.settings);
      automationState = next;
      loadError = "";
    } catch (error) {
      if (mounted) loadError = getErrorMessage(error);
    } finally {
      if (mounted) loading = false;
    }
  }

  async function save() {
    if (!mounted || !automationState || saving || loading) return;
    saveError = "";
    saved = false;
    let settings: AutomationSettings;
    try {
      settings = validateAutomationSettings({ ...draft, updateIntervalHours: intervalHours as number });
    } catch (error) {
      saveError = getErrorMessage(error);
      return;
    }
    saving = true;
    try {
      const next = await setAutomationSettings(settings);
      if (!mounted) return;
      automationState = next;
      applySettings(next.settings);
      loadError = "";
      saved = true;
    } catch (error) {
      if (mounted) saveError = getErrorMessage(error);
    } finally {
      if (mounted) saving = false;
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

<form bind:this={container} class="p-4 space-y-4" aria-busy={!automationState || saving} onsubmit={(event) => { event.preventDefault(); void save(); }}>
  <fieldset disabled={!automationState || saving} class="m-0 min-w-0 space-y-4 border-0 p-0">
    <div class="flex min-w-0 flex-col gap-2">
      <CheckBox id="automation-recovery" checked={draft.recoveryEnabled} disabled={!automationState || saving}
        onchange={() => { draft.recoveryEnabled = !draft.recoveryEnabled; markEdited(); }} label="异常退出自动恢复" />
      <p class="flex h-4 items-center gap-2 overflow-hidden text-xs leading-4 text-slate-500 dark:text-slate-400">
        <span class="shrink-0">仅恢复内核异常退出</span>
        <span class="min-w-0 truncate" title={automationState?.recovery.lastError || recoveryNote}
          aria-live="polite">{recoveryNote || "手动停止不恢复"}</span>
      </p>
    </div>

    <div class="flex min-w-0 flex-col gap-2">
      <CheckBox id="automation-update" checked={draft.updateEnabled} disabled={!automationState || saving}
        onchange={() => { draft.updateEnabled = !draft.updateEnabled; markEdited(); }} label="定时更新配置" />
      <p class="flex h-4 items-center gap-2 overflow-hidden text-xs leading-4 text-slate-500 dark:text-slate-400">
        <span class="shrink-0">校验后更新远程配置</span>
        <span class="min-w-0 truncate"
          title={automationState?.update.lastError ? `${updateNote} · ${automationState.update.lastError}` : updateNote}
          aria-live="polite">{updateNote || "下次启动生效"}</span>
      </p>
    </div>

    <div class="flex min-w-0 flex-col gap-2">
      <label for="automation-interval" class="text-sm font-bold text-slate-900 dark:text-slate-200">更新间隔（小时）</label>
      <input id="automation-interval" type="number" min="1" max="168" step="1" required bind:value={intervalHours} oninput={markEdited}
        aria-describedby="automation-interval-description"
        class="min-w-0 px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors disabled:opacity-50 rounded-lg" />
      <span id="automation-interval-description" class="text-xs text-slate-500 dark:text-slate-400">每隔指定小时更新一次，可设 1–168 小时</span>
    </div>

    <div class="flex min-w-0 flex-col gap-2">
      <CheckBox id="automation-wifi" checked={draft.updateWifiOnly} disabled={!automationState || saving}
        onchange={() => { draft.updateWifiOnly = !draft.updateWifiOnly; markEdited(); }} label="仅在 Wi-Fi 下更新" />
      <span class="text-xs text-slate-500 dark:text-slate-400">到期后等待 Wi-Fi 连接再更新</span>
    </div>
  </fieldset>

  <div class="flex gap-3 pt-2">
    <button type={retryRead ? "button" : "submit"}
      onclick={() => { if (retryRead) void refresh(); }}
      disabled={saving || loading || (!automationState && !retryRead) || (!!automationState && !dirty && !retryRead && !saveError)}
      title={feedback || undefined}
      aria-describedby={feedback ? "automation-feedback" : undefined}
      class="group relative overflow-hidden flex-1 border border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 px-4 py-2.5 text-sm font-bold transition-all duration-300 outline-none hover:bg-slate-700 dark:hover:bg-slate-300 hover:shadow-[0_8px_20px_rgba(15,23,42,0.15)] active:translate-y-px disabled:cursor-wait disabled:opacity-60 rounded-lg">
      <div class="grid place-items-center">
        <span class="flex items-center gap-2 col-start-1 row-start-1"
          class:text-emerald-400={saved && !dirty && !feedback} class:dark:text-emerald-600={saved && !dirty && !feedback}>
          {#if saving || retryRead}
            <RefreshCw size={16} class={saving || loading ? "animate-spin" : ""} />
          {:else if saved && !dirty}
            <Check size={16} />
          {:else}
            <Save size={16} />
          {/if}
          {saving ? "正在保存" : retryRead ? "读取失败，点击重试" : saveError ? "保存失败，点击重试" : saved && !dirty ? "保存成功" : "保存自动化设置"}
        </span>
      </div>
    </button>
    <span id="automation-feedback" class="sr-only" aria-live="polite">{feedback}</span>
  </div>
</form>
