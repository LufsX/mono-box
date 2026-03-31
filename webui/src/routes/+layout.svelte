<script lang="ts">
  import { page } from "$app/state";
  import TopNav from "$lib/components/TopNav.svelte";
  import BottomNav from "$lib/components/BottomNav.svelte";
  import "./layout.css";

  let currentPath = $derived(page.url.pathname);

  let { children } = $props();
</script>

<div class="min-h-screen bg-slate-50 dark:bg-black transition-colors">
  <div style="padding-top: calc(3.5rem + env(safe-area-inset-top)); padding-bottom: calc(4rem + env(safe-area-inset-bottom));">
    {@render children()}
  </div>
  <BottomNav bind:currentPath />
</div>
