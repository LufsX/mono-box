<script lang="ts">
  import { onMount } from "svelte";
  import { Download, RefreshCw } from "@lucide/svelte";
  import Select from "$lib/components/Select.svelte";
  import { roundedStore } from "$lib/settings";

  type UpdateSource = "release" | "nightly";
  type UpdateStatus = "idle" | "checking" | "ok" | "error";

  interface UpdateInfo {
    version: string;
    versionCode: number;
    zipUrl: string;
    changelog?: string;
  }

  let { currentVersion, currentVersionCode, releaseUpdateJson, onopen } = $props<{
    currentVersion: string;
    currentVersionCode: string;
    releaseUpdateJson: string;
    onopen: (url: string) => void | Promise<void>;
  }>();

  const r = $derived($roundedStore);
  const STORAGE_KEY = "mono-box.update-source";
  const NIGHTLY_UPDATE_JSON = "https://cors.isteed.cc/https://github.com/LufsX/mono-box/releases/download/Prerelease/update.json";
  const FALLBACK_RELEASE_UPDATE_JSON = "https://cors.isteed.cc/https://github.com/LufsX/mono-box/releases/latest/download/update.json";

  let updateSource = $state<UpdateSource>("release");
  let updateStatus = $state<UpdateStatus>("idle");
  let updateError = $state("");
  let updateInfo = $state<UpdateInfo | null>(null);

  const updateSourceOptions = [
    { value: "release", label: "正式版" },
    { value: "nightly", label: "每夜版" },
  ];

  const sourceUrl = $derived(updateSource === "nightly" ? NIGHTLY_UPDATE_JSON : releaseUpdateJson || FALLBACK_RELEASE_UPDATE_JSON);
  const currentCode = $derived(Number.parseInt(currentVersionCode || "0", 10) || 0);
  const hasUpdate = $derived(updateInfo ? updateInfo.versionCode > currentCode : false);
  const actionIsDownload = $derived(updateStatus === "ok" && hasUpdate && !!updateInfo?.zipUrl);
  const displayVersion = $derived(actionIsDownload && updateInfo ? updateInfo.version : currentVersion || "-");
  const displayMeta = $derived.by(() => {
    if (updateStatus === "checking") return "正在检查更新";
    if (updateStatus === "error") return updateError || "检查更新失败";
    if (updateStatus === "ok" && updateInfo) {
      if (hasUpdate) return `发现可用更新 (${updateInfo.versionCode})`;
      return `当前已是最新 (${currentVersionCode || "-"})`;
    }

    return `当前版本 ${currentVersionCode || "-"}`;
  });

  onMount(() => {
    try {
      const saved = localStorage.getItem(STORAGE_KEY);
      if (saved === "release" || saved === "nightly") {
        updateSource = saved;
      }
    } catch {
      // Ignore storage failures in restricted WebViews.
    }
  });

  function saveUpdateSource(value: string) {
    updateSource = value === "nightly" ? "nightly" : "release";
    updateStatus = "idle";
    updateInfo = null;
    updateError = "";

    try {
      localStorage.setItem(STORAGE_KEY, updateSource);
    } catch {
      // Ignore storage failures in restricted WebViews.
    }
  }

  function normalizeUpdateInfo(raw: unknown): UpdateInfo {
    const data = raw as Partial<UpdateInfo>;
    const versionCode = Number(data.versionCode);
    const version = typeof data.version === "string" ? data.version : "";
    const zipUrl = typeof data.zipUrl === "string" ? data.zipUrl : "";
    const changelog = typeof data.changelog === "string" ? data.changelog : "";

    if (!version || !Number.isFinite(versionCode) || !zipUrl) {
      throw new Error("update.json 格式无效");
    }

    return {
      version,
      versionCode,
      zipUrl,
      changelog,
    };
  }

  async function checkUpdate() {
    try {
      updateStatus = "checking";
      updateError = "";
      updateInfo = null;

      const response = await fetch(sourceUrl, {
        cache: "no-store",
      });

      if (!response.ok) {
        throw new Error(`请求失败: HTTP ${response.status}`);
      }

      updateInfo = normalizeUpdateInfo(await response.json());
      updateStatus = "ok";
    } catch (e) {
      updateError = e instanceof Error ? e.message : String(e);
      updateStatus = "error";
    }
  }

  async function handleAction() {
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
        class="mt-1 truncate text-xs {updateStatus === 'error' ? 'text-red-600 dark:text-red-400' : hasUpdate ? 'text-emerald-600 dark:text-emerald-400' : 'text-slate-500 dark:text-slate-400'}"
        title={displayMeta}
      >
        {displayMeta}
      </p>
    </div>

    <button
      type="button"
      onclick={handleAction}
      disabled={updateStatus === "checking"}
      class="group relative overflow-hidden shrink-0 whitespace-nowrap min-w-24 border px-3 py-2 text-sm font-bold transition-all duration-300 outline-none disabled:opacity-70 text-center {actionIsDownload
        ? 'border-emerald-700 dark:border-emerald-400 bg-emerald-700 dark:bg-emerald-300 text-white dark:text-zinc-900 shadow-sm'
        : 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 hover:bg-slate-700 dark:hover:bg-slate-300 shadow-sm'} {r ? 'rounded-lg' : ''}"
    >
      <div class="flex items-center justify-center gap-1.5 min-w-0">
        {#if actionIsDownload}
          <Download size={14} class="shrink-0" />
          <span class="truncate">下载更新</span>
        {:else}
          <RefreshCw size={14} class="shrink-0 {updateStatus === 'checking' ? 'animate-spin' : ''}" />
          <span class="truncate">{updateStatus === "checking" ? "检查中" : "检查更新"}</span>
        {/if}
      </div>
    </button>
  </div>

  <div class="h-px bg-slate-200 dark:bg-zinc-800"></div>

  <div class="flex flex-col gap-2">
    <label for="update-source" class="text-sm font-bold text-slate-900 dark:text-slate-200">更新通道</label>
    <div title={sourceUrl}>
      <Select id="update-source" bind:value={updateSource} options={updateSourceOptions} onchange={saveUpdateSource} />
    </div>
    <span class="text-xs text-slate-500 dark:text-slate-400">
      {updateSource === "release" ? "获取最新的正式版本发布代码，更稳定" : "获取每次提交代码后自动构建的最新版本，可能有未知的问题"}
    </span>
  </div>
</div>
