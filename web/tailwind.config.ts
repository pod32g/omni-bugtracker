import type { Config } from "tailwindcss";

// Dark mode is the default (applied on <html> in index.html); the class strategy lets
// users opt into light later without a rebuild.
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        // Developer-first palette: calm slate surfaces + a violet accent.
        surface: {
          DEFAULT: "#0f1117",
          raised: "#171a22",
          border: "#242835",
        },
        accent: {
          DEFAULT: "#8b5cf6",
          hover: "#a78bfa",
        },
        severity: {
          critical: "#ef4444",
          high: "#f97316",
          medium: "#eab308",
          low: "#22c55e",
        },
      },
      fontFamily: {
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "monospace"],
      },
    },
  },
  plugins: [],
} satisfies Config;
