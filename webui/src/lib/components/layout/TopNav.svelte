<script lang="ts">
  import { onMount } from "svelte";
  import ThemeSelector from "$lib/components/common/ThemeSelector.svelte";
  import { themeStore, type Theme } from "$lib/theme";

  let {
    title = "Mono Box",
  }: {
    title?: string;
  } = $props();

  let theme = $state<Theme>("system");
  let themeDropdownOpen = $state(false);

  onMount(() => {
    themeStore.init();
    const unsub = themeStore.subscribe((t) => {
      theme = t;
    });
    return unsub;
  });

  function setTheme(t: Theme) {
    themeStore.setTheme(t);
  }
</script>

<nav
  class="box-border w-full bg-white/44 dark:bg-zinc-950/42 backdrop-blur border-b border-slate-200/55 dark:border-zinc-800/55 flex items-center justify-between px-4 z-40 shadow-[0_8px_26px_rgba(15,23,42,0.08)] dark:shadow-[0_10px_30px_rgba(0,0,0,0.32)] transition-colors overflow-hidden relative
  rounded-b-xl"
  style="padding-top: var(--app-topbar-safe, env(safe-area-inset-top, 0px)); height: var(--app-topbar-height, calc(3.5rem + env(safe-area-inset-top, 0px)));"
>
  <div class="pointer-events-none absolute inset-0 bg-linear-to-b from-white/14 via-white/8 to-transparent dark:from-white/5 dark:via-white/2"></div>
  <div class="font-bold text-lg text-slate-900 dark:text-slate-100 uppercase tracking-widest shrink-0 relative z-10">{title}</div>
  <div class="relative flex items-center shrink-0 min-w-0 z-10">
    <ThemeSelector bind:theme bind:expanded={themeDropdownOpen} {setTheme} />
  </div>
</nav>
