<script lang="ts">
  import { flip } from "svelte/animate";
  import { cubicOut } from "svelte/easing";
  import { fly } from "svelte/transition";
  import { ArrowDown, ArrowUp, Check, Save } from "@lucide/svelte";
  import CheckBox from "$lib/components/common/CheckBox.svelte";
  import { type BottomTabId, type HomeLayoutSettings, type HomeModuleId } from "$lib/settings";
  import { BOTTOM_TAB_META, HOME_MODULE_META, isFixedBottomTab } from "$lib/app-registry";

  let {
    homeLayout = $bindable<HomeLayoutSettings>(),
    homeLayoutError,
    homeLayoutSaved,
    homeLayoutReseted,
    defaultPanelUrl,
    onsave,
    onreset,
  } = $props<{
    homeLayout: HomeLayoutSettings;
    homeLayoutError: string;
    homeLayoutSaved: boolean;
    homeLayoutReseted: boolean;
    defaultPanelUrl: string;
    onsave: () => void | Promise<void>;
    onreset: () => void;
  }>();

  const bottomTabOrder = $derived(homeLayout.bottomTabOrder as BottomTabId[]);
  const moduleOrder = $derived(homeLayout.moduleOrder as HomeModuleId[]);

  function isModuleVisible(moduleId: HomeModuleId): boolean {
    return !homeLayout.hiddenModules.includes(moduleId);
  }

  function toggleModuleVisible(moduleId: HomeModuleId) {
    if (isModuleVisible(moduleId)) {
      homeLayout.hiddenModules.push(moduleId);
      return;
    }

    const idx = homeLayout.hiddenModules.indexOf(moduleId);
    if (idx !== -1) homeLayout.hiddenModules.splice(idx, 1);
  }

  function moveModule(index: number, direction: -1 | 1) {
    const target = index + direction;
    if (target < 0 || target >= moduleOrder.length) return;

    const order = homeLayout.moduleOrder;
    const [item] = order.splice(index, 1);
    order.splice(target, 0, item);
  }

  function isBottomTabVisible(tabId: BottomTabId): boolean {
    if (isFixedBottomTab(tabId)) return true;
    return !homeLayout.bottomTabHidden.includes(tabId);
  }

  function toggleBottomTabVisible(tabId: BottomTabId) {
    if (isFixedBottomTab(tabId)) return;

    if (isBottomTabVisible(tabId)) {
      homeLayout.bottomTabHidden.push(tabId);
      return;
    }

    const idx = homeLayout.bottomTabHidden.indexOf(tabId);
    if (idx !== -1) homeLayout.bottomTabHidden.splice(idx, 1);
  }

  function moveBottomTab(index: number, direction: -1 | 1) {
    const target = index + direction;
    if (target < 0 || target >= bottomTabOrder.length) return;

    const order = homeLayout.bottomTabOrder;
    const [item] = order.splice(index, 1);
    order.splice(target, 0, item);
  }
</script>

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

  <div class="space-y-2">
    <p class="text-sm font-bold text-slate-900 dark:text-slate-200">底部 Tab 显示与排序</p>
    {#each bottomTabOrder as tabId, index (tabId)}
      <div animate:flip={{ duration: 220, easing: cubicOut }} class="flex items-center justify-between border border-slate-300 dark:border-zinc-700 px-3 py-2 bg-white dark:bg-zinc-950/50 rounded-lg">
        {#if isFixedBottomTab(tabId)}
          <div class="pointer-events-none opacity-90">
            <CheckBox id={`tab-${tabId}`} checked={true} onchange={() => {}} label={`${BOTTOM_TAB_META[tabId].label} (固定)`} bare />
          </div>
        {:else}
          <CheckBox id={`tab-${tabId}`} checked={isBottomTabVisible(tabId)} onchange={() => toggleBottomTabVisible(tabId)} label={BOTTOM_TAB_META[tabId].label} bare />
        {/if}
        <div class="flex items-center gap-1">
          <button
            type="button"
            onclick={() => moveBottomTab(index, -1)}
            class="inline-flex items-center justify-center p-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-slate-300 disabled:opacity-40 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors rounded-lg"
            disabled={index === 0}
            aria-label="上移"
          >
            <ArrowUp size={14} />
          </button>
          <button
            type="button"
            onclick={() => moveBottomTab(index, 1)}
            class="inline-flex items-center justify-center p-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-slate-300 disabled:opacity-40 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors rounded-lg"
            disabled={index === bottomTabOrder.length - 1}
            aria-label="下移"
          >
            <ArrowDown size={14} />
          </button>
        </div>
      </div>
    {/each}
    <span class="text-xs text-slate-500 dark:text-slate-400">设置 TAB 栏固定显示</span>
  </div>

  <div class="h-px bg-slate-200 dark:bg-zinc-800"></div>

  <div class="flex flex-col gap-2">
    <label for="panel-url" class="text-sm font-bold text-slate-900 dark:text-slate-200">面板快捷跳转 URL</label>
    <input
      id="panel-url"
      type="text"
      bind:value={homeLayout.panelUrl}
      class="px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors rounded-lg"
      placeholder={defaultPanelUrl}
    />
    <span class="text-xs text-slate-500 dark:text-slate-400">留空使用默认：{defaultPanelUrl}</span>
  </div>

  <div class="flex flex-col gap-2">
    <label for="proxy-test-url" class="text-sm font-bold text-slate-900 dark:text-slate-200">代理测速 URL</label>
    <input
      id="proxy-test-url"
      type="text"
      bind:value={homeLayout.proxyTestUrl}
      class="px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors rounded-lg"
      placeholder="http://cp.cloudflare.com/generate_204"
    />
    <span class="text-xs text-slate-500 dark:text-slate-400">代理页面测速默认使用该 URL，可按网络环境自行调整</span>
  </div>

  <div class="h-px bg-slate-200 dark:bg-zinc-800"></div>

  <div class="space-y-2">
    <p class="text-sm font-bold text-slate-900 dark:text-slate-200">首页模块显示与排序</p>
    {#each moduleOrder as moduleId, index (moduleId)}
      <div animate:flip={{ duration: 220, easing: cubicOut }} class="flex items-center justify-between border border-slate-300 dark:border-zinc-700 px-3 py-2 bg-white dark:bg-zinc-950/50 rounded-lg">
        <CheckBox id={`module-${moduleId}`} checked={isModuleVisible(moduleId)} onchange={() => toggleModuleVisible(moduleId)} label={HOME_MODULE_META[moduleId].label} bare />
        <div class="flex items-center gap-1">
          <button
            type="button"
            onclick={() => moveModule(index, -1)}
            class="inline-flex items-center justify-center p-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-slate-300 disabled:opacity-40 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors rounded-lg"
            disabled={index === 0}
            aria-label="上移"
          >
            <ArrowUp size={14} />
          </button>
          <button
            type="button"
            onclick={() => moveModule(index, 1)}
            class="inline-flex items-center justify-center p-1.5 border border-slate-300 dark:border-zinc-700 text-slate-600 dark:text-slate-300 disabled:opacity-40 hover:bg-slate-100 dark:hover:bg-zinc-800 transition-colors rounded-lg"
            disabled={index === moduleOrder.length - 1}
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
      type="button"
      onclick={onsave}
      class="group relative overflow-hidden flex-1 border border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 px-4 py-2.5 text-sm font-bold transition-all duration-300 outline-none hover:bg-slate-700 dark:hover:bg-slate-300 hover:shadow-[0_8px_20px_rgba(15,23,42,0.15)] active:translate-y-px rounded-lg"
    >
      <div class="grid place-items-center">
        {#if homeLayoutSaved}
          <span in:fly={{ y: 20, duration: 300, easing: cubicOut }} out:fly={{ y: -20, duration: 200 }} class="flex items-center gap-2 col-start-1 row-start-1 text-emerald-400 dark:text-emerald-600">
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
      onclick={onreset}
      class="group relative overflow-hidden border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-700 dark:text-slate-300 px-4 py-2.5 text-sm font-bold transition-colors outline-none hover:bg-slate-100 dark:hover:bg-zinc-900 rounded-lg"
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
