import { writable } from "svelte/store";
import { ALL_BOTTOM_TABS, ALL_HOME_MODULES, isFixedBottomTab, type BottomTabId, type HomeModuleId } from "./app-registry";

export { ALL_BOTTOM_TABS, ALL_HOME_MODULES, isFixedBottomTab, type BottomTabId, type HomeModuleId } from "./app-registry";

export interface HomeLayoutSettings {
  version: 1;
  moduleOrder: HomeModuleId[];
  hiddenModules: HomeModuleId[];
  panelUrl: string;
  proxyTestUrl: string;
  edgeToEdge: boolean;
  rounded: boolean;
  bottomTabOrder: BottomTabId[];
  bottomTabHidden: BottomTabId[];
}

const STORAGE_KEY = "mono-box.home-layout";

const DEFAULT_SETTINGS: HomeLayoutSettings = {
  version: 1,
  moduleOrder: ["tun", "proxy", "stats", "core", "panel", "service", "log"],
  hiddenModules: [],
  panelUrl: "",
  proxyTestUrl: "http://cp.cloudflare.com/generate_204",
  edgeToEdge: true,
  rounded: false,
  bottomTabOrder: [...ALL_BOTTOM_TABS],
  bottomTabHidden: ["logs"],
};

export function getDefaultHomeLayoutSettings(): HomeLayoutSettings {
  return structuredClone(DEFAULT_SETTINGS);
}

export function normalizeHomeLayoutSettings(input: unknown): HomeLayoutSettings {
  if (!input || typeof input !== "object") {
    return getDefaultHomeLayoutSettings();
  }

  const raw = input as {
    version?: unknown;
    moduleOrder?: unknown;
    hiddenModules?: unknown;
    panelUrl?: unknown;
    proxyTestUrl?: unknown;
    edgeToEdge?: unknown;
    rounded?: unknown;
    bottomTabOrder?: unknown;
    bottomTabHidden?: unknown;
    tabProxiesEnabled?: unknown;
    tabConnectionsEnabled?: unknown;
    tabRulesEnabled?: unknown;
    rulesEnabled?: unknown;
  };
  if (raw.version !== 1) {
    return getDefaultHomeLayoutSettings();
  }

  const orderSet = new Set<HomeModuleId>();
  if (Array.isArray(raw.moduleOrder)) {
    for (const item of raw.moduleOrder) {
      if (typeof item === "string" && ALL_HOME_MODULES.includes(item as HomeModuleId)) {
        orderSet.add(item as HomeModuleId);
      }
    }
  }

  const moduleOrder = [...orderSet];

  if (!moduleOrder.includes("panel")) {
    const serviceIndex = moduleOrder.indexOf("service");
    const coreIndex = moduleOrder.indexOf("core");
    if (serviceIndex >= 0 && coreIndex > serviceIndex) {
      moduleOrder.splice(serviceIndex + 1, 0, "panel");
    } else {
      moduleOrder.push("panel");
    }
  }

  for (const moduleId of ALL_HOME_MODULES) {
    if (!moduleOrder.includes(moduleId)) {
      moduleOrder.push(moduleId);
    }
  }

  const hiddenSet = new Set<HomeModuleId>();
  if (Array.isArray(raw.hiddenModules)) {
    for (const item of raw.hiddenModules) {
      if (typeof item === "string" && ALL_HOME_MODULES.includes(item as HomeModuleId)) {
        hiddenSet.add(item as HomeModuleId);
      }
    }
  }

  const tabOrderSet = new Set<BottomTabId>();
  if (Array.isArray(raw.bottomTabOrder)) {
    for (const item of raw.bottomTabOrder) {
      if (typeof item === "string" && ALL_BOTTOM_TABS.includes(item as BottomTabId)) {
        tabOrderSet.add(item as BottomTabId);
      }
    }
  }

  const bottomTabOrder = tabOrderSet.size ? [...tabOrderSet] : [...ALL_BOTTOM_TABS];
  for (const tabId of ALL_BOTTOM_TABS) {
    if (!bottomTabOrder.includes(tabId)) {
      if (tabId === "logs" && bottomTabOrder.includes("settings")) {
        bottomTabOrder.splice(bottomTabOrder.indexOf("settings"), 0, tabId);
      } else {
        bottomTabOrder.push(tabId);
      }
    }
  }

  const bottomTabHiddenSet = new Set<BottomTabId>();
  if (Array.isArray(raw.bottomTabHidden)) {
    for (const item of raw.bottomTabHidden) {
      if (typeof item === "string" && ALL_BOTTOM_TABS.includes(item as BottomTabId)) {
        bottomTabHiddenSet.add(item as BottomTabId);
      }
    }
  } else {
    const proxiesEnabled = typeof raw.tabProxiesEnabled === "boolean" ? raw.tabProxiesEnabled : true;
    const connectionsEnabled = typeof raw.tabConnectionsEnabled === "boolean" ? raw.tabConnectionsEnabled : true;
    const rulesEnabled = typeof raw.tabRulesEnabled === "boolean" ? raw.tabRulesEnabled : typeof raw.rulesEnabled === "boolean" ? raw.rulesEnabled : false;

    if (!proxiesEnabled) bottomTabHiddenSet.add("proxies");
    if (!connectionsEnabled) bottomTabHiddenSet.add("connections");
    if (!rulesEnabled) bottomTabHiddenSet.add("rules");
    bottomTabHiddenSet.add("logs");
  }

  for (const tabId of ALL_BOTTOM_TABS) {
    if (isFixedBottomTab(tabId)) {
      bottomTabHiddenSet.delete(tabId);
    }
  }

  return {
    version: 1,
    moduleOrder,
    hiddenModules: [...hiddenSet],
    panelUrl: typeof raw.panelUrl === "string" ? raw.panelUrl.trim() : "",
    proxyTestUrl: typeof raw.proxyTestUrl === "string" ? raw.proxyTestUrl.trim() : "http://cp.cloudflare.com/generate_204",
    edgeToEdge: typeof raw.edgeToEdge === "boolean" ? raw.edgeToEdge : true,
    rounded: typeof raw.rounded === "boolean" ? raw.rounded : false,
    bottomTabOrder,
    bottomTabHidden: [...bottomTabHiddenSet],
  };
}

export function loadHomeLayoutSettings(): HomeLayoutSettings {
  if (typeof window === "undefined") {
    return getDefaultHomeLayoutSettings();
  }

  const raw = localStorage.getItem(STORAGE_KEY);
  if (!raw) {
    return getDefaultHomeLayoutSettings();
  }

  try {
    return normalizeHomeLayoutSettings(JSON.parse(raw));
  } catch {
    return getDefaultHomeLayoutSettings();
  }
}

export function saveHomeLayoutSettings(settings: HomeLayoutSettings): void {
  if (typeof window === "undefined") {
    return;
  }

  const normalized = normalizeHomeLayoutSettings(settings);
  localStorage.setItem(STORAGE_KEY, JSON.stringify(normalized));
  roundedStore.set(normalized.rounded);
  bottomTabOrderStore.set([...normalized.bottomTabOrder]);
  bottomTabHiddenStore.set([...normalized.bottomTabHidden]);
}

export const roundedStore = writable(false);

roundedStore.subscribe((value) => {
  if (typeof document !== "undefined") {
    if (value) {
      document.documentElement.setAttribute("data-rounded", "");
    } else {
      document.documentElement.removeAttribute("data-rounded");
    }
  }
});
export const bottomTabOrderStore = writable<BottomTabId[]>([...ALL_BOTTOM_TABS]);
export const bottomTabHiddenStore = writable<BottomTabId[]>(["logs"]);

export function initRoundedStore(): void {
  const settings = loadHomeLayoutSettings();
  roundedStore.set(settings.rounded);
  bottomTabOrderStore.set([...settings.bottomTabOrder]);
  bottomTabHiddenStore.set([...settings.bottomTabHidden]);
}
