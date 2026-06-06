import { Activity, House, ListChecks, Settings, Waypoints } from "@lucide/svelte";

export type HomeModuleId = "tun" | "proxy" | "stats" | "service" | "panel" | "core" | "log";

export type BottomTabId = "home" | "proxies" | "connections" | "rules" | "settings";

export type BottomTabMeta = {
  href: string;
  label: string;
  icon: any;
  fixed?: boolean;
};

export type HomeModuleMeta = {
  label: string;
  requiresCore?: boolean;
};

export const BOTTOM_TAB_META: Record<BottomTabId, BottomTabMeta> = {
  home: { href: "#/", label: "首页", icon: House },
  proxies: { href: "#/proxies", label: "代理", icon: Waypoints },
  connections: { href: "#/connections", label: "连接", icon: Activity },
  rules: { href: "#/rules", label: "规则", icon: ListChecks },
  settings: { href: "#/settings", label: "设置", icon: Settings, fixed: true },
};

export const ALL_BOTTOM_TABS = Object.keys(BOTTOM_TAB_META) as BottomTabId[];

export const HOME_MODULE_META: Record<HomeModuleId, HomeModuleMeta> = {
  tun: { label: "网络接管与内核", requiresCore: true },
  proxy: { label: "代理模式选择", requiresCore: true },
  stats: { label: "实时内核信息", requiresCore: true },
  service: { label: "快速控制指令" },
  panel: { label: "面板快捷跳转" },
  core: { label: "核心状态与端口", requiresCore: true },
  log: { label: "Terminal Logs" },
};

export const ALL_HOME_MODULES = Object.keys(HOME_MODULE_META) as HomeModuleId[];

export function normalizeHash(hash: string): string {
  const base = (hash || "#/").split("?")[0];
  return base || "#/";
}

export function isFixedBottomTab(tabId: BottomTabId): boolean {
  return Boolean(BOTTOM_TAB_META[tabId]?.fixed);
}

export function isCoreBackedHomeModule(moduleId: HomeModuleId): boolean {
  return Boolean(HOME_MODULE_META[moduleId]?.requiresCore);
}
