<script lang="ts">
  import { Sun, Moon, Monitor } from "@lucide/svelte";

  let { theme = $bindable(), expanded = $bindable() }: { theme: "system" | "light" | "dark"; expanded: boolean } = $props();

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
        class="px-3 py-1.5 transition-all duration-300 outline-none border -ml-px first:ml-0 {theme === 'light'
          ? 'border-slate-800/75 dark:border-slate-400/75 bg-slate-800/82 dark:bg-slate-200/80 backdrop-blur-md text-white dark:text-zinc-900 z-10 shadow-[0_6px_14px_rgba(15,23,42,0.16)]'
          : 'border-slate-300/80 dark:border-zinc-700/75 bg-white/65 dark:bg-zinc-900/62 backdrop-blur-md text-slate-500 hover:bg-slate-50/82 dark:hover:bg-zinc-800/72 z-0'}"
        onclick={() => setTheme("light")}
      >
        <Sun size={18} strokeWidth={2} />
      </button>
      <button
        class="px-3 py-1.5 transition-all duration-300 outline-none border -ml-px {theme === 'system'
          ? 'border-slate-800/75 dark:border-slate-400/75 bg-slate-800/82 dark:bg-slate-200/80 backdrop-blur-md text-white dark:text-zinc-900 z-10 shadow-[0_6px_14px_rgba(15,23,42,0.16)]'
          : 'border-slate-300/80 dark:border-zinc-700/75 bg-white/65 dark:bg-zinc-900/62 backdrop-blur-md text-slate-500 hover:bg-slate-50/82 dark:hover:bg-zinc-800/72 z-0'}"
        onclick={() => setTheme("system")}
      >
        <Monitor size={18} strokeWidth={2} />
      </button>
      <button
        class="px-3 py-1.5 transition-all duration-300 outline-none border -ml-px {theme === 'dark'
          ? 'border-slate-800/75 dark:border-slate-400/75 bg-slate-800/82 dark:bg-slate-200/80 backdrop-blur-md text-white dark:text-zinc-900 z-10 shadow-[0_6px_14px_rgba(15,23,42,0.16)]'
          : 'border-slate-300/80 dark:border-zinc-700/75 bg-white/65 dark:bg-zinc-900/62 backdrop-blur-md text-slate-500 hover:bg-slate-50/82 dark:hover:bg-zinc-800/72 z-0'}"
        onclick={() => setTheme("dark")}
      >
        <Moon size={18} strokeWidth={2} />
      </button>
    </div>
  </div>

  <button
    class="px-3 py-1.5 border transition-all duration-300 outline-none {expanded
      ? 'border-slate-800/75 dark:border-slate-400/75 bg-slate-800/82 dark:bg-slate-200/80 backdrop-blur-md text-white dark:text-zinc-900 shadow-[0_6px_14px_rgba(15,23,42,0.16)]'
      : 'border-slate-300/80 dark:border-zinc-700/75 text-slate-800 dark:text-slate-200 bg-white/68 dark:bg-zinc-900/62 backdrop-blur-md hover:bg-slate-50/82 dark:hover:bg-zinc-800/75'} shrink-0"
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
