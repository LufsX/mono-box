import { writable } from "svelte/store";

export type Theme = "system" | "light" | "dark";

function createThemeStore() {
  const { subscribe, set, update } = writable<Theme>("system");

  function applyTheme(t: Theme) {
    const isDark = t === "dark" || (t === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    if (isDark) {
      document.documentElement.classList.add("dark");
    } else {
      document.documentElement.classList.remove("dark");
    }
  }

  function init() {
    let saved: Theme = "system";
    try {
      const raw = localStorage.getItem("theme");
      if (raw === "light" || raw === "dark" || raw === "system") {
        saved = raw;
      }
    } catch {
      // Ignore storage failures in restricted WebViews.
    }

    set(saved);
    applyTheme(saved);

    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    mediaQuery.addEventListener("change", () => {
      update((current) => {
        if (current === "system") applyTheme("system");
        return current;
      });
    });
  }

  function setTheme(t: Theme) {
    set(t);
    applyTheme(t);
    try {
      localStorage.setItem("theme", t);
    } catch {
      // Ignore storage failures in restricted WebViews.
    }
  }

  return { subscribe, init, setTheme, applyTheme };
}

export const themeStore = createThemeStore();
