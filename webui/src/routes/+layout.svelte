<script lang="ts">
  import { onMount } from "svelte";
  import { cubicOut } from "svelte/easing";
  import { fade, fly } from "svelte/transition";
  import TopNav from "$lib/components/TopNav.svelte";
  import BottomNav from "$lib/components/BottomNav.svelte";
  import { loadHomeLayoutSettings, initRoundedStore } from "$lib/settings";
  import * as actionRealApi from "$lib/api/action";
  import * as actionMockApi from "$lib/api/action.mock";
  import Home from "$lib/pages/Home.svelte";
  import Proxies from "$lib/pages/Proxies.svelte";
  import Settings from "$lib/pages/Settings.svelte";
  import Connections from "$lib/pages/Connections.svelte";
  import Rules from "$lib/pages/Rules.svelte";
  import "./layout.css";

  const isProd = import.meta.env.MODE !== "production";
  const actionApi = isProd ? actionMockApi : actionRealApi;

  // Execute immediately with highest priority
  if (typeof window !== "undefined") {
    const settings = loadHomeLayoutSettings();
    actionApi.setEdgeToEdge(settings.edgeToEdge);
    initRoundedStore();
  }

  let { children } = $props();

  let currentHash = $state("#/");

  const currentPath = $derived((currentHash || "#/").split("?")[0] || "#/");

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
    <div class="app-page-wrapper" in:fly={{ y: 12, duration: 300, easing: cubicOut }} out:fade={{ duration: 180 }}>
      <div class="app-scroll">
        {#if currentPath === "#/settings"}
          <Settings />
        {:else if currentPath === "#/proxies"}
          <Proxies />
        {:else if currentPath === "#/connections"}
          <Connections />
        {:else if currentPath === "#/rules"}
          <Rules />
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
