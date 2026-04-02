<script lang="ts">
  import { onMount } from "svelte";
  import { cubicOut } from "svelte/easing";
  import { fade, fly } from "svelte/transition";
  import { dev } from "$app/environment";
  import TopNav from "$lib/components/TopNav.svelte";
  import BottomNav from "$lib/components/BottomNav.svelte";
  import { loadHomeLayoutSettings, initRoundedStore } from "$lib/settings";
  import * as actionRealApi from "$lib/api/action";
  import * as actionMockApi from "$lib/api/action.mock";
  import Home from "$lib/pages/Home.svelte";
  import Settings from "$lib/pages/Settings.svelte";
  import "./layout.css";

  const actionApi = dev ? actionMockApi : actionRealApi;

  // Execute immediately with highest priority
  if (typeof window !== "undefined") {
    const settings = loadHomeLayoutSettings();
    actionApi.setEdgeToEdge(settings.edgeToEdge);
    initRoundedStore();
  }

  let { children } = $props();

  let currentHash = $state("#/");

  onMount(() => {
    const handleHashChange = () => {
      const hash = window.location.hash || "#/";
      currentHash = hash;
    };

    window.addEventListener("hashchange", handleHashChange);
    handleHashChange();

    return () => {
      window.removeEventListener("hashchange", handleHashChange);
    };
  });
</script>

<div class="app-shell bg-slate-50 dark:bg-black transition-colors">
  <div class="app-topbar">
    <TopNav />
  </div>
  {#key currentHash}
    <div class="app-page-wrapper" in:fly={{ y: 12, duration: 260, delay: 160, easing: cubicOut }} out:fade={{ duration: 160 }}>
      <div class="app-scroll">
        {#if currentHash === "#/settings"}
          <Settings />
        {:else}
          <Home />
        {/if}
      </div>
    </div>
  {/key}
  <div class="app-bottombar">
    <BottomNav bind:currentPath={currentHash} />
  </div>
  {@render children?.()}
</div>
