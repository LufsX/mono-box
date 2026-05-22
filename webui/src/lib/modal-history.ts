import { onMount, onDestroy } from "svelte";

export function useModalHistory(modalId: string, onPopState: () => void) {
  let pushed = false;

  function push() {
    if (pushed) return;
    if (typeof history === "undefined") return;
    history.pushState({ ...(history.state || {}), __modal: modalId }, "");
    pushed = true;
  }

  function handlePopState() {
    if (!pushed) return;
    onPopState();
    pushed = false;
  }

  function close() {
    if (pushed && typeof history !== "undefined") {
      history.back();
      return;
    }
    onPopState();
    pushed = false;
  }

  onMount(() => {
    window.addEventListener("popstate", handlePopState);
  });

  onDestroy(() => {
    window.removeEventListener("popstate", handlePopState);
  });

  return { push, close };
}
