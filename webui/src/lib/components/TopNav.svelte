<script lang="ts">
  import { onMount } from "svelte";
  import ThemeSelector from "./ThemeSelector.svelte";

  let {
    title = "Mono Box",
  }: {
    title?: string;
  } = $props();

  let theme = $state<"system" | "light" | "dark">("system");
  let themeDropdownOpen = $state(false);

  function applyTheme(t: "system" | "light" | "dark") {
    const isDark = t === "dark" || (t === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    if (isDark) {
      document.documentElement.classList.add("dark");
    } else {
      document.documentElement.classList.remove("dark");
    }
  }

  onMount(() => {
    const savedTheme = localStorage.getItem("theme") as "system" | "light" | "dark";
    if (savedTheme && ["system", "light", "dark"].includes(savedTheme)) {
      theme = savedTheme;
    }
    applyTheme(theme);

    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    const handleThemeChange = () => {
      if (theme === "system") applyTheme("system");
    };

    mediaQuery.addEventListener("change", handleThemeChange);

    return () => {
      mediaQuery.removeEventListener("change", handleThemeChange);
    };
  });

</script>

<nav
  class="box-border w-full bg-white/44 dark:bg-zinc-950/42 backdrop-blur border-b border-slate-200/55 dark:border-zinc-800/55 flex items-center justify-between px-4 z-40 shadow-[0_8px_26px_rgba(15,23,42,0.08)] dark:shadow-[0_10px_30px_rgba(0,0,0,0.32)] transition-colors overflow-hidden relative"
  style="padding-top: var(--app-topbar-safe, env(safe-area-inset-top, 0px)); height: var(--app-topbar-height, calc(3.5rem + env(safe-area-inset-top, 0px)));"
>
  <div class="pointer-events-none absolute inset-0 bg-linear-to-b from-white/14 via-white/8 to-transparent dark:from-white/5 dark:via-white/2"></div>
  <div class="font-bold text-lg text-slate-900 dark:text-slate-100 uppercase tracking-widest shrink-0 relative z-10">{title}</div>
  <div class="relative flex items-center shrink-0 min-w-0 z-10">
    <ThemeSelector bind:theme bind:expanded={themeDropdownOpen} />
  </div>
</nav>
