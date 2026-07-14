<script lang="ts">
  import { fade } from "svelte/transition";

  type NoticeBannerTone = "success" | "error" | "notice";

  let {
    tone = "notice",
    message = "",
  }: {
    tone?: NoticeBannerTone;
    message: string;
  } = $props();

  const toneClass: Record<NoticeBannerTone, string> = {
    success: "border-emerald-300 bg-emerald-50/95 text-emerald-700 dark:border-emerald-900/50 dark:bg-emerald-950/90 dark:text-emerald-300",
    error: "border-red-300 bg-red-50/95 text-red-700 dark:border-red-900/50 dark:bg-red-950/90 dark:text-red-300",
    notice: "border-amber-300 bg-amber-50/95 text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/90 dark:text-amber-300",
  };
</script>

<div class="notice-banner-toast" in:fade={{ duration: 120 }} out:fade={{ duration: 100 }}>
  <div class={`flex min-h-9 items-center border px-3 py-2 text-xs shadow-lg rounded-lg ${toneClass[tone]}`}>
    <span class="block truncate">{message}</span>
  </div>
</div>

<style>
  .notice-banner-toast {
    position: fixed;
    left: max(1rem, var(--ksu-insets-left, 0px));
    right: max(1rem, var(--ksu-insets-right, 0px));
    bottom: calc(var(--app-bottombar-height, 4rem) + 1rem);
    z-index: 50;
    max-width: 48rem;
    margin-inline: auto;
    pointer-events: none;
  }
</style>
