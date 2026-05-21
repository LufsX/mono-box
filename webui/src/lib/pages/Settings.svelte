<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import * as clashRealApi from "$lib/api/clash";
  import * as clashMockApi from "$lib/api/clash.mock";
  import * as actionRealApi from "$lib/api/action";
  import * as actionMockApi from "$lib/api/action.mock";
  import { getDefaultHomeLayoutSettings, loadHomeLayoutSettings, saveHomeLayoutSettings, roundedStore } from "$lib/settings";
  import AboutSettings from "$lib/components/settings/AboutSettings.svelte";
  import BoxConfigSettings from "$lib/components/settings/BoxConfigSettings.svelte";
  import CollapsibleSection from "$lib/components/settings/CollapsibleSection.svelte";
  import WebUiSettings from "$lib/components/settings/WebUiSettings.svelte";

  type ProxyMode = "rule" | "global" | "direct";
  type ControlMode = "disable" | "switch" | "tun" | "selector" | "mode";
  type SettingsSectionState = {
    boxConfig: boolean;
    webUi: boolean;
    about: boolean;
  };

  const isProd = import.meta.env.MODE !== "production";
  const clashApi = isProd ? clashMockApi : clashRealApi;
  const actionApi = isProd ? actionMockApi : actionRealApi;
  const ALL_PROXY_MODES: ProxyMode[] = ["rule", "global", "direct"];
  const SETTINGS_SECTION_STORAGE_KEY = "mono-box.settings-sections";
  const DEFAULT_SETTINGS_SECTION_STATE: SettingsSectionState = {
    boxConfig: true,
    webUi: true,
    about: true,
  };
  const initialSettingsSectionState = loadSettingsSectionState();

  let boxConfigPort = $state(9090);
  let boxConfigSecret = $state("");
  let boxConfigSaved = $state(false);
  let boxConfigLoading = $state(false);
  let boxConfigError = $state("");
  let clashApiChecking = $state(false);
  let clashApiCheckOk = $state<boolean | null>(null);
  let clashApiCheckResetTimer: ReturnType<typeof setTimeout> | undefined;
  let toggleAction = $state<"service" | "tun" | "mode_cycle">("service");
  let toggleTunTarget = $state<"toggle" | "on" | "off">("toggle");
  let toggleModeCycle = $state<ProxyMode[]>(["rule", "global", "direct"]);
  let toggleModeOrder = $state<ProxyMode[]>([...ALL_PROXY_MODES]);
  let controlMode = $state<ControlMode>("disable");
  let selectOutbound = $state("");
  let targetCellular = $state("");
  let targetWifi = $state("");
  let targetWifiList = $state("");

  let currentVersion = $state("-");
  let currentVersionCode = $state("");
  let currentUpdateJson = $state("");
  let saveResetTimer: ReturnType<typeof setTimeout> | undefined;
  let homeLayout = $state(loadHomeLayoutSettings());
  let homeLayoutSaved = $state(false);
  let homeLayoutReseted = $state(false);
  let homeLayoutError = $state("");
  let homeLayoutSaveResetTimer: ReturnType<typeof setTimeout> | undefined;
  let homeLayoutResetStateTimer: ReturnType<typeof setTimeout> | undefined;
  let boxConfigOpen = $state(initialSettingsSectionState.boxConfig);
  let webUiSettingsOpen = $state(initialSettingsSectionState.webUi);
  let aboutOpen = $state(initialSettingsSectionState.about);

  function normalizeSettingsSectionState(input: unknown): SettingsSectionState {
    if (!input || typeof input !== "object") {
      return { ...DEFAULT_SETTINGS_SECTION_STATE };
    }

    const raw = input as Partial<Record<keyof SettingsSectionState, unknown>>;
    return {
      boxConfig: typeof raw.boxConfig === "boolean" ? raw.boxConfig : DEFAULT_SETTINGS_SECTION_STATE.boxConfig,
      webUi: typeof raw.webUi === "boolean" ? raw.webUi : DEFAULT_SETTINGS_SECTION_STATE.webUi,
      about: typeof raw.about === "boolean" ? raw.about : DEFAULT_SETTINGS_SECTION_STATE.about,
    };
  }

  function loadSettingsSectionState(): SettingsSectionState {
    if (typeof window === "undefined") {
      return { ...DEFAULT_SETTINGS_SECTION_STATE };
    }

    try {
      const raw = localStorage.getItem(SETTINGS_SECTION_STORAGE_KEY);
      return raw ? normalizeSettingsSectionState(JSON.parse(raw)) : { ...DEFAULT_SETTINGS_SECTION_STATE };
    } catch {
      return { ...DEFAULT_SETTINGS_SECTION_STATE };
    }
  }

  function saveSettingsSectionState(next: Partial<SettingsSectionState> = {}): void {
    if (typeof window === "undefined") {
      return;
    }

    const state = {
      boxConfig: next.boxConfig ?? boxConfigOpen,
      webUi: next.webUi ?? webUiSettingsOpen,
      about: next.about ?? aboutOpen,
    } satisfies SettingsSectionState;

    boxConfigOpen = state.boxConfig;
    webUiSettingsOpen = state.webUi;
    aboutOpen = state.about;

    try {
      localStorage.setItem(SETTINGS_SECTION_STORAGE_KEY, JSON.stringify(state));
    } catch {
      // Keep controls usable in restricted WebViews.
    }
  }

  onMount(() => {
    loadBoxConfig();
    loadModuleInfo();
  });

  onDestroy(() => {
    if (saveResetTimer) clearTimeout(saveResetTimer);
    if (homeLayoutSaveResetTimer) clearTimeout(homeLayoutSaveResetTimer);
    if (homeLayoutResetStateTimer) clearTimeout(homeLayoutResetStateTimer);
    if (clashApiCheckResetTimer) clearTimeout(clashApiCheckResetTimer);
  });

  function triggerSavedState() {
    boxConfigSaved = true;
    if (saveResetTimer) clearTimeout(saveResetTimer);
    saveResetTimer = setTimeout(() => {
      boxConfigSaved = false;
      saveResetTimer = undefined;
    }, 1800);
  }

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

  function quoteBoxConfigValue(value: string): string {
    return `"${value.replace(/\r?\n/g, ";").replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\$/g, "\\$").replace(/`/g, "\\`").trim()}"`;
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
      controlMode = parsed.controlMode;
      selectOutbound = parsed.selectOutbound;
      targetCellular = parsed.targetCellular;
      targetWifi = parsed.targetWifi;
      targetWifiList = parsed.targetWifiList;
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
        clash_api_secret: quoteBoxConfigValue(boxConfigSecret),
        toggle_action: `"${toggleAction}"`,
        toggle_tun_target: `"${toggleTunTarget}"`,
        toggle_mode_cycle: `"${toggleModeOrder.filter((mode) => toggleModeCycle.includes(mode)).join(",")}"`,
        ctr_mode: controlMode,
        select_outbound: quoteBoxConfigValue(selectOutbound),
        target_cellular: quoteBoxConfigValue(targetCellular),
        target_wifi: quoteBoxConfigValue(targetWifi),
        target_wifi_list: quoteBoxConfigValue(targetWifiList),
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
      if (clashApiCheckResetTimer) clearTimeout(clashApiCheckResetTimer);
      clashApiChecking = true;
      clashApiCheckOk = null;

      const result = await clashApi.checkVersion({
        port: boxConfigPort,
        secret: boxConfigSecret,
      });

      clashApiCheckOk = result.ok;
    } catch {
      clashApiCheckOk = false;
    } finally {
      clashApiChecking = false;
      if (clashApiCheckResetTimer) clearTimeout(clashApiCheckResetTimer);
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
      currentUpdateJson = info.updateJson || "";
    } catch (e) {
      console.error("Failed to load module info:", e);
      currentVersion = "-";
      currentVersionCode = "";
      currentUpdateJson = "";
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

    if (homeLayoutResetStateTimer) clearTimeout(homeLayoutResetStateTimer);
    homeLayoutResetStateTimer = setTimeout(() => {
      homeLayoutReseted = false;
      homeLayoutResetStateTimer = undefined;
    }, 1800);
  }
</script>

<div class="max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-6">
  <CollapsibleSection title="box.config 配置" controls="box-config-settings" bind:open={boxConfigOpen} onchange={(open) => saveSettingsSectionState({ boxConfig: open })}>
    <BoxConfigSettings
      bind:boxConfigPort
      bind:boxConfigSecret
      {boxConfigSaved}
      {boxConfigLoading}
      bind:boxConfigError
      {clashApiChecking}
      {clashApiCheckOk}
      bind:toggleAction
      bind:toggleTunTarget
      bind:toggleModeCycle
      bind:toggleModeOrder
      bind:controlMode
      bind:selectOutbound
      bind:targetCellular
      bind:targetWifi
      bind:targetWifiList
      onsave={saveBoxConfig}
      oncheckapi={checkClashApiVersion}
    />
  </CollapsibleSection>

  <CollapsibleSection title="WEB UI 设置" controls="web-ui-settings" delay={50} bind:open={webUiSettingsOpen} onchange={(open) => saveSettingsSectionState({ webUi: open })}>
    <WebUiSettings bind:homeLayout {homeLayoutError} {homeLayoutSaved} {homeLayoutReseted} defaultPanelUrl={getDefaultPanelUrl()} onsave={saveHomeLayout} onreset={resetHomeLayoutToDefault} />
  </CollapsibleSection>

  <CollapsibleSection title="关于" controls="about-settings" delay={80} bind:open={aboutOpen} onchange={(open) => saveSettingsSectionState({ about: open })}>
    <AboutSettings {currentVersion} {currentVersionCode} {currentUpdateJson} onopen={openSupportLink} />
  </CollapsibleSection>
</div>
