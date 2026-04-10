<script lang="ts">
  import { flip } from "svelte/animate";
  import { onDestroy, onMount } from "svelte";
  import { cubicOut } from "svelte/easing";
  import { fly, slide } from "svelte/transition";
  import { ArrowDown, ArrowUp, Check, ChevronRight, Eye, EyeOff, RefreshCw, Save, X } from "@lucide/svelte";
  import CheckBox from "$lib/components/CheckBox.svelte";
  import * as clashRealApi from "$lib/api/clash";
  import * as clashMockApi from "$lib/api/clash.mock";
  import * as actionRealApi from "$lib/api/action";
  import * as actionMockApi from "$lib/api/action.mock";
  import { getDefaultHomeLayoutSettings, loadHomeLayoutSettings, saveHomeLayoutSettings, roundedStore, type HomeModuleId } from "$lib/settings";
  import Select from "$lib/components/Select.svelte";

  type ProxyMode = "rule" | "global" | "direct";

  const isProd = import.meta.env.MODE !== "production";
  const clashApi = isProd ? clashMockApi : clashRealApi;
  const actionApi = isProd ? actionMockApi : actionRealApi;

  // box.config 配置
  let boxConfigPort = $state(9090);
  let boxConfigSecret = $state("");
  let boxConfigSaved = $state(false);
  let boxConfigLoading = $state(false);
  let boxConfigError = $state("");
  let boxConfigSecretVisible = $state(false);
  let clashApiChecking = $state(false);
  let clashApiCheckOk = $state<boolean | null>(null);
  let clashApiCheckResetTimer: ReturnType<typeof setTimeout> | undefined;
  let toggleAction = $state<"service" | "tun" | "mode_cycle">("service");
  let toggleTunTarget = $state<"toggle" | "on" | "off">("toggle");
  let toggleModeCycle = $state<ProxyMode[]>(["rule", "global", "direct"]);
  let toggleModeOrder = $state<ProxyMode[]>([...ALL_PROXY_MODES]);
  let currentVersion = $state("-");
  let currentVersionCode = $state("");
  const r = $derived($roundedStore);
  let saveResetTimer: ReturnType<typeof setTimeout> | undefined;
  let homeLayout = $state(loadHomeLayoutSettings());
  let homeLayoutSaved = $state(false);
  let homeLayoutReseted = $state(false);
  let homeLayoutError = $state("");
  let homeLayoutSaveResetTimer: ReturnType<typeof setTimeout> | undefined;
  let homeLayoutResetStateTimer: ReturnType<typeof setTimeout> | undefined;

  const proxyModeLabels: Record<ProxyMode, string> = {
    rule: "规则",
    global: "全局",
    direct: "直连",
  };

  const moduleLabels: Record<HomeModuleId, string> = {
    tun: "网络接管与内核",
    proxy: "代理模式选择",
    stats: "实时内核信息",
    service: "快速控制指令",
    panel: "面板快捷跳转",
    core: "核心状态与端口",
    log: "Terminal Logs",
  };

  const toggleActionOptions = [
    { value: "service", label: "服务开关（默认）" },
    { value: "tun", label: "切换 TUN" },
    { value: "mode_cycle", label: "循环切换代理模式" },
  ];

  const toggleTunTargetOptions = [
    { value: "toggle", label: "按当前状态切换" },
    { value: "on", label: "固定开启" },
    { value: "off", label: "固定关闭" },
  ];

  function triggerSavedState() {
    boxConfigSaved = true;
    if (saveResetTimer) clearTimeout(saveResetTimer);
    saveResetTimer = setTimeout(() => {
      boxConfigSaved = false;
      saveResetTimer = undefined;
    }, 1800);
  }

  onMount(() => {
    loadBoxConfig();
    loadModuleInfo();
  });

  onDestroy(() => {
    if (saveResetTimer) {
      clearTimeout(saveResetTimer);
      saveResetTimer = undefined;
    }

    if (homeLayoutSaveResetTimer) {
      clearTimeout(homeLayoutSaveResetTimer);
      homeLayoutSaveResetTimer = undefined;
    }

    if (homeLayoutResetStateTimer) {
      clearTimeout(homeLayoutResetStateTimer);
      homeLayoutResetStateTimer = undefined;
    }

    if (clashApiCheckResetTimer) {
      clearTimeout(clashApiCheckResetTimer);
      clashApiCheckResetTimer = undefined;
    }
  });

  function triggerHomeLayoutSavedState() {
    homeLayoutSaved = true;
    if (homeLayoutSaveResetTimer) clearTimeout(homeLayoutSaveResetTimer);
    homeLayoutSaveResetTimer = setTimeout(() => {
      homeLayoutSaved = false;
      homeLayoutSaveResetTimer = undefined;
    }, 1800);
  }

  function buildToggleModeOrder(selectedModes: ProxyMode[]): ProxyMode[] {
    const selected = selectedModes.filter((mode): mode is ProxyMode => ALL_PROXY_MODES.includes(mode));
    const remaining = ALL_PROXY_MODES.filter((mode) => !selected.includes(mode));
    return [...selected, ...remaining];
  }

  async function loadBoxConfig() {
    try {
      boxConfigLoading = true;
      boxConfigError = "";
      const content = await clashApi.readBoxConfig();

      const parsed = clashApi.parseBoxConfig(content);
      boxConfigPort = parsed.clashApiPort;
      boxConfigSecret = parsed.clashApiSecret;
      toggleAction = parsed.toggleAction;
      toggleTunTarget = parsed.toggleTunTarget;
      toggleModeCycle = [...parsed.toggleModeCycle];
      toggleModeOrder = buildToggleModeOrder(parsed.toggleModeCycle);
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      boxConfigError = `无法读取 box.config 文件: ${message}`;
      console.error("Failed to load box.config:", e);
    } finally {
      boxConfigLoading = false;
    }
  }

  async function saveBoxConfig() {
    try {
      boxConfigLoading = true;
      boxConfigError = "";

      await clashApi.updateBoxConfigValues({
        clash_api_port: boxConfigPort.toString(),
        clash_api_secret: boxConfigSecret,
        toggle_action: `"${toggleAction}"`,
        toggle_tun_target: `"${toggleTunTarget}"`,
        toggle_mode_cycle: `"${toggleModeOrder.filter((mode) => toggleModeCycle.includes(mode)).join(",")}"`,
      });

      triggerSavedState();
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      boxConfigError = `保存失败: ${message}`;
      console.error("Failed to save box.config:", e);
    } finally {
      boxConfigLoading = false;
    }
  }

  async function checkClashApiVersion() {
    try {
      if (clashApiCheckResetTimer) {
        clearTimeout(clashApiCheckResetTimer);
        clashApiCheckResetTimer = undefined;
      }

      clashApiChecking = true;
      clashApiCheckOk = null;

      const result = await clashApi.checkVersion({
        port: boxConfigPort,
        secret: boxConfigSecret,
      });

      if (result.ok) {
        clashApiCheckOk = true;
      } else {
        clashApiCheckOk = false;
      }
    } catch {
      clashApiCheckOk = false;
    } finally {
      clashApiChecking = false;

      if (clashApiCheckResetTimer) {
        clearTimeout(clashApiCheckResetTimer);
      }
      clashApiCheckResetTimer = setTimeout(() => {
        clashApiCheckOk = null;
        clashApiCheckResetTimer = undefined;
      }, 2000);
    }
  }

  async function loadModuleInfo() {
    try {
      const info = await actionApi.getModuleInfo();
      currentVersion = info.version || "-";
      currentVersionCode = info.versionCode || "";
    } catch (e) {
      console.error("Failed to load module info:", e);
      currentVersion = "-";
      currentVersionCode = "";
    }
  }

  async function openSupportLink(url: string) {
    try {
      await actionApi.openExternalUrl(url);
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      boxConfigError = `打开链接失败: ${message}`;
    }
  }

  function toggleModeCycleEntry(mode: ProxyMode) {
    const exists = toggleModeCycle.includes(mode);
    if (exists) {
      const next = toggleModeCycle.filter((item) => item !== mode);
      if (!next.length) {
        boxConfigError = "模式循环至少需要一个状态";
        return;
      }
      toggleModeCycle = next;
      boxConfigError = "";
      return;
    }

    toggleModeCycle = [...toggleModeCycle, mode];
    boxConfigError = "";
  }

  function moveToggleModeCycle(index: number, direction: -1 | 1) {
    const targetIndex = index + direction;
    if (targetIndex < 0 || targetIndex >= toggleModeOrder.length) return;

    const next = [...toggleModeOrder];
    const [item] = next.splice(index, 1);
    next.splice(targetIndex, 0, item);
    toggleModeOrder = next;
  }

  function isModuleVisible(moduleId: HomeModuleId): boolean {
    return !homeLayout.hiddenModules.includes(moduleId);
  }

  function toggleModuleVisible(moduleId: HomeModuleId) {
    if (isModuleVisible(moduleId)) {
      homeLayout.hiddenModules = [...homeLayout.hiddenModules, moduleId];
    } else {
      homeLayout.hiddenModules = homeLayout.hiddenModules.filter((id) => id !== moduleId);
    }
    homeLayout = { ...homeLayout };
  }

  function moveModule(index: number, direction: -1 | 1) {
    const target = index + direction;
    if (target < 0 || target >= homeLayout.moduleOrder.length) return;

    const next = [...homeLayout.moduleOrder];
    const [item] = next.splice(index, 1);
    next.splice(target, 0, item);
    homeLayout.moduleOrder = next;
    homeLayout = { ...homeLayout };
  }

  function getDefaultPanelUrl(): string {
    return `http://127.0.0.1:${boxConfigPort}/ui`;
  }

  async function saveHomeLayout() {
    try {
      homeLayoutError = "";
      saveHomeLayoutSettings(homeLayout);
      triggerHomeLayoutSavedState();
      await actionApi.setEdgeToEdge(homeLayout.edgeToEdge);
      roundedStore.set(homeLayout.rounded);
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      homeLayoutError = `保存首页布局失败: ${message}`;
    }
  }

  function resetHomeLayoutToDefault() {
    homeLayoutError = "";
    homeLayoutSaved = false;
    homeLayout = getDefaultHomeLayoutSettings();
    homeLayoutReseted = true;

    if (homeLayoutResetStateTimer) {
      clearTimeout(homeLayoutResetStateTimer);
    }
    homeLayoutResetStateTimer = setTimeout(() => {
      homeLayoutReseted = false;
      homeLayoutResetStateTimer = undefined;
    }, 1800);
  }
</script>

<div class="max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-6">
  <section in:fly={{ y: 12, duration: 260, delay: 20, easing: cubicOut }} class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors {r ? 'rounded-xl' : ''}">
    <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 {r ? 'rounded-t-xl' : ''}">
      <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">box.config 配置</h2>
    </div>
    <form
      class="p-4 space-y-4"
      autocomplete="off"
      onsubmit={(event) => {
        event.preventDefault();
        saveBoxConfig();
      }}
    >
      <input type="text" name="username" autocomplete="username" tabindex="-1" aria-hidden="true" class="sr-only absolute opacity-0 pointer-events-none" />

      {#if boxConfigError}
        <div transition:fly={{ y: -8, duration: 220, easing: cubicOut }} class="px-3 py-2 bg-red-50 dark:bg-red-950 border border-red-300 dark:border-red-800 text-red-700 dark:text-red-300 text-sm">
          {boxConfigError}
        </div>
      {/if}

      <div class="flex flex-col gap-2">
        <label for="box-api-port" class="text-sm font-bold text-slate-900 dark:text-slate-200">Clash API Port</label>
        <div class="grid grid-cols-[minmax(0,1fr)_auto] items-stretch gap-2">
          <input
            id="box-api-port"
            type="number"
            bind:value={boxConfigPort}
            disabled={boxConfigLoading}
            class="min-w-0 px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors disabled:opacity-50
{r ? 'rounded-lg' : ''}"
            placeholder="9090"
          />

          <button
            type="button"
            onclick={checkClashApiVersion}
            disabled={boxConfigLoading || clashApiChecking}
            class="group relative overflow-hidden shrink-0 whitespace-nowrap max-w-[42vw] sm:max-w-none border px-3 py-2.5 text-sm font-bold transition-all duration-300 outline-none disabled:opacity-70 text-center {clashApiCheckOk ===
            true
              ? 'border-emerald-700 dark:border-emerald-400 bg-emerald-700 dark:bg-emerald-300 text-white dark:text-zinc-900'
              : clashApiCheckOk === false
                ? 'border-rose-700 dark:border-rose-400 bg-rose-700 dark:bg-rose-300 text-white dark:text-zinc-900'
                : 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 hover:bg-slate-700 dark:hover:bg-slate-300'} {r ? 'rounded-lg' : ''}"
          >
            <div class="grid place-items-center">
              {#if clashApiChecking}
                <span
                  in:fly={{ y: 20, duration: 300, easing: cubicOut }}
                  out:fly={{ y: -20, duration: 200 }}
                  class="inline-flex items-center justify-center gap-1.5 col-start-1 row-start-1 w-full min-w-0"
                >
                  <RefreshCw size={14} class="animate-spin" />
                  <span class="truncate">正在检测</span>
                </span>
              {:else if clashApiCheckOk === true}
                <span
                  in:fly={{ y: 20, duration: 300, easing: cubicOut }}
                  out:fly={{ y: -20, duration: 200 }}
                  class="inline-flex items-center justify-center gap-1.5 col-start-1 row-start-1 w-full min-w-0"
                >
                  <Check size={14} />
                  <span class="truncate">连接成功</span>
                </span>
              {:else if clashApiCheckOk === false}
                <span
                  in:fly={{ y: 20, duration: 300, easing: cubicOut }}
                  out:fly={{ y: -20, duration: 200 }}
                  class="inline-flex items-center justify-center gap-1.5 col-start-1 row-start-1 w-full min-w-0"
                >
                  <X size={14} />
                  <span class="truncate">连接失败</span>
                </span>
              {:else}
                <span
                  in:fly={{ y: 20, duration: 300, easing: cubicOut }}
                  out:fly={{ y: -20, duration: 200 }}
                  class="inline-flex items-center justify-center gap-1.5 col-start-1 row-start-1 w-full min-w-0"
                >
                  <RefreshCw size={14} />
                  <span class="truncate">检查连接</span>
                </span>
              {/if}
            </div>
          </button>
        </div>
        <span class="text-xs text-slate-500 dark:text-slate-400">Clash API 外部控制端口</span>
      </div>

      <div class="flex flex-col gap-2">
        <label for="box-api-secret" class="text-sm font-bold text-slate-900 dark:text-slate-200">Clash API Secret</label>
        <div class="relative">
          <input
            id="box-api-secret"
            type={boxConfigSecretVisible ? "text" : "password"}
            bind:value={boxConfigSecret}
            name="clash-api-secret"
            autocomplete="off"
            data-form-type="other"
            data-lpignore="true"
            disabled={boxConfigLoading}
            class="w-full px-3 pr-10 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors font-mono disabled:opacity-50
            {r ? 'rounded-lg' : ''}"
            placeholder="留空表示无密钥"
          />
          <button
            type="button"
            onclick={() => (boxConfigSecretVisible = !boxConfigSecretVisible)}
            class="absolute right-2 top-1/2 -translate-y-1/2 inline-flex items-center justify-center p-1 text-slate-500 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200"
            aria-label={boxConfigSecretVisible ? "隐藏密钥" : "显示密钥"}
            disabled={boxConfigLoading}
          >
            {#if boxConfigSecretVisible}
              <EyeOff size={16} />
            {:else}
              <Eye size={16} />
            {/if}
          </button>
        </div>
        <span class="text-xs text-slate-500 dark:text-slate-400">Clash API 访问密钥</span>
      </div>

      <div class="h-px bg-slate-200 dark:bg-zinc-800"></div>

      <div class="flex flex-col gap-2">
        <label for="toggle-action" class="text-sm font-bold text-slate-900 dark:text-slate-200">默认 Toggle 行为</label>
        <Select id="toggle-action" bind:value={toggleAction} options={toggleActionOptions} disabled={boxConfigLoading} />
        <span class="text-xs text-slate-500 dark:text-slate-400">作用于 action.sh 的 toggle 命令</span>
      </div>

      {#if toggleAction === "tun"}
        <div in:slide={{ duration: 220, easing: cubicOut }} out:slide={{ duration: 180, easing: cubicOut }}>
          <div class="flex flex-col gap-2">
            <label for="toggle-tun-target" class="text-sm font-bold text-slate-900 dark:text-slate-200">TUN Toggle 策略</label>
            <Select id="toggle-tun-target" bind:value={toggleTunTarget} options={toggleTunTargetOptions} disabled={boxConfigLoading} />
          </div>
        </div>
      {/if}

      {#if toggleAction === "mode_cycle"}
        <div in:slide={{ duration: 220, easing: cubicOut }} out:slide={{ duration: 180, easing: cubicOut }} class="overflow-hidden">
          <div class="space-y-3">
            <div class="flex flex-col gap-2">
              <div class="text-sm font-bold text-slate-900 dark:text-slate-200">循环包含模式与顺序</div>
              <div class="space-y-2">
                {#each toggleModeOrder as mode, index (mode)}
                  <div
                    in:fly={{ y: 16, duration: 220, easing: cubicOut }}
                    animate:flip={{ duration: 220, easing: cubicOut }}
                    class="flex items-center justify-between border border-slate-300 dark:border-zinc-700 px-3 py-2 bg-white dark:bg-zinc-950/50 {r ? 'rounded-lg' : ''}"
                  >
                    <CheckBox id={`mode-cycle-${mode}`} checked={toggleModeCycle.includes(mode)} onchange={() => toggleModeCycleEntry(mode)} label={proxyModeLabels[mode]} bare />
                    <div class="flex items-center gap-1">
                      <button
                        type="button"
                        onclick={() => moveToggleModeCycle(index, -1)}
                        class="inline-flex items-center justify-center p-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-slate-300 disabled:opacity-40 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors {r
                          ? 'rounded-lg'
                          : ''}"
                        disabled={index === 0 || boxConfigLoading}
                        aria-label="上移"
                      >
                        <ArrowUp size={13} />
                      </button>
                      <button
                        type="button"
                        onclick={() => moveToggleModeCycle(index, 1)}
                        class="inline-flex items-center justify-center p-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-slate-300 disabled:opacity-40 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors {r
                          ? 'rounded-lg'
                          : ''}"
                        disabled={index === toggleModeOrder.length - 1 || boxConfigLoading}
                        aria-label="下移"
                      >
                        <ArrowDown size={13} />
                      </button>
                    </div>
                  </div>
                {/each}
              </div>
              <span class="text-xs text-slate-500 dark:text-slate-400">勾选表示参与循环；上下调整后即为执行顺序</span>
            </div>
          </div>
        </div>
      {/if}

      <div class="flex gap-3 pt-2">
        <button
          type="submit"
          disabled={boxConfigLoading}
          class="group relative overflow-hidden flex-1 border border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 px-4 py-2.5 text-sm font-bold transition-all duration-300 outline-none disabled:opacity-80 hover:bg-slate-700 dark:hover:bg-slate-300 hover:shadow-[0_8px_20px_rgba(15,23,42,0.15)] active:translate-y-px
          {r ? 'rounded-lg' : ''}"
        >
          <div class="grid place-items-center">
            {#if boxConfigSaved}
              <span
                in:fly={{ y: 20, duration: 300, easing: cubicOut }}
                out:fly={{ y: -20, duration: 200 }}
                class="flex items-center gap-2 col-start-1 row-start-1 text-emerald-400 dark:text-emerald-600"
              >
                <Check size={16} />
                保存成功
              </span>
            {:else}
              <span in:fly={{ y: 20, duration: 300, easing: cubicOut }} out:fly={{ y: -20, duration: 200 }} class="flex items-center gap-2 col-start-1 row-start-1">
                <Save size={16} />
                保存配置
              </span>
            {/if}
          </div>
        </button>
      </div>

      <!-- <div class="px-3 py-2 bg-blue-50 dark:bg-blue-950 border border-blue-300 dark:border-blue-800 text-blue-700 dark:text-blue-300 text-xs">注意：修改配置后需要重启服务才能生效</div> -->
    </form>
  </section>

  <section in:fly={{ y: 12, duration: 260, delay: 50, easing: cubicOut }} class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors {r ? 'rounded-xl' : ''}">
    <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 {r ? 'rounded-t-xl' : ''}">
      <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">WEB UI 设置</h2>
    </div>
    <div class="p-4 space-y-4">
      {#if homeLayoutError}
        <div transition:fly={{ y: -8, duration: 220, easing: cubicOut }} class="px-3 py-2 bg-red-50 dark:bg-red-950 border border-red-300 dark:border-red-800 text-red-700 dark:text-red-300 text-sm">
          {homeLayoutError}
        </div>
      {/if}

      <div class="flex flex-col gap-2">
        <label for="edge-to-edge" class="text-sm font-bold text-slate-900 dark:text-slate-200">启用沉浸式全面屏 (Edge-to-Edge)</label>

        <CheckBox id="edge-to-edge" checked={homeLayout.edgeToEdge} onchange={() => (homeLayout.edgeToEdge = !homeLayout.edgeToEdge)} label="跟随系统手势区域扩展内容" />

        <span class="text-xs text-slate-500 dark:text-slate-400">启用后将由系统处理状态栏和底部手势区域的沉浸显示</span>
      </div>

      <div class="h-px bg-slate-200 dark:bg-zinc-800"></div>

      <div class="flex flex-col gap-2">
        <label for="rounded-ui" class="text-sm font-bold text-slate-900 dark:text-slate-200">界面风格</label>

        <CheckBox id="rounded-ui" checked={homeLayout.rounded} onchange={() => (homeLayout.rounded = !homeLayout.rounded)} label="使用圆润风格" />

        <span class="text-xs text-slate-500 dark:text-slate-400">开启后所有元素使用圆角</span>
      </div>

      <div class="h-px bg-slate-200 dark:bg-zinc-800"></div>

      <div class="flex flex-col gap-2">
        <label for="panel-url" class="text-sm font-bold text-slate-900 dark:text-slate-200">面板快捷跳转 URL</label>
        <input
          id="panel-url"
          type="text"
          bind:value={homeLayout.panelUrl}
          class="px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors
          {r ? 'rounded-lg' : ''}"
          placeholder={getDefaultPanelUrl()}
        />
        <span class="text-xs text-slate-500 dark:text-slate-400">留空使用默认：{getDefaultPanelUrl()}</span>
      </div>

      <div class="h-px bg-slate-200 dark:bg-zinc-800"></div>

      <div class="space-y-2">
        <p class="text-sm font-bold text-slate-900 dark:text-slate-200">首页模块显示与排序</p>
        {#each homeLayout.moduleOrder as moduleId, index (moduleId)}
          <div
            animate:flip={{ duration: 220, easing: cubicOut }}
            class="flex items-center justify-between border border-slate-300 dark:border-zinc-700 px-3 py-2 bg-white dark:bg-zinc-950/50 {r ? 'rounded-lg' : ''}"
          >
            <CheckBox id={`module-${moduleId}`} checked={isModuleVisible(moduleId)} onchange={() => toggleModuleVisible(moduleId)} label={moduleLabels[moduleId]} bare />
            <div class="flex items-center gap-1">
              <button
                type="button"
                onclick={() => moveModule(index, -1)}
                class="inline-flex items-center justify-center p-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-slate-300 disabled:opacity-40 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors
                {r ? 'rounded-lg' : ''}"
                disabled={index === 0}
                aria-label="上移"
              >
                <ArrowUp size={14} />
              </button>
              <button
                type="button"
                onclick={() => moveModule(index, 1)}
                class="inline-flex items-center justify-center p-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-slate-300 disabled:opacity-40 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors
                {r ? 'rounded-lg' : ''}"
                disabled={index === homeLayout.moduleOrder.length - 1}
                aria-label="下移"
              >
                <ArrowDown size={14} />
              </button>
            </div>
          </div>
        {/each}
      </div>

      <div class="flex gap-3 pt-2">
        <button
          onclick={saveHomeLayout}
          class="group relative overflow-hidden flex-1 border border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 px-4 py-2.5 text-sm font-bold transition-all duration-300 outline-none hover:bg-slate-700 dark:hover:bg-slate-300 hover:shadow-[0_8px_20px_rgba(15,23,42,0.15)] active:translate-y-px
          {r ? 'rounded-lg' : ''}"
        >
          <div class="grid place-items-center">
            {#if homeLayoutSaved}
              <span
                in:fly={{ y: 20, duration: 300, easing: cubicOut }}
                out:fly={{ y: -20, duration: 200 }}
                class="flex items-center gap-2 col-start-1 row-start-1 text-emerald-400 dark:text-emerald-600"
              >
                <Check size={16} />
                保存成功
              </span>
            {:else}
              <span in:fly={{ y: 20, duration: 300, easing: cubicOut }} out:fly={{ y: -20, duration: 200 }} class="flex items-center gap-2 col-start-1 row-start-1">
                <Save size={16} />
                保存 WEB UI 设置
              </span>
            {/if}
          </div>
        </button>
        <button
          type="button"
          onclick={resetHomeLayoutToDefault}
          class="group relative overflow-hidden border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-700 dark:text-slate-300 px-4 py-2.5 text-sm font-bold transition-colors outline-none hover:bg-slate-100 dark:hover:bg-zinc-900
          {r ? 'rounded-lg' : ''}"
        >
          <div class="grid place-items-center">
            {#if homeLayoutReseted}
              <span in:fly={{ y: 20, duration: 300, easing: cubicOut }} out:fly={{ y: -20, duration: 200 }} class="col-start-1 row-start-1">恢复成功</span>
            {:else}
              <span in:fly={{ y: 20, duration: 300, easing: cubicOut }} out:fly={{ y: -20, duration: 200 }} class="col-start-1 row-start-1">恢复默认</span>
            {/if}
          </div>
        </button>
      </div>
    </div>
  </section>

  <section in:fly={{ y: 12, duration: 260, delay: 80, easing: cubicOut }} class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors {r ? 'rounded-xl' : ''}">
    <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950 {r ? 'rounded-t-xl' : ''}">
      <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">关于</h2>
    </div>
    <button
      type="button"
      class="w-full p-4 text-left text-sm text-slate-600 dark:text-slate-400 cursor-pointer hover:bg-slate-50 dark:hover:bg-zinc-800/50 transition-colors"
      onclick={() => openSupportLink("https://github.com/LufsX/mono-box")}
    >
      <div class="flex items-center justify-between gap-3">
        <div class="space-y-2">
          <p class="font-bold text-slate-800 dark:text-slate-200">Mono Box {currentVersion}{currentVersionCode ? ` (${currentVersionCode})` : ""}</p>
          <p class="text-xs">基于 Mihomo 核心的代理工具</p>
        </div>
        <ChevronRight size={18} class="text-slate-400 dark:text-zinc-500" aria-hidden="true" />
      </div>
    </button>
  </section>
</div>
