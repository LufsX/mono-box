<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { clashApi, actionApi, createDefaultBoxConfigFormValues, getBoxConfigForm, saveBoxConfigForm } from "$lib/api";
  import type { BoxConfigFormValues } from "$lib/api";
  import { getDefaultHomeLayoutSettings, loadHomeLayoutSettings, saveHomeLayoutSettings } from "$lib/settings";
  import AboutSettings from "$lib/components/settings/AboutSettings.svelte";
  import BoxConfigSettings from "$lib/components/settings/BoxConfigSettings.svelte";
  import CollapsibleSection from "$lib/components/settings/CollapsibleSection.svelte";
  import WebUiSettings from "$lib/components/settings/WebUiSettings.svelte";

  type SettingsSectionState = {
    boxConfig: boolean;
    webUi: boolean;
    about: boolean;
  };

  const SETTINGS_SECTION_STORAGE_KEY = "mono-box.settings-sections";
  const DEFAULT_SETTINGS_SECTION_STATE: SettingsSectionState = {
    boxConfig: true,
    webUi: true,
    about: true,
  };
  const initialSettingsSectionState = loadSettingsSectionState();

  let boxConfig = $state<BoxConfigFormValues>(createDefaultBoxConfigFormValues());
  let boxConfigSaved = $state(false);
  let boxConfigLoading = $state(false);
  let boxConfigError = $state("");
  let clashApiChecking = $state(false);
  let clashApiCheckOk = $state<boolean | null>(null);
  let clashApiCheckResetTimer: ReturnType<typeof setTimeout> | undefined;

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

  async function loadBoxConfig() {
    try {
      boxConfigLoading = true;
      boxConfigError = "";
      boxConfig = await getBoxConfigForm();
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

      await saveBoxConfigForm(boxConfig);

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
    return `http://127.0.0.1:${boxConfig.clashApiPort}/ui`;
  }

  async function saveHomeLayout() {
    try {
      homeLayoutError = "";
      saveHomeLayoutSettings(homeLayout);
      triggerHomeLayoutSavedState();
      await actionApi.setEdgeToEdge(homeLayout.edgeToEdge);
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
    <BoxConfigSettings bind:boxConfig {boxConfigSaved} {boxConfigLoading} bind:boxConfigError {clashApiChecking} {clashApiCheckOk} onsave={saveBoxConfig} oncheckapi={checkClashApiVersion} />
  </CollapsibleSection>

  <CollapsibleSection title="WEB UI 设置" controls="web-ui-settings" delay={50} bind:open={webUiSettingsOpen} onchange={(open) => saveSettingsSectionState({ webUi: open })}>
    <WebUiSettings bind:homeLayout {homeLayoutError} {homeLayoutSaved} {homeLayoutReseted} defaultPanelUrl={getDefaultPanelUrl()} onsave={saveHomeLayout} onreset={resetHomeLayoutToDefault} />
  </CollapsibleSection>

  <CollapsibleSection title="关于" controls="about-settings" delay={80} bind:open={aboutOpen} onchange={(open) => saveSettingsSectionState({ about: open })}>
    <AboutSettings {currentVersion} {currentVersionCode} {currentUpdateJson} onopen={openSupportLink} />
  </CollapsibleSection>
</div>
