<script lang="ts">
  import { House, Settings, Waypoints, Activity, ListChecks } from "@lucide/svelte";
  import { bottomTabHiddenStore, bottomTabOrderStore, type BottomTabId } from "$lib/settings";

  let { currentPath = $bindable() }: { currentPath: string } = $props();

  const bottomTabOrder = $derived($bottomTabOrderStore);
  const bottomTabHidden = $derived($bottomTabHiddenStore);

  const visibleTabs = $derived(
    (() => {
      const hiddenSet = new Set(bottomTabHidden);
      hiddenSet.delete("settings");
      return bottomTabOrder.filter((tabId) => tabId === "settings" || !hiddenSet.has(tabId));
    })(),
  );

  function normalizePath(hash: string): string {
    const base = (hash || "#/").split("?")[0];
    return base || "#/";
  }

  const TAB_META: Record<BottomTabId, { href: string; label: string; icon: any }> = {
    home: { href: "#/", label: "首页", icon: House },
    proxies: { href: "#/proxies", label: "代理", icon: Waypoints },
    connections: { href: "#/connections", label: "连接", icon: Activity },
    rules: { href: "#/rules", label: "规则", icon: ListChecks },
    settings: { href: "#/settings", label: "设置", icon: Settings },
  };

  function linkClass(href: string): string {
    return normalizePath(currentPath) === href ? "text-slate-900 dark:text-slate-100" : "text-slate-500 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200";
  }
</script>

<nav
  class="box-border w-full bg-white/78 dark:bg-zinc-950/72 backdrop-blur border-t border-slate-200/80 dark:border-zinc-800/80 z-40 shadow-[0_-6px_18px_rgba(15,23,42,0.08)] dark:shadow-[0_-8px_24px_rgba(0,0,0,0.35)] transition-colors
  rounded-t-xl"
  style="padding-bottom: var(--app-bottombar-safe, env(safe-area-inset-bottom, 0px)); height: var(--app-bottombar-height, calc(4rem + env(safe-area-inset-bottom, 0px)));"
>
  <div class="flex items-center justify-around h-16">
    {#each visibleTabs as tabId (tabId)}
      {@const meta = TAB_META[tabId]}
      <a href={meta.href} class={`flex flex-col items-center justify-center flex-1 h-full transition-colors outline-none ${linkClass(meta.href)}`} onclick={() => (currentPath = meta.href)}>
        <meta.icon size={20} strokeWidth={2} />
        <span class="text-xs font-bold mt-1">{meta.label}</span>
      </a>
    {/each}
  </div>
</nav>
