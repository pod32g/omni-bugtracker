import { useState } from "react";

// Light/dark theme. Initial value comes from the class the inline script in index.html
// stamped on <html> before paint; toggling flips the `.dark` class and persists it.
export type Theme = "light" | "dark";

const readTheme = (): Theme =>
  document.documentElement.classList.contains("dark") ? "dark" : "light";

export function useTheme() {
  const [theme, setTheme] = useState<Theme>(readTheme);

  const toggle = () => {
    const next: Theme = theme === "dark" ? "light" : "dark";
    document.documentElement.classList.toggle("dark", next === "dark");
    try {
      localStorage.setItem("obt_theme", next);
    } catch {
      /* ignore storage errors (private mode) */
    }
    setTheme(next);
  };

  return { theme, toggle };
}
