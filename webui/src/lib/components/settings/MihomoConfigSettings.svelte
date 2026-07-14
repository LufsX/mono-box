<script lang="ts">
  import { onDestroy, onMount, tick } from "svelte";
  import { flip } from "svelte/animate";
  import { cubicOut, quintOut } from "svelte/easing";
  import { fade, fly, slide } from "svelte/transition";
  import { Check, CloudDownload, FileText, FileUp, Link, Plus, Power, RefreshCw, RotateCw, Search, Trash2, Upload, X } from "@lucide/svelte";
  import { actionApi, type MihomoConfigFile } from "$lib/api";
  import { useModalHistory } from "$lib/modal-history";
  import { formatBytes } from "$lib/utils";
  import NoticeBanner from "$lib/components/common/NoticeBanner.svelte";

  type AddMode = "url" | "local";
  type Banner = { tone: "success" | "error"; message: string };
  type PendingAction = { kind: "switch" | "delete"; config: MihomoConfigFile };
  type ConfigViewState = "loading" | "empty" | "no-results" | "list";

  const MAX_LOCAL_CONFIG_BYTES = 5 * 1024 * 1024;
  const MODAL_EXIT_MS = 160;

  let configs = $state<MihomoConfigFile[]>([]);
  let loaded = $state(false);
  let loading = $state(false);
  let busyKey = $state("");
  let banner = $state<Banner | null>(null);
  let bannerResetTimer: ReturnType<typeof setTimeout> | undefined;
  let searchQuery = $state("");
  let configContentHeight = $state(0);

  let addModalOpen = $state(false);
  let addMode = $state<AddMode>("url");
  let modalError = $state("");
  let sourceUrl = $state("");
  let sourceName = $state("");
  let urlInput: HTMLInputElement | undefined = $state();
  let fileInput: HTMLInputElement | undefined = $state();
  let fileDragging = $state(false);
  let urlPanelHeight = $state(0);
  let localPanelHeight = $state(0);

  let pendingAction = $state<PendingAction | null>(null);
  let actionError = $state("");

  let updateAllDone = $state(0);
  let updateAllTotal = $state(0);
  let updatingName = $state("");

  let modalExitWork: (() => void) | null = null;
  let modalExitTimer: ReturnType<typeof setTimeout> | undefined;

  const busy = $derived(loading || !!busyKey);
  const remoteConfigs = $derived(configs.filter((config) => !!config.sourceUrl));
  const activeAddPanelHeight = $derived(addMode === "url" ? urlPanelHeight : localPanelHeight);
  const normalizedProposedName = $derived(normalizeProfileName(sourceName.trim() || (addMode === "url" && sourceUrl.trim() ? inferNameFromUrl(sourceUrl.trim()) : "")));
  const duplicateConfig = $derived(normalizedProposedName ? (configs.find((config) => config.kind === "profile" && config.name === normalizedProposedName) ?? null) : null);
  const visibleConfigs = $derived.by(() => {
    const query = searchQuery.trim().toLowerCase();
    return [...configs]
      .sort((left, right) => (left.kind === right.kind ? left.name.localeCompare(right.name) : left.kind === "current" ? -1 : 1))
      .filter((config) => {
        if (!query) return true;
        return config.name.toLowerCase().includes(query) || config.sourceUrl.toLowerCase().includes(query);
      });
  });
  const configViewState = $derived<ConfigViewState>(!loaded ? "loading" : configs.length === 0 ? "empty" : visibleConfigs.length === 0 ? "no-results" : "list");

  const addModalHistory = useModalHistory("mihomo-config-add", resetAddModal);
  const actionModalHistory = useModalHistory("mihomo-config-action", resetActionModal);

  onMount(() => {
    void loadConfigs();
  });

  onDestroy(() => {
    if (bannerResetTimer) clearTimeout(bannerResetTimer);
    if (modalExitTimer) clearTimeout(modalExitTimer);
  });

  function isCoarsePointer(): boolean {
    return typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia("(pointer: coarse)").matches;
  }

  // 把列表更新与 toast 推迟到弹窗退出动画结束后，避免同帧竞争导致掉帧。
  function queueAfterModalExit(run: () => void) {
    modalExitWork = run;
  }

  function flushModalExitWork() {
    const run = modalExitWork;
    if (!run) return;
    modalExitWork = null;
    if (modalExitTimer) clearTimeout(modalExitTimer);
    modalExitTimer = setTimeout(() => {
      modalExitTimer = undefined;
      run();
    }, MODAL_EXIT_MS);
  }

  function normalizeProfileName(value: string): string {
    if (!value) return "";
    const rawName = value.split(/[\\/]/).filter(Boolean).pop() || "config";
    let name = rawName.replace(/[^A-Za-z0-9._-]+/g, "_").replace(/^_+|_+$/g, "");
    if (!name || name === "." || name === "..") name = "config";
    if (!/\.ya?ml$/i.test(name)) name = `${name}.yaml`;
    return name;
  }

  function clearErrorBanner() {
    if (banner?.tone !== "error") return;
    banner = null;
  }

  function setSuccess(message: string) {
    if (bannerResetTimer) clearTimeout(bannerResetTimer);
    banner = { tone: "success", message };
    bannerResetTimer = setTimeout(() => {
      banner = null;
      bannerResetTimer = undefined;
    }, 2800);
  }

  function setFailure(message: string) {
    if (bannerResetTimer) {
      clearTimeout(bannerResetTimer);
      bannerResetTimer = undefined;
    }
    banner = { tone: "error", message };
  }

  function getErrorMessage(error: unknown): string {
    const message = error instanceof Error ? error.message : String(error);
    if (message.includes("active config cannot be deleted")) return "当前选择的配置不能删除";
    if (message.includes("config validation failed")) return "配置校验未通过，请检查文件内容";
    if (message.includes("config profile not found")) return "配置文件不存在，请刷新后重试";
    if (message.includes("default config not found")) return "默认 config.yaml 不存在";
    if (message.includes("config profile has no source URL")) return "该配置没有可用的源链接";
    if (message.includes("only HTTP or HTTPS URLs are allowed")) return "仅支持 HTTP 或 HTTPS 链接";
    if (message.includes("config file is empty")) return "配置文件内容为空";
    return message;
  }

  function inferNameFromUrl(url: string): string {
    try {
      const parsed = new URL(url);
      const name = parsed.pathname.split("/").filter(Boolean).pop();
      return normalizeProfileName(name || "remote-config.yaml");
    } catch {
      return "remote-config.yaml";
    }
  }

  function formatUpdatedAt(value: number): string {
    if (!value) return "未知时间";
    return new Intl.DateTimeFormat("zh-CN", {
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    }).format(new Date(value));
  }

  function configKey(config: Pick<MihomoConfigFile, "kind" | "name">): string {
    return `${config.kind}:${config.name}`;
  }

  function applyConfigPatch(next: MihomoConfigFile) {
    let found = false;
    configs = configs.map((config) => {
      if (configKey(config) === configKey(next)) {
        found = true;
        return next;
      }
      return next.active && config.active ? { ...config, active: false } : config;
    });
    if (!found) configs = [...configs, next];
  }

  async function loadConfigs() {
    if (loading) return;
    try {
      loading = true;
      clearErrorBanner();
      configs = await actionApi.listMihomoConfigFiles();
    } catch (error) {
      setFailure(`读取配置失败：${getErrorMessage(error)}`);
    } finally {
      loading = false;
      loaded = true;
    }
  }

  async function withBusy(key: string, failurePrefix: string, run: () => Promise<void>) {
    if (busyKey) return;
    try {
      busyKey = key;
      clearErrorBanner();
      await run();
    } catch (error) {
      setFailure(`${failurePrefix}：${getErrorMessage(error)}`);
    } finally {
      busyKey = "";
    }
  }

  async function openAddModal() {
    if (addModalOpen || busy) return;
    modalError = "";
    addMode = "url";
    addModalHistory.push();
    addModalOpen = true;
    if (isCoarsePointer()) return;
    await tick();
    urlInput?.focus();
  }

  function closeAddModal() {
    if (busyKey) return;
    addModalHistory.close();
  }

  function resetAddModal() {
    flushModalExitWork();
    addModalOpen = false;
    addMode = "url";
    modalError = "";
    sourceUrl = "";
    sourceName = "";
    fileDragging = false;
    if (fileInput) fileInput.value = "";
  }

  function selectAddMode(mode: AddMode) {
    if (addMode === mode || busyKey) return;
    addMode = mode;
    modalError = "";
  }

  function handleWindowKeydown(event: KeyboardEvent) {
    if (event.key !== "Escape" || busyKey) return;
    if (pendingAction) {
      closeActionModal();
      return;
    }
    if (addModalOpen) closeAddModal();
  }

  async function addFromUrl() {
    const url = sourceUrl.trim();
    if (!url) {
      modalError = "请输入配置链接";
      return;
    }
    if (!/^https?:\/\//i.test(url)) {
      modalError = "链接需以 http:// 或 https:// 开头";
      return;
    }
    if (busyKey) return;

    try {
      busyKey = "download";
      modalError = "";
      const overwrote = !!duplicateConfig;
      const file = await actionApi.downloadMihomoConfigFromUrl(url, sourceName.trim() || inferNameFromUrl(url));
      queueAfterModalExit(() => {
        applyConfigPatch(file);
        setSuccess(overwrote ? `已更新同名配置 ${file.name}` : `已添加 ${file.name}`);
      });
      addModalHistory.close();
    } catch (error) {
      modalError = getErrorMessage(error);
    } finally {
      busyKey = "";
    }
  }

  function chooseLocalFile() {
    if (!busyKey) fileInput?.click();
  }

  async function importLocalFile(file: File) {
    if (!/\.ya?ml$/i.test(file.name)) {
      modalError = "请选择 .yaml 或 .yml 文件";
      return;
    }
    if (file.size > MAX_LOCAL_CONFIG_BYTES) {
      modalError = `文件不能超过 ${formatBytes(MAX_LOCAL_CONFIG_BYTES)}`;
      return;
    }
    if (busyKey) return;

    try {
      busyKey = "local";
      modalError = "";
      const content = await file.text();
      const saved = await actionApi.importMihomoConfigFile(sourceName.trim() || file.name, content);
      const overwrote = configs.some((config) => config.kind === "profile" && config.name === saved.name);
      queueAfterModalExit(() => {
        applyConfigPatch(saved);
        setSuccess(overwrote ? `已覆盖 ${saved.name}` : `已添加 ${saved.name}`);
      });
      addModalHistory.close();
    } catch (error) {
      modalError = getErrorMessage(error);
    } finally {
      busyKey = "";
      fileDragging = false;
      if (fileInput) fileInput.value = "";
    }
  }

  async function handleLocalFileChange(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (file) await importLocalFile(file);
  }

  async function handleLocalFileDrop(event: DragEvent) {
    event.preventDefault();
    fileDragging = false;
    const file = event.dataTransfer?.files?.[0];
    if (file) await importLocalFile(file);
  }

  async function updateConfig(config: MihomoConfigFile) {
    await withBusy(`update:${config.name}`, `更新 ${config.name} 失败`, async () => {
      applyConfigPatch(await actionApi.updateMihomoConfigFromUrl(config.name));
      setSuccess(`已更新 ${config.name}`);
    });
  }

  async function updateAllConfigs() {
    const targets = remoteConfigs;
    if (!targets.length || busyKey) return;
    busyKey = "update-all";
    updateAllDone = 0;
    updateAllTotal = targets.length;
    clearErrorBanner();
    const failed: string[] = [];

    for (const config of targets) {
      updatingName = config.name;
      try {
        applyConfigPatch(await actionApi.updateMihomoConfigFromUrl(config.name));
      } catch {
        failed.push(config.name);
      }
      updateAllDone += 1;
    }

    updatingName = "";
    busyKey = "";
    if (failed.length) {
      setFailure(`已更新 ${targets.length - failed.length} 个，${failed.length} 个失败：${failed.join("、")}`);
    } else {
      setSuccess(`已更新全部 ${targets.length} 个远程配置`);
    }
  }

  function openActionModal(kind: PendingAction["kind"], config: MihomoConfigFile) {
    if (busy || pendingAction) return;
    actionError = "";
    pendingAction = { kind, config };
    actionModalHistory.push();
  }

  function closeActionModal() {
    if (busyKey) return;
    actionModalHistory.close();
  }

  function resetActionModal() {
    flushModalExitWork();
    pendingAction = null;
    actionError = "";
  }

  async function confirmDelete() {
    if (!pendingAction || pendingAction.kind !== "delete" || busyKey) return;
    const target = pendingAction.config;
    try {
      busyKey = `delete:${target.name}`;
      actionError = "";
      await actionApi.deleteMihomoConfigFile(target.name);
      queueAfterModalExit(() => {
        configs = configs.filter((config) => configKey(config) !== configKey(target));
        setSuccess(`已删除 ${target.name}`);
      });
      actionModalHistory.close();
    } catch (error) {
      actionError = getErrorMessage(error);
    } finally {
      busyKey = "";
    }
  }

  async function confirmSwitch(restart: boolean) {
    if (!pendingAction || pendingAction.kind !== "switch" || busyKey) return;
    const target = pendingAction.config;
    try {
      busyKey = restart ? "switch:restart" : "switch:save";
      actionError = "";
      const next = await actionApi.switchMihomoConfigFile(target.name, target.kind);

      if (restart) {
        const result = await actionApi.runActionScript("restart");
        if (result.errno !== 0) {
          const detail = result.stderr.trim() || result.stdout.trim() || `errno ${result.errno}`;
          queueAfterModalExit(() => {
            applyConfigPatch(next);
            setFailure(`已选择 ${next.name}，但核心重启失败：${detail}`);
          });
          actionModalHistory.close();
          return;
        }
      }

      queueAfterModalExit(() => {
        applyConfigPatch(next);
        setSuccess(restart ? `已应用 ${next.name} 并重启核心` : `已选择 ${next.name}，下次启动时生效`);
      });
      actionModalHistory.close();
    } catch (error) {
      actionError = getErrorMessage(error);
    } finally {
      busyKey = "";
    }
  }
</script>

<svelte:window onkeydown={handleWindowKeydown} />

<div class="space-y-3 p-3 sm:p-4">
  <div class="flex items-center justify-between gap-2">
    <div class="flex min-w-0 items-center gap-2">
      <FileText size={15} class="shrink-0 text-slate-500 dark:text-zinc-400" />
      <p class="truncate text-sm font-bold text-slate-900 dark:text-slate-100">运行配置</p>
      {#if loaded}
        <span class="shrink-0 text-xs tabular-nums text-slate-500 dark:text-zinc-400" in:fade={{ duration: 180 }}>{configs.length} 个</span>
      {/if}
    </div>

    <div class="flex shrink-0 items-center gap-1.5">
      {#if remoteConfigs.length > 1}
        <button
          type="button"
          onclick={updateAllConfigs}
          disabled={busy}
          class="inline-flex h-8 items-center justify-center gap-1.5 border border-slate-300 bg-white px-2.5 text-xs font-bold text-slate-700 transition-colors hover:bg-slate-100 disabled:cursor-wait disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-950 dark:text-slate-200 dark:hover:bg-zinc-900 rounded-lg"
          title="从源链接更新全部远程配置"
          in:fly={{ y: -4, duration: 180, easing: cubicOut }}
          out:fade={{ duration: 100 }}
        >
          <CloudDownload size={13} class={busyKey === "update-all" ? "animate-pulse" : ""} />
          <span class="tabular-nums">{busyKey === "update-all" ? `${updateAllDone}/${updateAllTotal}` : "更新全部"}</span>
        </button>
      {/if}
      <button
        type="button"
        onclick={loadConfigs}
        disabled={busy}
        class="inline-flex h-8 w-8 items-center justify-center border border-slate-300 bg-white text-slate-700 transition-colors hover:bg-slate-100 disabled:cursor-wait disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-950 dark:text-slate-200 dark:hover:bg-zinc-900 rounded-lg"
        aria-label="刷新配置列表"
        title="刷新配置列表"
      >
        <RefreshCw size={14} class={loading ? "animate-spin" : ""} />
      </button>
      <button
        type="button"
        onclick={openAddModal}
        disabled={busy}
        class="inline-flex h-8 items-center justify-center gap-1.5 border border-slate-800 bg-slate-800 px-2.5 text-xs font-bold text-white transition-colors hover:bg-slate-700 disabled:cursor-wait disabled:opacity-50 dark:border-slate-300 dark:bg-slate-200 dark:text-zinc-900 dark:hover:bg-slate-300 rounded-lg"
      >
        <Plus size={14} />
        添加
      </button>
    </div>
  </div>

  {#if configs.length > 3 || searchQuery}
    <div class="relative" transition:slide={{ duration: 160, easing: cubicOut }}>
      <Search size={14} class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 dark:text-zinc-500" />
      <input
        type="search"
        bind:value={searchQuery}
        class="h-9 w-full border border-slate-300 bg-white pl-9 pr-9 text-sm text-slate-900 outline-none transition-colors placeholder:text-slate-400 focus:border-slate-700 dark:border-zinc-700 dark:bg-zinc-950 dark:text-slate-100 dark:placeholder:text-zinc-600 dark:focus:border-zinc-400 rounded-lg"
        placeholder="搜索名称或源链接"
        aria-label="搜索配置"
      />
      {#if searchQuery}
        <button
          type="button"
          onclick={() => (searchQuery = "")}
          class="absolute right-2 top-1/2 -translate-y-1/2 p-1 text-slate-400 transition-colors hover:text-slate-800 dark:text-zinc-500 dark:hover:text-zinc-200"
          aria-label="清空搜索"
        >
          <X size={13} />
        </button>
      {/if}
    </div>
  {/if}

  <div
    class="overflow-hidden transition-[height] duration-300 ease-[cubic-bezier(0.22,1,0.36,1)] motion-reduce:transition-none"
    style:height={configContentHeight > 0 ? `${configContentHeight}px` : undefined}
    aria-live="polite"
  >
    <div bind:offsetHeight={configContentHeight} class="grid items-start">
      {#if configViewState === "loading"}
        <div class="col-start-1 row-start-1 grid gap-2" in:fade={{ duration: 140 }} out:fade={{ duration: 100 }}>
          {#each Array(2) as _}
            <div class="h-18 animate-pulse border border-slate-200 bg-slate-100/70 dark:border-zinc-800 dark:bg-zinc-950/60 rounded-lg"></div>
          {/each}
        </div>
      {:else if configViewState === "empty"}
        <div
          class="col-start-1 row-start-1 flex flex-col items-center border border-dashed border-slate-300 px-4 py-8 text-center dark:border-zinc-700 rounded-lg"
          in:fly={{ y: 8, duration: 240, easing: quintOut }}
          out:fade={{ duration: 100 }}
        >
          <FileUp size={20} class="text-slate-400 dark:text-zinc-500" />
          <p class="mt-2.5 text-sm font-bold text-slate-700 dark:text-zinc-200">还没有可管理的配置</p>
          <p class="mt-1 max-w-sm text-xs text-slate-500 dark:text-zinc-400">添加订阅链接或导入本地 YAML 文件后，即可在这里切换。</p>
          <button
            type="button"
            onclick={openAddModal}
            class="mt-3.5 inline-flex items-center gap-1.5 border border-slate-800 bg-slate-800 px-3 py-2 text-xs font-bold text-white dark:border-slate-300 dark:bg-slate-200 dark:text-zinc-900 rounded-lg"
          >
            <Plus size={13} />
            添加第一个配置
          </button>
        </div>
      {:else if configViewState === "no-results"}
        <div
          class="col-start-1 row-start-1 border border-dashed border-slate-300 px-3 py-6 text-center text-sm text-slate-500 dark:border-zinc-700 dark:text-zinc-400 rounded-lg"
          in:fly={{ y: 6, duration: 200, easing: quintOut }}
          out:fade={{ duration: 100 }}
        >
          没有匹配“{searchQuery.trim()}”的配置
        </div>
      {:else}
        <div class="col-start-1 row-start-1 space-y-2">
          {#each visibleConfigs as config, index (config.kind + ":" + config.name)}
            <article
              class="relative overflow-hidden border p-2.5 transition-colors duration-200 {config.active
                ? 'border-emerald-400 bg-emerald-50/70 dark:border-emerald-800 dark:bg-emerald-950/20'
                : 'border-slate-300 bg-white hover:bg-slate-50 dark:border-zinc-700 dark:bg-zinc-950/50 dark:hover:bg-zinc-900/70'} rounded-lg"
              animate:flip={{ duration: 200, easing: quintOut }}
              in:fly={{ y: 8, duration: 240, delay: Math.min(index * 28, 140), easing: quintOut }}
              out:fade={{ duration: 100 }}
            >
              {#if config.active}
                <span class="absolute inset-y-0 left-0 w-0.5 bg-emerald-500" aria-hidden="true"></span>
              {/if}

              <div class="flex min-w-0 flex-wrap items-center gap-1.5">
                <p class="max-w-full truncate text-sm font-bold text-slate-900 dark:text-slate-100" title={config.name}>{config.name}</p>
                {#if config.active}
                  <span
                    class="inline-flex shrink-0 items-center gap-1 border border-emerald-300 bg-emerald-50 px-1.5 py-0.5 text-[10px] font-bold text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950 dark:text-emerald-300 rounded-md"
                  >
                    <Check size={10} />
                    已选择
                  </span>
                {/if}
                <span class="shrink-0 border border-slate-200 bg-slate-50 px-1.5 py-0.5 text-[10px] font-bold text-slate-500 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400 rounded-md">
                  {config.kind === "current" ? "默认" : config.sourceUrl ? "远程" : "本地"}
                </span>
              </div>

              <div class="mt-1.5 flex items-center justify-between gap-2">
                <div class="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-slate-500 dark:text-zinc-400">
                  <span class="tabular-nums">{formatBytes(config.size)}</span>
                  <span class="text-slate-300 dark:text-zinc-600">·</span>
                  <span>{formatUpdatedAt(config.updatedAt)}</span>
                  {#if config.sourceUrl}
                    <span class="text-slate-300 dark:text-zinc-600">·</span>
                    <span class="inline-flex min-w-0 max-w-full items-center gap-1" title={config.sourceUrl}>
                      <Link size={10} class="shrink-0" />
                      <span class="min-w-0 truncate">{config.sourceUrl}</span>
                    </span>
                  {/if}
                </div>

                {#if config.sourceUrl || !config.active}
                  <div class="flex shrink-0 items-center gap-1.5">
                    {#if config.sourceUrl}
                      <button
                        type="button"
                        onclick={() => updateConfig(config)}
                        disabled={busy}
                        class="inline-flex h-7 w-7 items-center justify-center border border-slate-300 bg-white text-slate-700 transition-colors hover:bg-slate-100 disabled:cursor-wait disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-950 dark:text-slate-200 dark:hover:bg-zinc-900 rounded-lg"
                        aria-label={`更新 ${config.name}`}
                        title="从源链接更新"
                      >
                        <RotateCw size={13} class={busyKey === `update:${config.name}` || updatingName === config.name ? "animate-spin" : ""} />
                      </button>
                    {/if}

                    {#if config.kind === "profile" && !config.active}
                      <button
                        type="button"
                        onclick={() => openActionModal("delete", config)}
                        disabled={busy}
                        class="inline-flex h-7 w-7 items-center justify-center border border-rose-200 bg-rose-50 text-rose-600 transition-colors hover:bg-rose-100 disabled:cursor-wait disabled:opacity-50 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-400 dark:hover:bg-rose-900/40 rounded-lg"
                        aria-label={`删除 ${config.name}`}
                        title="删除"
                      >
                        <Trash2 size={13} />
                      </button>
                    {/if}

                    {#if !config.active}
                      <button
                        type="button"
                        onclick={() => openActionModal("switch", config)}
                        disabled={busy}
                        class="inline-flex h-7 items-center justify-center gap-1 border border-slate-800 bg-slate-800 px-2.5 text-[11px] font-bold text-white transition-colors hover:bg-slate-700 disabled:cursor-wait disabled:opacity-50 dark:border-slate-300 dark:bg-slate-200 dark:text-zinc-900 dark:hover:bg-slate-300 rounded-lg"
                      >
                        <Power size={12} />
                        应用
                      </button>
                    {/if}
                  </div>
                {/if}
              </div>
            </article>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</div>

{#if banner}
  <NoticeBanner tone={banner.tone} message={banner.message} />
{/if}

{#if addModalOpen}
  <div class="fixed inset-0 z-50 flex items-center justify-center p-3 md:p-6">
    <button type="button" class="absolute inset-0 bg-slate-950/55" onclick={closeAddModal} disabled={!!busyKey} aria-label="关闭添加配置窗口" in:fade={{ duration: 120 }} out:fade={{ duration: 100 }}
    ></button>
    <div
      class="relative z-10 mx-auto flex max-h-[84dvh] w-full max-w-lg flex-col overflow-hidden border border-slate-300 bg-white dark:border-zinc-700 dark:bg-zinc-950 rounded-xl"
      role="dialog"
      aria-modal="true"
      aria-labelledby="mihomo-config-add-title"
      in:fly={{ y: 10, duration: 200, easing: cubicOut }}
      out:fade={{ duration: 120 }}
    >
      <div class="flex shrink-0 items-center justify-between gap-3 border-b border-slate-300 px-4 py-3 dark:border-zinc-700">
        <div class="flex min-w-0 items-center gap-2">
          <Plus size={15} class="shrink-0 text-slate-500 dark:text-zinc-400" />
          <div id="mihomo-config-add-title" class="truncate text-sm font-bold text-slate-900 dark:text-slate-100">添加配置</div>
        </div>
        <button
          type="button"
          class="inline-flex items-center justify-center border border-slate-300 p-1.5 text-slate-600 transition-colors hover:bg-slate-100 disabled:opacity-50 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800 rounded-lg"
          onclick={closeAddModal}
          disabled={!!busyKey}
          aria-label="关闭"
        >
          <X size={14} />
        </button>
      </div>

      <div class="overflow-y-auto p-4">
        <div class="relative grid grid-cols-2 border border-slate-300 bg-slate-100 p-0.5 text-xs font-bold dark:border-zinc-700 dark:bg-zinc-900 rounded-lg">
          <span
            aria-hidden="true"
            class="pointer-events-none absolute bottom-0.5 left-0.5 top-0.5 w-[calc(50%-0.125rem)] transform-gpu bg-slate-800 transition-transform duration-300 ease-[cubic-bezier(0.22,1,0.36,1)] will-change-transform motion-reduce:transition-none dark:bg-slate-200 rounded-md {addMode ===
            'local'
              ? 'translate-x-full'
              : 'translate-x-0'}"
          ></span>
          <button
            type="button"
            class="relative z-10 inline-flex items-center justify-center gap-1.5 px-3 py-2 transition-colors duration-200 motion-reduce:transition-none {addMode === 'url'
              ? 'text-white dark:text-zinc-900'
              : 'text-slate-500 hover:text-slate-800 dark:text-zinc-400 dark:hover:text-zinc-200'}"
            onclick={() => selectAddMode("url")}
            aria-pressed={addMode === "url"}
          >
            <Link size={13} />
            链接导入
          </button>
          <button
            type="button"
            class="relative z-10 inline-flex items-center justify-center gap-1.5 px-3 py-2 transition-colors duration-200 motion-reduce:transition-none {addMode === 'local'
              ? 'text-white dark:text-zinc-900'
              : 'text-slate-500 hover:text-slate-800 dark:text-zinc-400 dark:hover:text-zinc-200'}"
            onclick={() => selectAddMode("local")}
            aria-pressed={addMode === "local"}
          >
            <FileUp size={13} />
            本地文件
          </button>
        </div>

        {#if modalError}
          <div transition:slide={{ duration: 150, easing: cubicOut }}>
            <div class="mt-3 border border-red-300 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-800 dark:bg-red-950 dark:text-red-300 rounded-lg">
              {modalError}
            </div>
          </div>
        {/if}

        <div
          class="mt-4 grid items-start overflow-hidden transition-[height] duration-300 ease-[cubic-bezier(0.22,1,0.36,1)] motion-reduce:transition-none"
          style:height={activeAddPanelHeight > 0 ? `${activeAddPanelHeight}px` : undefined}
        >
          <div
            bind:offsetHeight={urlPanelHeight}
            class="col-start-1 row-start-1 self-start transition-opacity duration-200 ease-out will-change-opacity motion-reduce:transition-none {addMode === 'url'
              ? 'opacity-100'
              : 'pointer-events-none opacity-0'}"
            inert={addMode !== "url"}
            aria-hidden={addMode !== "url"}
          >
            <form
              class="space-y-3"
              autocomplete="off"
              onsubmit={(event) => {
                event.preventDefault();
                void addFromUrl();
              }}
            >
              <div class="grid gap-1.5">
                <label for="mihomo-config-url" class="text-xs font-bold text-slate-600 dark:text-zinc-300">配置链接</label>
                <input
                  bind:this={urlInput}
                  id="mihomo-config-url"
                  type="url"
                  inputmode="url"
                  bind:value={sourceUrl}
                  disabled={!!busyKey}
                  class="min-w-0 border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 outline-none transition-colors placeholder:text-slate-400 focus:border-slate-800 disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-950 dark:text-slate-200 dark:placeholder:text-zinc-600 dark:focus:border-slate-400 rounded-lg"
                  placeholder="https://example.com/config.yaml"
                />
              </div>

              <div class="grid gap-1.5">
                <label for="mihomo-config-name" class="text-xs font-bold text-slate-600 dark:text-zinc-300">配置名称（可选）</label>
                <input
                  id="mihomo-config-name"
                  type="text"
                  bind:value={sourceName}
                  disabled={!!busyKey}
                  class="min-w-0 border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 outline-none transition-colors placeholder:text-slate-400 focus:border-slate-800 disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-950 dark:text-slate-200 dark:placeholder:text-zinc-600 dark:focus:border-slate-400 rounded-lg"
                  placeholder="留空则从链接推断"
                />
                <p class="truncate text-[11px] {duplicateConfig ? 'text-amber-600 dark:text-amber-400' : 'text-slate-400 dark:text-zinc-500'}">
                  {duplicateConfig ? `将覆盖已有配置 ${duplicateConfig.name}` : "同名配置会被覆盖更新"}
                </p>
              </div>

              <button
                type="submit"
                disabled={!!busyKey || !sourceUrl.trim()}
                class="inline-flex w-full items-center justify-center gap-2 border border-slate-800 bg-slate-800 px-3 py-2.5 text-sm font-bold text-white transition-colors hover:bg-slate-700 disabled:cursor-wait disabled:opacity-50 dark:border-slate-300 dark:bg-slate-200 dark:text-zinc-900 dark:hover:bg-slate-300 rounded-lg"
              >
                {#if busyKey === "download"}
                  <RefreshCw size={15} class="animate-spin" />
                  正在下载
                {:else}
                  <Upload size={15} />
                  下载并添加
                {/if}
              </button>
            </form>
          </div>

          <div
            bind:offsetHeight={localPanelHeight}
            class="col-start-1 row-start-1 self-start transition-opacity duration-200 ease-out will-change-opacity motion-reduce:transition-none {addMode === 'local'
              ? 'opacity-100'
              : 'pointer-events-none opacity-0'}"
            inert={addMode !== "local"}
            aria-hidden={addMode !== "local"}
          >
            <div class="space-y-3">
              <div class="grid gap-1.5">
                <label for="mihomo-local-config-name" class="text-xs font-bold text-slate-600 dark:text-zinc-300">配置名称（可选）</label>
                <input
                  id="mihomo-local-config-name"
                  type="text"
                  bind:value={sourceName}
                  disabled={!!busyKey}
                  class="min-w-0 border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 outline-none transition-colors placeholder:text-slate-400 focus:border-slate-800 disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-950 dark:text-slate-200 dark:placeholder:text-zinc-600 dark:focus:border-slate-400 rounded-lg"
                  placeholder="留空则使用原文件名"
                />
                <p class="truncate text-[11px] {duplicateConfig ? 'text-amber-600 dark:text-amber-400' : 'text-slate-400 dark:text-zinc-500'}">
                  {duplicateConfig ? `将覆盖已有配置 ${duplicateConfig.name}` : "同名配置会被覆盖更新"}
                </p>
              </div>

              <input bind:this={fileInput} type="file" accept=".yaml,.yml,text/yaml,application/x-yaml" class="sr-only" onchange={handleLocalFileChange} />
              <button
                type="button"
                onclick={chooseLocalFile}
                ondragenter={(event) => {
                  event.preventDefault();
                  fileDragging = true;
                }}
                ondragover={(event) => {
                  event.preventDefault();
                  fileDragging = true;
                }}
                ondragleave={() => (fileDragging = false)}
                ondrop={handleLocalFileDrop}
                disabled={!!busyKey}
                class="flex w-full flex-col items-center gap-2.5 border border-dashed p-5 text-center transition-colors disabled:cursor-wait disabled:opacity-60 {fileDragging
                  ? 'border-slate-800 bg-slate-100 dark:border-slate-300 dark:bg-zinc-800'
                  : 'border-slate-300 bg-slate-50/60 hover:bg-slate-100 dark:border-zinc-700 dark:bg-zinc-900/30 dark:hover:bg-zinc-900'} rounded-lg"
              >
                <span class="flex h-10 w-10 items-center justify-center border border-slate-300 bg-white text-slate-500 dark:border-zinc-700 dark:bg-zinc-950 dark:text-zinc-400 rounded-lg">
                  {#if busyKey === "local"}<RefreshCw size={18} class="animate-spin" />{:else}<FileUp size={18} />{/if}
                </span>
                <span>
                  <span class="block text-sm font-bold text-slate-900 dark:text-slate-200">{fileDragging ? "松开即可导入" : "选择或拖入配置文件"}</span>
                  <span class="mt-1 block text-[11px] text-slate-500 dark:text-zinc-400">.yaml / .yml，最大 {formatBytes(MAX_LOCAL_CONFIG_BYTES)}</span>
                </span>
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
{/if}

{#if pendingAction}
  <div class="fixed inset-0 z-50 flex items-center justify-center p-4">
    <button type="button" class="absolute inset-0 bg-slate-950/55" onclick={closeActionModal} disabled={!!busyKey} aria-label="关闭确认窗口" in:fade={{ duration: 120 }} out:fade={{ duration: 100 }}
    ></button>
    <div
      class="relative z-10 w-full max-w-md border border-slate-300 bg-white dark:border-zinc-700 dark:bg-zinc-950 rounded-xl"
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="mihomo-action-title"
      in:fly={{ y: 10, duration: 180, easing: cubicOut }}
      out:fade={{ duration: 110 }}
    >
      <div class="flex items-start gap-3 p-4">
        <div
          class="flex h-9 w-9 shrink-0 items-center justify-center border {pendingAction.kind === 'delete'
            ? 'border-rose-200 bg-rose-50 text-rose-600 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-400'
            : 'border-slate-300 bg-slate-50 text-slate-600 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300'} rounded-lg"
        >
          {#if pendingAction.kind === "delete"}<Trash2 size={17} />{:else}<Power size={17} />{/if}
        </div>
        <div class="min-w-0 flex-1">
          <h3 id="mihomo-action-title" class="text-sm font-bold text-slate-900 dark:text-slate-100">
            {pendingAction.kind === "delete" ? "删除配置" : "应用配置"}
          </h3>
          <p class="mt-1 break-all font-mono text-xs text-slate-500 dark:text-zinc-400">{pendingAction.config.name}</p>
          <p class="mt-3 text-xs leading-relaxed text-slate-600 dark:text-zinc-300">
            {pendingAction.kind === "delete" ? "配置文件及保存的源链接会一并删除，此操作无法撤销。" : "应用前会先调用 Mihomo 校验配置。可以立即重启核心生效，或仅保存为下次启动配置。"}
          </p>
        </div>
      </div>

      {#if actionError}
        <div transition:slide={{ duration: 150, easing: cubicOut }}>
          <div class="mx-4 mb-3 border border-red-300 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-800 dark:bg-red-950 dark:text-red-300 rounded-lg">
            {actionError}
          </div>
        </div>
      {/if}

      <div class="flex flex-col-reverse gap-2 border-t border-slate-200 p-3 dark:border-zinc-800 sm:flex-row sm:justify-end">
        <button
          type="button"
          onclick={closeActionModal}
          disabled={!!busyKey}
          class="inline-flex h-9 items-center justify-center border border-slate-300 bg-white px-3 text-xs font-bold text-slate-700 transition-colors hover:bg-slate-100 disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-950 dark:text-zinc-300 dark:hover:bg-zinc-900 rounded-lg"
          >取消</button
        >
        {#if pendingAction.kind === "delete"}
          <button
            type="button"
            onclick={confirmDelete}
            disabled={!!busyKey}
            class="inline-flex h-9 items-center justify-center gap-1.5 border border-rose-600 bg-rose-600 px-3 text-xs font-bold text-white transition-colors hover:bg-rose-700 disabled:cursor-wait disabled:opacity-60 dark:border-rose-500 dark:bg-rose-500 dark:text-white rounded-lg"
          >
            {#if busyKey.startsWith("delete:")}<RefreshCw size={13} class="animate-spin" />{:else}<Trash2 size={13} />{/if}
            确认删除
          </button>
        {:else}
          <button
            type="button"
            onclick={() => confirmSwitch(false)}
            disabled={!!busyKey}
            class="inline-flex h-9 items-center justify-center gap-1.5 border border-slate-300 bg-white px-3 text-xs font-bold text-slate-700 transition-colors hover:bg-slate-100 disabled:cursor-wait disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-950 dark:text-zinc-300 dark:hover:bg-zinc-900 rounded-lg"
          >
            {#if busyKey === "switch:save"}<RefreshCw size={13} class="animate-spin" />{/if}
            下次启动生效
          </button>
          <button
            type="button"
            onclick={() => confirmSwitch(true)}
            disabled={!!busyKey}
            class="inline-flex h-9 items-center justify-center gap-1.5 border border-slate-800 bg-slate-800 px-3 text-xs font-bold text-white transition-colors hover:bg-slate-700 disabled:cursor-wait disabled:opacity-60 dark:border-slate-300 dark:bg-slate-200 dark:text-zinc-900 dark:hover:bg-slate-300 rounded-lg"
          >
            {#if busyKey === "switch:restart"}<RefreshCw size={13} class="animate-spin" />{:else}<RotateCw size={13} />{/if}
            应用并重启
          </button>
        {/if}
      </div>
    </div>
  </div>
{/if}
