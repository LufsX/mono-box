<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { Download, RefreshCw, ChevronRight, ScrollText, X } from "@lucide/svelte";
  import { fade, fly, scale } from "svelte/transition";
  import { cubicOut } from "svelte/easing";
  import Select from "$lib/components/common/Select.svelte";
  import Markdown from "$lib/components/common/Markdown.svelte";
  import { useModalHistory } from "$lib/modal-history";

  import { fetchUpdateChangelog, fetchUpdateInfo, getModuleAbi, getUpdateUrls, type UpdateInfo, type UpdateSource } from "$lib/api/module-update";

  type UpdateStatus = "idle" | "checking" | "ok" | "error";

  let { currentVersion, currentVersionCode, currentUpdateJson, currentModuleAbi = "", moduleInfoLoading = false, moduleInfoError = "", onreloadmodule, onopen } = $props<{
    currentVersion: string;
    currentVersionCode: string;
    currentUpdateJson: string;
    currentModuleAbi?: string;
    moduleInfoLoading?: boolean;
    moduleInfoError?: string;
    onreloadmodule: () => void | Promise<void>;
    onopen: (url: string) => void | Promise<void>;
  }>();

  const STORAGE_KEY = "mono-box.update-source";

  let updateSource = $state<UpdateSource>("current");
  let updateStatus = $state<UpdateStatus>("idle");
  let updateError = $state("");
  let updateInfo = $state<UpdateInfo | null>(null);
  let changelogLoading = $state(false);
  let changelogError = $state("");
  let changelogText = $state("");
  let changelogOpen = $state(false);
  let updateRequest = 0;
  let changelogRequest = 0;
  let updateController: AbortController | undefined;
  let changelogController: AbortController | undefined;
  const modalHistory = useModalHistory("about-changelog", () => {
    changelogOpen = false;
  });

  const updateSourceOptions = [
    { value: "current", label: "当前通道" },
    { value: "release", label: "正式版" },
    { value: "nightly", label: "每夜版" },
  ];

  const sourceUrls = $derived(getUpdateUrls(updateSource, currentUpdateJson, currentModuleAbi));
  const sourceUrl = $derived(sourceUrls[0]);
  const installedAbi = $derived(getModuleAbi(currentUpdateJson, currentModuleAbi));
  const currentCode = $derived(Number.parseInt(currentVersionCode || "0", 10) || 0);
  const hasUpdate = $derived(updateInfo ? updateInfo.versionCode > currentCode : false);
  const actionIsDownload = $derived(!moduleInfoError && updateStatus === "ok" && hasUpdate && !!updateInfo?.zipUrl);
  const displayVersion = $derived(currentVersion || "-");
  const displayChangelog = $derived(changelogText.trim());
  const hasChangelogSource = $derived(!!updateInfo?.changelog?.trim());
  const displayMeta = $derived.by(() => {
    if (moduleInfoLoading) return "正在读取模块信息";
    if (moduleInfoError) return `读取模块信息失败: ${moduleInfoError}`;
    if (updateStatus === "checking") return "正在检查更新";
    if (updateStatus === "error") return updateError || "检查更新失败";
    if (updateStatus === "ok" && updateInfo) {
      if (hasUpdate) return `发现可用更新 ${updateInfo.version}(${updateInfo.versionCode})`;
      return `当前已是最新 (${currentVersionCode || "-"})`;
    }

    return `当前版本 ${currentVersionCode || "-"}`;
  });

  onMount(() => {
    try {
      const saved = localStorage.getItem(STORAGE_KEY);
      if (saved === "current" || saved === "release" || saved === "nightly") {
        updateSource = saved;
      }
    } catch {
      // Ignore storage failures in restricted WebViews.
    }
  });

  onDestroy(cancelRequests);

  function cancelRequests() {
    updateRequest += 1;
    changelogRequest += 1;
    updateController?.abort();
    changelogController?.abort();
    updateController = undefined;
    changelogController = undefined;
  }

  function saveUpdateSource(value: string) {
    cancelRequests();
    updateSource = value === "nightly" ? "nightly" : value === "release" ? "release" : "current";
    updateStatus = "idle";
    updateInfo = null;
    updateError = "";
    changelogLoading = false;
    changelogError = "";
    changelogText = "";
    changelogOpen = false;

    try {
      localStorage.setItem(STORAGE_KEY, updateSource);
    } catch {
      // Ignore storage failures in restricted WebViews.
    }
  }

  function isHttpUrl(value: string): boolean {
    return /^https?:\/\//i.test(value);
  }

  async function loadChangelog(info: UpdateInfo) {
    const request = ++changelogRequest;
    changelogController?.abort();
    changelogController = undefined;
    const source = info.changelog?.trim() || "";
    changelogLoading = false;
    changelogError = "";
    changelogText = "";

    if (!source) return;

    if (!isHttpUrl(source)) {
      changelogText = source;
      return;
    }

    const controller = new AbortController();
    changelogController = controller;
    try {
      changelogLoading = true;
      const text = await fetchUpdateChangelog(source, controller.signal);
      if (request !== changelogRequest || updateInfo !== info) return;
      changelogText = text;
    } catch (e) {
      if (request !== changelogRequest || updateInfo !== info) return;
      changelogError = e instanceof Error ? e.message : String(e);
    } finally {
      if (request === changelogRequest) changelogLoading = false;
      if (changelogController === controller) changelogController = undefined;
    }
  }

  function openChangelog() {
    if (!updateInfo || !hasChangelogSource) return;
    if (!changelogOpen) modalHistory.push();
    changelogOpen = true;

    if (!changelogLoading && !changelogText && !changelogError) {
      void loadChangelog(updateInfo);
    }
  }

  function closeChangelog() {
    modalHistory.close();
  }

  async function checkUpdate() {
    if (moduleInfoLoading || moduleInfoError) return;
    cancelRequests();
    const request = updateRequest;
    const controller = new AbortController();
    updateController = controller;
    try {
      updateStatus = "checking";
      updateError = "";
      updateInfo = null;
      changelogLoading = false;
      changelogError = "";
      changelogText = "";
      changelogOpen = false;

      const info = await fetchUpdateInfo(sourceUrls, installedAbi, controller.signal);
      if (request !== updateRequest) return;
      updateInfo = info;
      updateStatus = "ok";
      void loadChangelog(updateInfo);
    } catch (e) {
      if (request !== updateRequest) return;
      updateError = e instanceof Error ? e.message : String(e);
      updateStatus = "error";
      changelogLoading = false;
      changelogOpen = false;
    } finally {
      if (updateController === controller) updateController = undefined;
    }
  }

  async function handleAction() {
    if (moduleInfoLoading) return;
    if (moduleInfoError) {
      await onreloadmodule();
      return;
    }
    if (actionIsDownload && updateInfo?.zipUrl) {
      await onopen(updateInfo.zipUrl);
      return;
    }

    await checkUpdate();
  }
</script>

<div class="p-4 space-y-4">
  <div class="flex items-center justify-between gap-4">
    <div class="min-w-0 flex-1">
      <p class="truncate font-bold text-slate-900 dark:text-slate-200 text-base">
        Mono Box {displayVersion}
      </p>
      <p
        class="mt-1 truncate text-xs {updateStatus === 'error' || moduleInfoError ? 'text-red-600 dark:text-red-400' : hasUpdate ? 'text-emerald-600 dark:text-emerald-400' : 'text-slate-500 dark:text-slate-400'}"
        title={displayMeta}
      >
        {displayMeta}
      </p>
    </div>

    <div class="flex shrink-0 items-center gap-0">
      <div
        class="overflow-hidden transition-[width,opacity,transform,margin] duration-300 ease-out {updateStatus === 'ok' && hasChangelogSource
          ? 'mr-2 w-9 translate-x-0 opacity-100'
          : 'mr-0 w-0 translate-x-2 opacity-0 pointer-events-none'}"
        aria-hidden={!(updateStatus === "ok" && hasChangelogSource)}
      >
        <button
          type="button"
          class="inline-flex h-9 w-9 items-center justify-center border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-700 dark:text-zinc-300 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-all duration-300 outline-none rounded-lg"
          onclick={openChangelog}
          disabled={!(updateStatus === "ok" && hasChangelogSource)}
          title="更新日志"
          aria-label="更新日志"
        >
          <ScrollText size={15} />
        </button>
      </div>

      <button
        type="button"
        onclick={handleAction}
        disabled={moduleInfoLoading || updateStatus === "checking"}
        class="group relative h-9 overflow-hidden shrink-0 whitespace-nowrap border text-sm font-bold transition-[width,background-color,border-color,color,opacity,transform] duration-300 outline-none disabled:opacity-70 text-center {actionIsDownload
          ? 'w-9 px-0 border-emerald-700 dark:border-emerald-400 bg-emerald-700 dark:bg-emerald-300 text-white dark:text-zinc-900 shadow-sm'
          : 'w-26 px-3 border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 hover:bg-slate-700 dark:hover:bg-slate-300 shadow-sm'} rounded-lg"
        title={actionIsDownload ? "下载更新" : moduleInfoError ? "重试读取模块信息" : "检查更新"}
        aria-label={actionIsDownload ? "下载更新" : moduleInfoError ? "重试读取模块信息" : "检查更新"}
      >
        {#key actionIsDownload}
          <div class="flex h-full items-center justify-center gap-1.5 min-w-0" in:fly={{ x: actionIsDownload ? 8 : -8, duration: 160, easing: cubicOut }} out:fade={{ duration: 80 }}>
            {#if actionIsDownload}
              <Download size={15} class="shrink-0" />
            {:else}
              <RefreshCw size={14} class="shrink-0 {moduleInfoLoading || updateStatus === 'checking' ? 'animate-spin' : ''}" />
              <span class="truncate">{moduleInfoLoading ? "正在读取" : moduleInfoError ? "重试" : updateStatus === "checking" ? "正在检查" : "检查更新"}</span>
            {/if}
          </div>
        {/key}
      </button>
    </div>
  </div>

  <div class="h-px bg-slate-200 dark:bg-zinc-800"></div>

  <div class="flex flex-col gap-2">
    <label for="update-source" class="text-sm font-bold text-slate-900 dark:text-slate-200">更新通道</label>
    <div title={sourceUrl}>
      <Select id="update-source" bind:value={updateSource} options={updateSourceOptions} onchange={saveUpdateSource} />
    </div>
    <span class="text-xs text-slate-500 dark:text-slate-400">
      {updateSource === "current" ? "使用现有已安装模块的通道" : updateSource === "release" ? "获取最新的正式版本" : "获取自动构建的最新版本"}
    </span>
  </div>

  <div class="h-px bg-slate-200 dark:bg-zinc-800"></div>

  <button type="button" class="w-full flex items-center justify-between gap-3 text-left group outline-none" onclick={() => onopen("https://github.com/LufsX/mono-box")}>
    <div class="space-y-1">
      <p class="font-bold text-slate-900 dark:text-slate-200 text-sm group-hover:text-slate-700 dark:group-hover:text-slate-100 transition-colors">项目主页</p>
      <p class="text-xs text-slate-500 dark:text-slate-400">基于 Mihomo 核心的代理工具，在 GitHub 上查看源码或提交问题</p>
    </div>
    <ChevronRight size={18} class="text-slate-400 dark:text-zinc-500 shrink-0 group-hover:translate-x-0.5 transition-transform" aria-hidden="true" />
  </button>
</div>

{#if changelogOpen}
  <div
    class="fixed inset-0 z-50 bg-slate-950/55 p-3 md:p-6 flex items-center justify-center"
    role="button"
    tabindex="0"
    in:fade={{ duration: 180 }}
    out:fade={{ duration: 140 }}
    onclick={(event) => {
      if (event.target === event.currentTarget) closeChangelog();
    }}
    onkeydown={(event) => {
      if (event.key === "Escape" || (event.target === event.currentTarget && (event.key === "Enter" || event.key === " "))) {
        event.preventDefault();
        closeChangelog();
      }
    }}
  >
    <div
      class="mx-auto w-full flex max-h-[72dvh] max-w-2xl flex-col border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 rounded-xl overflow-hidden"
      role="dialog"
      aria-modal="true"
      aria-labelledby="update-changelog-title"
      in:fly={{ y: 10, duration: 220, easing: cubicOut }}
      out:scale={{ duration: 150, easing: cubicOut, start: 0.98 }}
    >
      <div class="flex items-center justify-between gap-3 px-4 py-3 border-b border-slate-300 dark:border-zinc-700 shrink-0">
        <div class="min-w-0">
          <div id="update-changelog-title" class="text-sm font-bold text-slate-900 dark:text-slate-100 truncate">更新日志</div>
          {#if updateInfo?.version}
            <div class="mt-0.5 font-mono text-[11px] text-slate-500 dark:text-zinc-500">{updateInfo.version} ({updateInfo.versionCode})</div>
          {/if}
        </div>
        <button
          class="p-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-zinc-300 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors rounded-lg"
          onclick={closeChangelog}
        >
          <X size={14} />
        </button>
      </div>

      <div class="p-4 overflow-y-auto flex-1">
        {#key changelogLoading ? "loading" : changelogError ? "error" : displayChangelog ? "content" : "empty"}
          <div in:fly={{ y: 6, duration: 160, easing: cubicOut }} out:fade={{ duration: 90 }}>
            {#if changelogLoading}
              <div class="py-12 text-center text-sm text-slate-500 dark:text-zinc-400">正在加载更新日志...</div>
            {:else if changelogError}
              <div class="border border-red-300 dark:border-red-900/50 bg-red-50 dark:bg-red-950/30 px-3 py-3 text-xs text-red-700 dark:text-red-300 rounded-lg">
                无法加载更新日志: {changelogError}
              </div>
            {:else if displayChangelog}
              <Markdown source={changelogText} baseUrl={isHttpUrl(updateInfo?.changelog || "") ? updateInfo?.changelog : undefined} {onopen} />
            {:else}
              <div class="py-12 text-center text-sm text-slate-500 dark:text-zinc-400">暂无更新日志</div>
            {/if}
          </div>
        {/key}
      </div>
    </div>
  </div>
{/if}
