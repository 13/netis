import { defineStore } from "pinia";

const STORAGE_KEY = "netis_theme";

export type Theme = "dark" | "light";

export const useThemeStore = defineStore("theme", {
  state: () => ({
    theme: "dark" as Theme,
  }),
  actions: {
    init() {
      const saved = localStorage.getItem(STORAGE_KEY) as Theme | null;
      this.theme = saved ?? "dark";
      this.apply();
    },
    set(theme: Theme) {
      this.theme = theme;
      localStorage.setItem(STORAGE_KEY, theme);
      this.apply();
    },
    toggle() {
      this.set(this.theme === "dark" ? "light" : "dark");
    },
    apply() {
      const html = document.documentElement;
      if (this.theme === "dark") html.classList.add("dark");
      else html.classList.remove("dark");
    },
  },
});
