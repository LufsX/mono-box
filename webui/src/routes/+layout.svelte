<script lang="ts">
  import { page } from "$app/state";
  import { cubicOut } from "svelte/easing";
  import { fade, fly } from "svelte/transition";
  import TopNav from "$lib/components/TopNav.svelte";
  import BottomNav from "$lib/components/BottomNav.svelte";
  import "./layout.css";

  let currentPath = $derived(page.url.pathname);

  let { children } = $props();
</script>

<div class="app-shell bg-slate-50 dark:bg-black transition-colors">
  <div class="app-topbar">
    <TopNav />
  </div>
  <div class="app-scroll">
    {#key currentPath}
      <div in:fly={{ y: 12, duration: 260, easing: cubicOut }} out:fade={{ duration: 160 }}>
        {@render children()}
      </div>
    {/key}
  </div>
  <div class="app-bottombar">
    <BottomNav bind:currentPath />
  </div>
</div>
