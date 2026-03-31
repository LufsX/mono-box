<script lang="ts">
  import { dev } from "$app/environment";
  import { onDestroy, onMount } from "svelte";
  import { cubicOut } from "svelte/easing";
  import { fly } from "svelte/transition";
  import { Check, ChevronRight, Loader, Save } from "@lucide/svelte";
  import * as clashRealApi from "$lib/api/clash";
  import * as clashMockApi from "$lib/api/clash.mock";
  import * as actionRealApi from "$lib/api/action";
  import * as actionMockApi from "$lib/api/action.mock";

  const clashApi = dev ? clashMockApi : clashRealApi;
  const actionApi = dev ? actionMockApi : actionRealApi;

  // box.config 配置
  let boxConfigPort = $state(9090);
  let boxConfigSecret = $state("");
  let boxConfigSaved = $state(false);
  let boxConfigLoading = $state(false);
  let boxConfigError = $state("");
  let currentVersion = $state("-");
  let currentVersionCode = $state("");
  let saveResetTimer: ReturnType<typeof setTimeout> | undefined;

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
  });

  async function loadBoxConfig() {
    try {
      boxConfigLoading = true;
      boxConfigError = "";
      const content = await clashApi.readBoxConfig();

      const parsed = clashApi.parseBoxConfig(content);
      boxConfigPort = parsed.clashApiPort;
      boxConfigSecret = parsed.clashApiSecret;
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
</script>

<div class="max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-6">
  <section in:fly={{ y: 12, duration: 260, delay: 20, easing: cubicOut }} class="bg-white dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-colors">
    <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950">
      <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">Clash API 配置</h2>
    </div>
    <div class="p-4 space-y-4">
      {#if boxConfigError}
        <div transition:fly={{ y: -8, duration: 220, easing: cubicOut }} class="px-3 py-2 bg-red-50 dark:bg-red-950 border border-red-300 dark:border-red-800 text-red-700 dark:text-red-300 text-sm">
          {boxConfigError}
        </div>
      {/if}

      <div class="flex flex-col gap-2">
        <label for="box-api-port" class="text-sm font-bold text-slate-900 dark:text-slate-200">Clash API Port</label>
        <input
          id="box-api-port"
          type="number"
          bind:value={boxConfigPort}
          disabled={boxConfigLoading}
          class="px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors disabled:opacity-50"
          placeholder="9090"
        />
        <span class="text-xs text-slate-500 dark:text-slate-400">Clash API 外部控制端口</span>
      </div>

      <div class="flex flex-col gap-2">
        <label for="box-api-secret" class="text-sm font-bold text-slate-900 dark:text-slate-200">Clash API Secret</label>
        <input
          id="box-api-secret"
          type="text"
          bind:value={boxConfigSecret}
          disabled={boxConfigLoading}
          class="px-3 py-2 border border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-900 dark:text-slate-200 outline-none focus:border-slate-800 dark:focus:border-slate-400 transition-colors font-mono disabled:opacity-50"
          placeholder="留空表示无密钥"
        />
        <span class="text-xs text-slate-500 dark:text-slate-400">Clash API 访问密钥</span>
      </div>

      <div class="flex gap-3 pt-2">
        <button
          onclick={saveBoxConfig}
          disabled={boxConfigLoading}
          class="group flex-1 border border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 backdrop-blur-md text-white dark:text-zinc-900 px-4 py-2.5 text-sm font-bold transition-all duration-300 outline-none disabled:opacity-60 disabled:cursor-not-allowed hover:bg-slate-700 dark:hover:bg-slate-300 hover:shadow-[0_10px_24px_rgba(15,23,42,0.18)] active:translate-y-px"
        >
          <span class="flex items-center justify-center gap-2">
            {#if boxConfigLoading}
              <Loader size={16} class="animate-spin" />
              保存中…
            {:else if boxConfigSaved}
              <Check size={16} class="text-emerald-200 dark:text-emerald-700" />
              保存成功
            {:else}
              <Save size={16} class="transition-transform duration-300 group-hover:scale-105" />
              保存配置
            {/if}
          </span>
        </button>
      </div>

      <!-- <div class="px-3 py-2 bg-blue-50 dark:bg-blue-950 border border-blue-300 dark:border-blue-800 text-blue-700 dark:text-blue-300 text-xs">注意：修改配置后需要重启服务才能生效</div> -->
    </div>
  </section>

  <section
    in:fly={{ y: 12, duration: 260, delay: 70, easing: cubicOut }}
    class="bg-white/88 dark:bg-zinc-900 border border-slate-300 dark:border-zinc-700 transition-all duration-300 cursor-pointer backdrop-blur-sm hover:bg-slate-50/92 dark:hover:bg-zinc-900 hover:border-slate-400 dark:hover:border-zinc-500"
    role="button"
    tabindex="0"
    onclick={() => openSupportLink("https://github.com/LufsX/mono-box")}
    onkeydown={(e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        void openSupportLink("https://github.com/LufsX/mono-box");
      }
    }}
  >
    <div class="px-4 py-3 border-b border-slate-300 dark:border-zinc-700 bg-slate-50 dark:bg-zinc-950">
      <h2 class="text-xs font-bold uppercase tracking-widest text-slate-600 dark:text-zinc-400 m-0">关于</h2>
    </div>
    <div class="p-4 text-sm text-slate-600 dark:text-slate-400">
      <div class="flex items-center justify-between gap-3">
        <div class="space-y-2">
          <p>Mono Box {currentVersion}{currentVersionCode ? ` (${currentVersionCode})` : ""}</p>
          <p class="text-xs">基于 Mihomo 核心的代理工具</p>
        </div>
        <ChevronRight size={18} class="text-slate-400 dark:text-zinc-500" aria-hidden="true" />
      </div>
    </div>
  </section>
</div>
