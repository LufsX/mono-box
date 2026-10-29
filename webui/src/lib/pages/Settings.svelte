<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { clashApi, actionApi, createDefaultBoxConfigFormValues, getBoxConfigForm, saveBoxConfigForm, saveStoredClashConfig } from "$lib/api";
  import type { BoxConfigFormValues } from "$lib/api";
  import { getDefaultHomeLayoutSettings, loadHomeLayoutSettings, saveHomeLayoutSettings } from "$lib/settings";
  import AboutSettings from "$lib/components/settings/AboutSettings.svelte";
  import AutomationSettings from "$lib/components/settings/AutomationSettings.svelte";
  import BoxConfigSettings from "$lib/components/settings/BoxConfigSettings.svelte";
  import CollapsibleSection from "$lib/components/settings/CollapsibleSection.svelte";
  import MihomoConfigSettings from "$lib/components/settings/MihomoConfigSettings.svelte";
  import WebUiSettings from "$lib/components/settings/WebUiSettings.svelte";

  type SettingsSectionState = {
    mihomoConfig: boolean;
    automation: boolean;
    boxConfig: boolean;
    webUi: boolean;
    about: boolean;
  };

  const SETTINGS_SECTION_STORAGE_KEY = "mono-box.settings-sections";
  const DEFAULT_SETTINGS_SECTION_STATE: SettingsSectionState = {
    mihomoConfig: true,
    automation: false,
    boxConfig: true,
    webUi: true,
    about: true,
  };
  const initialSettingsSectionState = loadSettingsSectionState();

  let boxConfig = $state<BoxConfigFormValues>(createDefaultBoxConfigFormValues());
  let boxConfigSaved = $state(false);
  let boxConfigLoading = $state(false);
  let boxConfigLoaded = $state(false);
  let boxConfigSaving = $state(false);
  let boxConfigError = $state("");
  let clashApiChecking = $state(false);
  let clashApiCheckOk = $state<boolean | null>(null);
  let clashApiCheckResetTimer: ReturnType<typeof setTimeout> | undefined;

  let currentVersion = $state("-");
  let currentVersionCode = $state("");
  let currentUpdateJson = $state("");
  let currentModuleAbi = $state("");
  let moduleInfoLoading = $state(true);
  let moduleInfoError = $state("");
  let saveResetTimer: ReturnType<typeof setTimeout> | undefined;
  let homeLayout = $state(loadHomeLayoutSettings());
  let homeLayoutSaved = $state(false);
  let homeLayoutSaving = $state(false);
  let homeLayoutReseted = $state(false);
  let homeLayoutError = $state("");
  let homeLayoutSaveResetTimer: ReturnType<typeof setTimeout> | undefined;
  let homeLayoutResetStateTimer: ReturnType<typeof setTimeout> | undefined;
  let mihomoConfigOpen = $state(false);
  let mihomoConfigSection: CollapsibleSection | undefined;
  let mihomoConfigSettings: MihomoConfigSettings | undefined;
  let automationOpen = $state(initialSettingsSectionState.automation);
  let boxConfigOpen = $state(initialSettingsSectionState.boxConfig);
  let webUiSettingsOpen = $state(initialSettingsSectionState.webUi);
  let aboutOpen = $state(initialSettingsSectionState.about);

  function normalizeSettingsSectionState(input: unknown): SettingsSectionState {
    if (!input || typeof input !== "object") {
      return { ...DEFAULT_SETTINGS_SECTION_STATE };
    }

    const raw = input as Partial<Record<keyof SettingsSectionState, unknown>>;
    return {
      mihomoConfig: typeof raw.mihomoConfig === "boolean" ? raw.mihomoConfig : DEFAULT_SETTINGS_SECTION_STATE.mihomoConfig,
      automation: typeof raw.automation === "boolean" ? raw.automation : DEFAULT_SETTINGS_SECTION_STATE.automation,
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
      mihomoConfig: next.mihomoConfig ?? mihomoConfigOpen,
      automation: next.automation ?? automationOpen,
      boxConfig: next.boxConfig ?? boxConfigOpen,
      webUi: next.webUi ?? webUiSettingsOpen,
      about: next.about ?? aboutOpen,
    } satisfies SettingsSectionState;

    mihomoConfigOpen = state.mihomoConfig;
    automationOpen = state.automation;
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
    if (initialSettingsSectionState.mihomoConfig) void mihomoConfigSection?.expand();
    loadBoxConfig();
    loadModuleInfo();
  });

  onDestroy(() => {
    if (saveResetTimer) clearTimeout(saveResetTimer);
    if (homeLayoutSaveResetTimer) clearTimeout(homeLayoutSaveResetTimer);
    if (homeLayoutResetStateTimer) clearTimeout(homeLayoutResetStateTimer);
    if (clashApiCheckResetTimer) clearTimeout(clashApiCheckResetTimer);
  });

  async function loadMihomoConfigs() {
    if (!mihomoConfigSettings) throw new Error("配置管理尚未就绪，请重试");
    await mihomoConfigSettings.loadConfigs(true);
  }

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

  async function loadBoxConfig() {
    if (boxConfigLoading) return;
    try {
      boxConfigLoading = true;
      boxConfigError = "";
      boxConfig = await getBoxConfigForm();
      saveStoredClashConfig({ port: boxConfig.clashApiPort, secret: boxConfig.clashApiSecret });
      boxConfigLoaded = true;
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      boxConfigError = `无法读取 box.config 文件: ${message}`;
      console.error("Failed to load box.config:", e);
    } finally {
      boxConfigLoading = false;
    }
  }

  async function saveBoxConfig() {
    if (!boxConfigLoaded || boxConfigSaving) return;
    try {
      boxConfigSaving = true;
      boxConfigError = "";

      await saveBoxConfigForm(boxConfig);
      saveStoredClashConfig({ port: boxConfig.clashApiPort, secret: boxConfig.clashApiSecret });

      triggerSavedState();
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      boxConfigError = `保存失败: ${message}`;
      console.error("Failed to save box.config:", e);
    } finally {
      boxConfigSaving = false;
    }
  }

  async function checkClashApiVersion() {
    if (clashApiChecking) return;
    try {
      if (clashApiCheckResetTimer) clearTimeout(clashApiCheckResetTimer);
      clashApiChecking = true;
      clashApiCheckOk = null;

      const result = await clashApi.checkVersion({
        port: boxConfig.clashApiPort,
        secret: boxConfig.clashApiSecret,
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
    moduleInfoLoading = true;
    moduleInfoError = "";
    try {
      const info = await actionApi.getModuleInfo();
      currentVersion = info.version || "-";
      currentVersionCode = info.versionCode || "";
      currentUpdateJson = info.updateJson || "";
      currentModuleAbi = info.moduleAbi || "";
    } catch (e) {
      console.error("Failed to load module info:", e);
      moduleInfoError = (e instanceof Error ? e.message : String(e)) || "无法读取 module.prop";
      currentVersion = "-";
      currentVersionCode = "";
      currentUpdateJson = "";
      currentModuleAbi = "";
    } finally {
      moduleInfoLoading = false;
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
    return `http://127.0.0.1:${boxConfig.clashApiPort}/ui`;
  }

  async function saveHomeLayout() {
    if (homeLayoutSaving) return;
    const settings = {
      ...homeLayout,
      moduleOrder: [...homeLayout.moduleOrder],
      hiddenModules: [...homeLayout.hiddenModules],
      bottomTabOrder: [...homeLayout.bottomTabOrder],
      bottomTabHidden: [...homeLayout.bottomTabHidden],
    };
    try {
      homeLayoutSaving = true;
      homeLayoutError = "";
      await actionApi.setEdgeToEdge(settings.edgeToEdge);
      saveHomeLayoutSettings(settings);
      homeLayout = settings;
      triggerHomeLayoutSavedState();
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      homeLayoutError = `保存首页布局失败: ${message}`;
    } finally {
      homeLayoutSaving = false;
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

  function markHomeLayoutEdited() {
    homeLayoutSaved = false;
    homeLayoutReseted = false;
    homeLayoutError = "";
  }
</script>

<div class="max-w-3xl mx-auto px-4 py-6 min-h-full flex flex-col gap-6">
  <CollapsibleSection bind:this={mihomoConfigSection} title="Mihomo 配置管理" controls="mihomo-config-settings" keepMounted beforeopen={loadMihomoConfigs} bind:open={mihomoConfigOpen} onchange={(open) => saveSettingsSectionState({ mihomoConfig: open })}>
    <MihomoConfigSettings bind:this={mihomoConfigSettings} />
  </CollapsibleSection>

  <CollapsibleSection title="自动化" controls="automation-settings" bind:open={automationOpen} onchange={(open) => saveSettingsSectionState({ automation: open })}>
    <AutomationSettings />
  </CollapsibleSection>

  <CollapsibleSection title="box.config 配置" controls="box-config-settings" bind:open={boxConfigOpen} onchange={(open) => saveSettingsSectionState({ boxConfig: open })}>
    <BoxConfigSettings bind:boxConfig {boxConfigSaved} {boxConfigLoading} {boxConfigSaving} boxConfigLoadFailed={!boxConfigLoading && !boxConfigLoaded} bind:boxConfigError {clashApiChecking} {clashApiCheckOk} onsave={saveBoxConfig} oncheckapi={checkClashApiVersion} onreload={loadBoxConfig} />
  </CollapsibleSection>

  <CollapsibleSection title="WEB UI 设置" controls="web-ui-settings" delay={50} bind:open={webUiSettingsOpen} onchange={(open) => saveSettingsSectionState({ webUi: open })}>
    <WebUiSettings bind:homeLayout {homeLayoutError} {homeLayoutSaved} {homeLayoutReseted} saving={homeLayoutSaving} defaultPanelUrl={getDefaultPanelUrl()} onsave={saveHomeLayout} onreset={resetHomeLayoutToDefault} onedit={markHomeLayoutEdited} />
  </CollapsibleSection>

  <CollapsibleSection title="关于" controls="about-settings" delay={80} bind:open={aboutOpen} onchange={(open) => saveSettingsSectionState({ about: open })}>
    <AboutSettings {currentVersion} {currentVersionCode} {currentUpdateJson} {currentModuleAbi} {moduleInfoLoading} {moduleInfoError} onreloadmodule={loadModuleInfo} onopen={openSupportLink} />
  </CollapsibleSection>
</div>
