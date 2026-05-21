<script lang="ts">
  import { flip } from "svelte/animate";
  import { cubicOut } from "svelte/easing";
  import { fly, slide } from "svelte/transition";
  import { ArrowDown, ArrowUp, Check, Eye, EyeOff, RefreshCw, Save, X } from "@lucide/svelte";
  import CheckBox from "$lib/components/CheckBox.svelte";
  import Select from "$lib/components/Select.svelte";
  import { roundedStore } from "$lib/settings";

  type ProxyMode = "rule" | "global" | "direct";
  type ControlMode = "disable" | "switch" | "tun" | "selector" | "mode";
  type ToggleAction = "service" | "tun" | "mode_cycle";
  type ToggleTunTarget = "toggle" | "on" | "off";

  let {
    boxConfigPort = $bindable<number>(),
    boxConfigSecret = $bindable<string>(),
    boxConfigSaved,
    boxConfigLoading,
    boxConfigError = $bindable<string>(),
    clashApiChecking,
    clashApiCheckOk,
    toggleAction = $bindable<ToggleAction>(),
    toggleTunTarget = $bindable<ToggleTunTarget>(),
    toggleModeCycle = $bindable<ProxyMode[]>(),
    toggleModeOrder = $bindable<ProxyMode[]>(),
    controlMode = $bindable<ControlMode>(),
    selectOutbound = $bindable<string>(),
    targetCellular = $bindable<string>(),
    targetWifi = $bindable<string>(),
    targetWifiList = $bindable<string>(),
    onsave,
    oncheckapi,
  } = $props<{
    boxConfigPort: number;
    boxConfigSecret: string;
    boxConfigSaved: boolean;
    boxConfigLoading: boolean;
    boxConfigError: string;
    clashApiChecking: boolean;
    clashApiCheckOk: boolean | null;
    toggleAction: ToggleAction;
    toggleTunTarget: ToggleTunTarget;
    toggleModeCycle: ProxyMode[];
    toggleModeOrder: ProxyMode[];
    controlMode: ControlMode;
    selectOutbound: string;
    targetCellular: string;
    targetWifi: string;
    targetWifiList: string;
    onsave: () => void | Promise<void>;
    oncheckapi: () => void | Promise<void>;
  }>();

  const ALL_PROXY_MODES: ProxyMode[] = ["rule", "global", "direct"];
  const r = $derived($roundedStore);
  const typedToggleModeOrder = $derived(toggleModeOrder as ProxyMode[]);
  const typedToggleModeCycle = $derived(toggleModeCycle as ProxyMode[]);
  let boxConfigSecretVisible = $state(false);

  const proxyModeLabels: Record<ProxyMode, string> = {
    rule: "规则",
    global: "全局",
    direct: "直连",
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

  const controlModeOptions = [
    { value: "disable", label: "禁用事件控制" },
    { value: "switch", label: "启停核心" },
    { value: "tun", label: "切换 TUN" },
    { value: "selector", label: "切换代理策略组" },
    { value: "mode", label: "切换 Clash 模式" },
  ];

  const serviceStateOptions = [
    { value: "", label: "不改变" },
    { value: "start", label: "启动" },
    { value: "stop", label: "停止" },
  ];

  const tunStateOptions = [
    { value: "", label: "不改变" },
    { value: "true", label: "开启" },
    { value: "false", label: "关闭" },
  ];

  const clashModeTargetOptions = [
    { value: "", label: "不指定" },
    { value: "rule", label: "规则" },
    { value: "global", label: "全局" },
    { value: "direct", label: "直连" },
  ];

  const clashModeCellularTargetOptions = [...clashModeTargetOptions, { value: "last", label: "恢复上次状态" }];

  function applyControlModeDefaults(mode: ControlMode) {
    targetWifiList = "";

    if (mode === "switch") {
      targetCellular = "start";
      targetWifi = "stop";
      return;
    }

    if (mode === "tun") {
      targetCellular = "true";
      targetWifi = "false";
      return;
    }

    if (mode === "selector") {
      targetCellular = "last";
      targetWifi = "DIRECT";
      return;
    }

    if (mode === "mode") {
      targetCellular = "last";
      targetWifi = "direct";
      return;
    }

    targetCellular = "";
    targetWifi = "";
  }

  function toggleModeCycleEntry(mode: ProxyMode) {
    if (typedToggleModeCycle.includes(mode)) {
      const next = typedToggleModeCycle.filter((item: ProxyMode) => item !== mode);
      if (!next.length) {
        boxConfigError = "模式循环至少需要一个状态";
        return;
      }
      toggleModeCycle = next;
      boxConfigError = "";
      return;
    }

    toggleModeCycle = [...typedToggleModeCycle, mode];
    boxConfigError = "";
  }

  function moveToggleModeCycle(index: number, direction: -1 | 1) {
    const targetIndex = index + direction;
    if (targetIndex < 0 || targetIndex >= typedToggleModeOrder.length) return;

    const next = [...typedToggleModeOrder];
    const [item] = next.splice(index, 1);
    next.splice(targetIndex, 0, item);
    toggleModeOrder = next;
  }
</script>

<form
  class="p-4 space-y-4"
  autocomplete="off"
  onsubmit={(event) => {
    event.preventDefault();
    onsave();
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
        class="min-w-0 px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors disabled:opacity-50 {r
          ? 'rounded-lg'
          : ''}"
        placeholder="9090"
      />

      <button
        type="button"
        onclick={oncheckapi}
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
            <span in:fly={{ y: 20, duration: 300, easing: cubicOut }} out:fly={{ y: -20, duration: 200 }} class="inline-flex items-center justify-center gap-1.5 col-start-1 row-start-1 w-full min-w-0">
              <RefreshCw size={14} class="animate-spin" />
              <span class="truncate">正在检测</span>
            </span>
          {:else if clashApiCheckOk === true}
            <span in:fly={{ y: 20, duration: 300, easing: cubicOut }} out:fly={{ y: -20, duration: 200 }} class="inline-flex items-center justify-center gap-1.5 col-start-1 row-start-1 w-full min-w-0">
              <Check size={14} />
              <span class="truncate">连接成功</span>
            </span>
          {:else if clashApiCheckOk === false}
            <span in:fly={{ y: 20, duration: 300, easing: cubicOut }} out:fly={{ y: -20, duration: 200 }} class="inline-flex items-center justify-center gap-1.5 col-start-1 row-start-1 w-full min-w-0">
              <X size={14} />
              <span class="truncate">连接失败</span>
            </span>
          {:else}
            <span in:fly={{ y: 20, duration: 300, easing: cubicOut }} out:fly={{ y: -20, duration: 200 }} class="inline-flex items-center justify-center gap-1.5 col-start-1 row-start-1 w-full min-w-0">
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
        class="w-full px-3 pr-10 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors font-mono disabled:opacity-50 {r
          ? 'rounded-lg'
          : ''}"
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
            {#each typedToggleModeOrder as mode, index (mode)}
              <div animate:flip={{ duration: 220, easing: cubicOut }} class="flex items-center justify-between border border-slate-300 dark:border-zinc-700 px-3 py-2 bg-white dark:bg-zinc-950/50 {r ? 'rounded-lg' : ''}">
                <CheckBox id={`mode-cycle-${mode}`} checked={typedToggleModeCycle.includes(mode)} onchange={() => toggleModeCycleEntry(mode)} label={proxyModeLabels[mode]} bare />
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
                    disabled={index === typedToggleModeOrder.length - 1 || boxConfigLoading}
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

  <div class="h-px bg-slate-200 dark:bg-zinc-800"></div>

  <div class="space-y-4">
    <div class="flex flex-col gap-2">
      <label for="control-mode" class="text-sm font-bold text-slate-900 dark:text-slate-200">网络事件控制</label>
      <Select id="control-mode" bind:value={controlMode} options={controlModeOptions} disabled={boxConfigLoading} onchange={(value) => applyControlModeDefaults(value as ControlMode)} />
      <span class="text-xs text-slate-500 dark:text-slate-400">监听 Wi-Fi / 蜂窝网络变化后执行对应策略</span>
    </div>

    {#if controlMode === "switch" || controlMode === "tun" || controlMode === "selector" || controlMode === "mode"}
      <div in:slide={{ duration: 220, easing: cubicOut }} out:slide={{ duration: 180, easing: cubicOut }} class="space-y-4">
        {#if controlMode === "switch"}
          <div class="space-y-4">
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div class="flex flex-col gap-2">
                <label for="target-cellular-service" class="text-sm font-bold text-slate-900 dark:text-slate-200">蜂窝核心动作</label>
                <Select id="target-cellular-service" bind:value={targetCellular} options={serviceStateOptions} disabled={boxConfigLoading} />
              </div>
              <div class="flex flex-col gap-2">
                <label for="target-wifi-service" class="text-sm font-bold text-slate-900 dark:text-slate-200">Wi-Fi 核心动作</label>
                <Select id="target-wifi-service" bind:value={targetWifi} options={serviceStateOptions} disabled={boxConfigLoading} />
              </div>
            </div>

            <div class="flex flex-col gap-2">
              <label for="target-wifi-list-service" class="text-sm font-bold text-slate-900 dark:text-slate-200">SSID 核心动作映射</label>
              <textarea id="target-wifi-list-service" bind:value={targetWifiList} disabled={boxConfigLoading} rows="2" class="px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors disabled:opacity-50 resize-y font-mono text-sm {r ? 'rounded-lg' : ''}" placeholder="CMCC_XXXX,start;TPLINK_XXXX,stop"></textarea>
              <span class="text-xs text-slate-500 dark:text-slate-400">留空则所有 Wi-Fi 使用上方动作；格式：SSID,start/stop;SSID,start/stop</span>
            </div>
          </div>
        {:else if controlMode === "tun"}
          <div class="space-y-4">
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div class="flex flex-col gap-2">
                <label for="target-cellular-tun" class="text-sm font-bold text-slate-900 dark:text-slate-200">蜂窝目标 TUN</label>
                <Select id="target-cellular-tun" bind:value={targetCellular} options={tunStateOptions} disabled={boxConfigLoading} />
              </div>
              <div class="flex flex-col gap-2">
                <label for="target-wifi-tun" class="text-sm font-bold text-slate-900 dark:text-slate-200">Wi-Fi 目标 TUN</label>
                <Select id="target-wifi-tun" bind:value={targetWifi} options={tunStateOptions} disabled={boxConfigLoading} />
              </div>
            </div>

            <div class="flex flex-col gap-2">
              <label for="target-wifi-list-tun" class="text-sm font-bold text-slate-900 dark:text-slate-200">SSID TUN 映射</label>
              <textarea id="target-wifi-list-tun" bind:value={targetWifiList} disabled={boxConfigLoading} rows="2" class="px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors disabled:opacity-50 resize-y font-mono text-sm {r ? 'rounded-lg' : ''}" placeholder="CMCC_XXXX,true;TPLINK_XXXX,false"></textarea>
              <span class="text-xs text-slate-500 dark:text-slate-400">留空则所有 Wi-Fi 使用上方目标；格式：SSID,true/false;SSID,true/false</span>
            </div>
          </div>
        {:else if controlMode === "selector"}
          <div class="space-y-4">
            <div class="flex flex-col gap-2">
              <label for="select-outbound" class="text-sm font-bold text-slate-900 dark:text-slate-200">策略组名称</label>
              <input id="select-outbound" type="text" bind:value={selectOutbound} disabled={boxConfigLoading} class="px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors disabled:opacity-50 {r ? 'rounded-lg' : ''}" placeholder="Proxy" />
              <span class="text-xs text-slate-500 dark:text-slate-400">对应 Clash API 中的 selector 代理组</span>
            </div>

            <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div class="flex flex-col gap-2">
                <label for="target-cellular-outbound" class="text-sm font-bold text-slate-900 dark:text-slate-200">蜂窝目标</label>
                <input id="target-cellular-outbound" type="text" bind:value={targetCellular} disabled={boxConfigLoading} class="px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors disabled:opacity-50 {r ? 'rounded-lg' : ''}" placeholder="Proxy / last / 留空不改变" />
              </div>
              <div class="flex flex-col gap-2">
                <label for="target-wifi-outbound" class="text-sm font-bold text-slate-900 dark:text-slate-200">Wi-Fi 目标</label>
                <input id="target-wifi-outbound" type="text" bind:value={targetWifi} disabled={boxConfigLoading} class="px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors disabled:opacity-50 {r ? 'rounded-lg' : ''}" placeholder="DIRECT" />
              </div>
            </div>

            <div class="flex flex-col gap-2">
              <label for="target-wifi-list-outbound" class="text-sm font-bold text-slate-900 dark:text-slate-200">SSID 出站映射</label>
              <textarea id="target-wifi-list-outbound" bind:value={targetWifiList} disabled={boxConfigLoading} rows="2" class="px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors disabled:opacity-50 resize-y font-mono text-sm {r ? 'rounded-lg' : ''}" placeholder="CMCC_XXXX,DIRECT;TPLINK_XXXX,Proxy"></textarea>
              <span class="text-xs text-slate-500 dark:text-slate-400">留空则所有 Wi-Fi 使用上方目标；格式：SSID,出站;SSID,出站</span>
            </div>
          </div>
        {:else if controlMode === "mode"}
          <div class="space-y-4">
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div class="flex flex-col gap-2">
                <label for="target-cellular-mode" class="text-sm font-bold text-slate-900 dark:text-slate-200">蜂窝目标模式</label>
                <Select id="target-cellular-mode" bind:value={targetCellular} options={clashModeCellularTargetOptions} disabled={boxConfigLoading} />
              </div>
              <div class="flex flex-col gap-2">
                <label for="target-wifi-mode" class="text-sm font-bold text-slate-900 dark:text-slate-200">Wi-Fi 目标模式</label>
                <Select id="target-wifi-mode" bind:value={targetWifi} options={clashModeTargetOptions} disabled={boxConfigLoading} />
              </div>
            </div>

            <div class="flex flex-col gap-2">
              <label for="target-wifi-list-mode" class="text-sm font-bold text-slate-900 dark:text-slate-200">SSID 模式映射</label>
              <textarea id="target-wifi-list-mode" bind:value={targetWifiList} disabled={boxConfigLoading} rows="2" class="px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors disabled:opacity-50 resize-y font-mono text-sm {r ? 'rounded-lg' : ''}" placeholder="CMCC_XXXX,rule;TPLINK_XXXX,direct"></textarea>
              <span class="text-xs text-slate-500 dark:text-slate-400">留空则所有 Wi-Fi 使用上方目标；格式：SSID,rule/global/direct;SSID,rule/global/direct</span>
            </div>
          </div>
        {/if}
      </div>
    {/if}
  </div>

  <div class="flex gap-3 pt-2">
    <button
      type="submit"
      disabled={boxConfigLoading}
      class="group relative overflow-hidden flex-1 border border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 px-4 py-2.5 text-sm font-bold transition-all duration-300 outline-none disabled:opacity-80 hover:bg-slate-700 dark:hover:bg-slate-300 hover:shadow-[0_8px_20px_rgba(15,23,42,0.15)] active:translate-y-px {r
        ? 'rounded-lg'
        : ''}"
    >
      <div class="grid place-items-center">
        {#if boxConfigSaved}
          <span in:fly={{ y: 20, duration: 300, easing: cubicOut }} out:fly={{ y: -20, duration: 200 }} class="flex items-center gap-2 col-start-1 row-start-1 text-emerald-400 dark:text-emerald-600">
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
</form>
