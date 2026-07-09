import type { Config } from "tailwindcss";

// Colors are driven by CSS variables (channel triplets) defined in index.css, so the
// whole palette flips between light and dark under the `.dark` class while Tailwind's
// opacity utilities (bg-paper/80, shadow-ink/10) keep working. See index.css for values.
const withAlpha = (v: string) => `rgb(var(${v}) / <alpha-value>)`;

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        paper: withAlpha("--color-paper"),
        panel: withAlpha("--color-panel"),
        mist: withAlpha("--color-mist"),
        hairline: withAlpha("--color-hairline"),
        ink: withAlpha("--color-ink"),
        graphite: { DEFAULT: withAlpha("--color-graphite"), soft: withAlpha("--color-graphite-soft") },
        blueprint: {
          DEFAULT: withAlpha("--color-blueprint"),
          soft: withAlpha("--color-blueprint-soft"),
          border: withAlpha("--color-blueprint-border"),
        },
        critical: {
          DEFAULT: withAlpha("--color-critical"),
          soft: withAlpha("--color-critical-soft"),
          border: withAlpha("--color-critical-border"),
        },
        high: {
          DEFAULT: withAlpha("--color-high"),
          soft: withAlpha("--color-high-soft"),
          border: withAlpha("--color-high-border"),
        },
        resolved: {
          DEFAULT: withAlpha("--color-resolved"),
          soft: withAlpha("--color-resolved-soft"),
          border: withAlpha("--color-resolved-border"),
        },
        medium: withAlpha("--color-medium"),
        terminal: { DEFAULT: withAlpha("--color-terminal"), ink: withAlpha("--color-terminal-ink") },
        chip: { DEFAULT: withAlpha("--color-chip"), empty: withAlpha("--color-chip-empty") },
      },
      fontFamily: {
        sans: ['"Space Grotesk"', "system-ui", "sans-serif"],
        mono: ['"JetBrains Mono"', "ui-monospace", "SFMono-Regular", "Menlo", "monospace"],
      },
      borderRadius: {
        sm: "6px",
        md: "10px",
        lg: "14px",
      },
      letterSpacing: {
        caps: "0.12em",
      },
    },
  },
  plugins: [],
} satisfies Config;
