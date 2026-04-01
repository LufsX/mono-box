export type HomeModuleId = "tun" | "proxy" | "stats" | "service" | "panel" | "core" | "log";

export interface HomeLayoutSettings {
  version: 1;
  moduleOrder: HomeModuleId[];
  hiddenModules: HomeModuleId[];
  panelUrl: string;
  edgeToEdge: boolean;
}

const STORAGE_KEY = "mono-box.home-layout";

export const ALL_HOME_MODULES: HomeModuleId[] = ["tun", "proxy", "stats", "service", "panel", "core", "log"];

const DEFAULT_SETTINGS: HomeLayoutSettings = {
  version: 1,
  moduleOrder: ["tun", "proxy", "stats", "core", "panel", "service", "log"],
  hiddenModules: [],
  panelUrl: "",
  edgeToEdge: true,
};

export function getDefaultHomeLayoutSettings(): HomeLayoutSettings {
  return structuredClone(DEFAULT_SETTINGS);
}

export function normalizeHomeLayoutSettings(input: unknown): HomeLayoutSettings {
  if (!input || typeof input !== "object") {
    return getDefaultHomeLayoutSettings();
  }

  const raw = input as { version?: unknown; moduleOrder?: unknown; hiddenModules?: unknown; panelUrl?: unknown; edgeToEdge?: unknown };
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

  return {
    version: 1,
    moduleOrder,
    hiddenModules: [...hiddenSet],
    panelUrl: typeof raw.panelUrl === "string" ? raw.panelUrl.trim() : "",
    edgeToEdge: typeof raw.edgeToEdge === "boolean" ? raw.edgeToEdge : true,
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

  localStorage.setItem(STORAGE_KEY, JSON.stringify(normalizeHomeLayoutSettings(settings)));
}
