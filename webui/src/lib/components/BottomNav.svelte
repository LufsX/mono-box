<script lang="ts">
  import { House, Settings, Waypoints, Activity } from "@lucide/svelte";
  import { roundedStore } from "$lib/settings";

  let { currentPath = $bindable() }: { currentPath: string } = $props();

  const r = $derived($roundedStore);

  const navItems = [
    { href: "#/", label: "首页", icon: House },
    { href: "#/proxies", label: "代理", icon: Waypoints },
    { href: "#/connections", label: "连接", icon: Activity },
    { href: "#/settings", label: "设置", icon: Settings },
  ] as const;

  function linkClass(href: string): string {
    return currentPath === href ? "text-slate-900 dark:text-slate-100" : "text-slate-500 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200";
  }
</script>

<nav
  class="box-border w-full bg-white/78 dark:bg-zinc-950/72 backdrop-blur border-t border-slate-200/80 dark:border-zinc-800/80 z-40 shadow-[0_-6px_18px_rgba(15,23,42,0.08)] dark:shadow-[0_-8px_24px_rgba(0,0,0,0.35)] transition-colors
  {r ? 'rounded-t-xl' : ''}"
  style="padding-bottom: var(--app-bottombar-safe, env(safe-area-inset-bottom, 0px)); height: var(--app-bottombar-height, calc(4rem + env(safe-area-inset-bottom, 0px)));"
>
  <div class="flex items-center justify-around h-16">
    {#each navItems as item}
      <a href={item.href} class={`flex flex-col items-center justify-center flex-1 h-full transition-colors outline-none ${linkClass(item.href)}`} onclick={() => (currentPath = item.href)}>
        <item.icon size={20} strokeWidth={2} />
        <span class="text-xs font-bold mt-1">{item.label}</span>
      </a>
    {/each}
  </div>
</nav>
