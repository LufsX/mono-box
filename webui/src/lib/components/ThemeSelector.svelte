<script lang="ts">
  import { Sun, Moon, Monitor } from "@lucide/svelte";
  import { roundedStore } from "$lib/settings";

  let { theme = $bindable(), expanded = $bindable() }: { theme: "system" | "light" | "dark"; expanded: boolean } = $props();

  const r = $derived($roundedStore);

  function applyTheme(t: "system" | "light" | "dark") {
    const isDark = t === "dark" || (t === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    if (isDark) {
      document.documentElement.classList.add("dark");
    } else {
      document.documentElement.classList.remove("dark");
    }
  }

  function setTheme(t: "system" | "light" | "dark") {
    theme = t;
    localStorage.setItem("theme", t);
    applyTheme(t);
    expanded = false;
  }
</script>

<div class="relative flex items-center gap-0 shrink-0">
  <div class="flex items-center overflow-hidden transition-all duration-300 ease-in-out pr-2" style="max-width: {expanded ? '200px' : '0'}; opacity: {expanded ? '1' : '0'}">
    <div class="flex text-sm font-bold whitespace-nowrap">
      <button
        class="px-3 py-1.5 transition-all duration-300 outline-none border -ml-px first:ml-0 {r ? 'rounded-l-lg' : ''} {theme === 'light'
          ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
          : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
        onclick={() => setTheme("light")}
      >
        <Sun size={18} strokeWidth={2} />
      </button>
      <button
        class="px-3 py-1.5 transition-all duration-300 outline-none border -ml-px {theme === 'system'
          ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
          : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
        onclick={() => setTheme("system")}
      >
        <Monitor size={18} strokeWidth={2} />
      </button>
      <button
        class="px-3 py-1.5 transition-all duration-300 outline-none border -ml-px {r ? 'rounded-r-lg' : ''} {theme === 'dark'
          ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 z-10 shadow-sm'
          : 'border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950 text-slate-500 hover:bg-slate-100 dark:hover:bg-zinc-800 z-0'}"
        onclick={() => setTheme("dark")}
      >
        <Moon size={18} strokeWidth={2} />
      </button>
    </div>
  </div>

  <button
    class="px-3 py-1.5 border transition-all duration-300 outline-none {r ? 'rounded-lg' : ''} {expanded
      ? 'border-slate-800 dark:border-slate-300 bg-slate-800 dark:bg-slate-200 text-white dark:text-zinc-900 shadow-sm'
      : 'border-slate-300 dark:border-zinc-700 text-slate-800 dark:text-slate-200 bg-white dark:bg-zinc-950 hover:bg-slate-100 dark:hover:bg-zinc-800'} shrink-0"
    onclick={() => (expanded = !expanded)}
  >
    {#if theme === "light"}
      <Sun size={18} strokeWidth={2} />
    {:else if theme === "dark"}
      <Moon size={18} strokeWidth={2} />
    {:else}
      <Monitor size={18} strokeWidth={2} />
    {/if}
  </button>
</div>
