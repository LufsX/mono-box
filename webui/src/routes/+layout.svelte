<script lang="ts">
  import { page } from "$app/state";
  import TopNav from "$lib/components/TopNav.svelte";
  import BottomNav from "$lib/components/BottomNav.svelte";
  import "./layout.css";

  let currentPath = $derived(page.url.pathname);

  async function handleTopNavRefresh() {
    if (typeof window === "undefined") return;
    window.dispatchEvent(new CustomEvent("mono-box:refresh-request"));
  }

  let { children } = $props();
</script>

<div class="app-shell bg-slate-50 dark:bg-black transition-colors">
  <div class="app-topbar">
    <TopNav showRefresh={currentPath === "/"} onRefresh={handleTopNavRefresh} />
  </div>
  <div class="app-scroll">
    {@render children()}
  </div>
  <div class="app-bottombar">
    <BottomNav bind:currentPath />
  </div>
</div>
