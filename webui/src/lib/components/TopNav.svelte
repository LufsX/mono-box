<script lang="ts">
  import { RefreshCw } from "@lucide/svelte";
  import ThemeSelector from "./ThemeSelector.svelte";

  let { 
    title = "Mono Box",
    onRefresh,
    showRefresh = true
  }: { 
    title?: string; 
    onRefresh?: () => void | Promise<void>;
    showRefresh?: boolean;
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

  $effect(() => {
    const savedTheme = localStorage.getItem("theme") as "system" | "light" | "dark";
    if (savedTheme && ["system", "light", "dark"].includes(savedTheme)) {
      theme = savedTheme;
    }
    applyTheme(theme);

    window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
      if (theme === "system") applyTheme("system");
    });
  });

  async function handleRefresh(e: MouseEvent) {
    if (!onRefresh) return;
    
    const btn = e.currentTarget as HTMLButtonElement;
    btn.classList.add("animate-spin");
    
    try {
      await onRefresh();
    } finally {
      setTimeout(() => btn.classList.remove("animate-spin"), 500);
    }
  }
</script>

<nav
  class="fixed top-0 left-0 w-full bg-white dark:bg-zinc-950 border-b border-slate-300 dark:border-zinc-800 flex items-center justify-between px-4 z-50 shadow-sm transition-colors overflow-hidden"
  style="padding-top: env(safe-area-inset-top); height: calc(3.5rem + env(safe-area-inset-top));"
>
  <div class="font-bold text-lg text-slate-900 dark:text-slate-100 uppercase tracking-widest shrink-0">{title}</div>
  <div class="relative flex items-center shrink-0 min-w-0">
    {#if showRefresh && !themeDropdownOpen}
      <button
        class="p-1.5 transition-all outline-none text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200 shrink-0"
        onclick={handleRefresh}
      >
        <RefreshCw size={18} strokeWidth={2} />
      </button>
    {/if}

    <ThemeSelector bind:theme bind:expanded={themeDropdownOpen} />
  </div>
</nav>
