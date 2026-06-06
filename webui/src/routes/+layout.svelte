<script lang="ts">
  import { onMount } from "svelte";
  import { cubicOut } from "svelte/easing";
  import { fade, fly } from "svelte/transition";
  import TopNav from "$lib/components/layout/TopNav.svelte";
  import BottomNav from "$lib/components/layout/BottomNav.svelte";
  import { loadHomeLayoutSettings, initRoundedStore } from "$lib/settings";
  import { actionApi } from "$lib/api";
  import { normalizeHash } from "$lib/app-registry";
  import { resolvePageComponent } from "$lib/app-pages";
  import "./layout.css";

  // Execute immediately with highest priority
  if (typeof window !== "undefined") {
    const settings = loadHomeLayoutSettings();
    actionApi.setEdgeToEdge(settings.edgeToEdge);
    initRoundedStore();
  }

  let { children } = $props();

  let currentHash = $state("#/");

  const currentPath = $derived(normalizeHash(currentHash));
  const CurrentPage = $derived(resolvePageComponent(currentPath));

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
        <CurrentPage />
      </div>
    </div>
  {/key}
  <div class="app-bottombar">
    <BottomNav bind:currentPath={currentHash} />
  </div>
  {@render children?.()}
</div>
