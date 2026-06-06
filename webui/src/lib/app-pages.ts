import type { Component } from "svelte";
import { BOTTOM_TAB_META, normalizeHash } from "./app-registry";
import Home from "$lib/pages/Home.svelte";
import Proxies from "$lib/pages/Proxies.svelte";
import Settings from "$lib/pages/Settings.svelte";
import Connections from "$lib/pages/Connections.svelte";
import Rules from "$lib/pages/Rules.svelte";
import Logs from "$lib/pages/Logs.svelte";

const PAGE_COMPONENTS: Record<string, Component> = {
  [BOTTOM_TAB_META.home.href]: Home,
  [BOTTOM_TAB_META.proxies.href]: Proxies,
  [BOTTOM_TAB_META.connections.href]: Connections,
  [BOTTOM_TAB_META.rules.href]: Rules,
  [BOTTOM_TAB_META.logs.href]: Logs,
  [BOTTOM_TAB_META.settings.href]: Settings,
};

export function resolvePageComponent(hash: string): Component {
  return PAGE_COMPONENTS[normalizeHash(hash)] || Home;
}
