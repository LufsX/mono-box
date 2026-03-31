<script lang="ts">
  import { Home, Settings } from "@lucide/svelte";
  
  let { currentPath = $bindable() }: { currentPath: string } = $props();
</script>

<nav class="fixed bottom-0 left-0 w-full bg-white dark:bg-zinc-950 border-t border-slate-300 dark:border-zinc-800 z-50 transition-colors" style="padding-bottom: env(safe-area-inset-bottom);">
  <div class="flex items-center justify-around h-16">
    <a
      href="/"
      class="flex flex-col items-center justify-center flex-1 h-full transition-colors outline-none {currentPath === '/'
        ? 'text-slate-900 dark:text-slate-100'
        : 'text-slate-500 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200'}"
      onclick={() => (currentPath = '/')}
    >
      <Home size={20} strokeWidth={2} />
      <span class="text-xs font-bold mt-1">首页</span>
    </a>
    
    <a
      href="/settings"
      class="flex flex-col items-center justify-center flex-1 h-full transition-colors outline-none {currentPath === '/settings'
        ? 'text-slate-900 dark:text-slate-100'
        : 'text-slate-500 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200'}"
      onclick={() => (currentPath = '/settings')}
    >
      <Settings size={20} strokeWidth={2} />
      <span class="text-xs font-bold mt-1">设置</span>
    </a>
  </div>
</nav>
