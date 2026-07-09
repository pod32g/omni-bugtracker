import type { Config } from "tailwindcss";

// Design tokens mirror the Paper design system ("blueprint" developer tool, light mode).
// darkMode:"class" is retained so a dark variant can be layered on later without a rebuild.
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        paper: "#FFFFFF", // primary surface (main content, cards)
        panel: "#F5F7FA", // recessed surface (sidebar, column header, tracks)
        mist: "#FAFBFC", // faint surface (issue meta rail)
        hairline: "#E4E8EF", // borders / dividers
        ink: "#131923", // primary text
        graphite: { DEFAULT: "#5C6879", soft: "#8A94A6" }, // secondary / tertiary text
        blueprint: { DEFAULT: "#2A4CDB", soft: "#EAEEFC", border: "#CDD8F7" }, // brand accent
        critical: { DEFAULT: "#DA3633", soft: "#FBEBEA", border: "#F3D2D1" },
        high: { DEFAULT: "#C2620E", soft: "#FBF4E9", border: "#F0E2C8" },
        resolved: { DEFAULT: "#1F8A54", soft: "#E7F3EC", border: "#CFE8D9" },
        medium: "#5B6B8C", // medium-severity marker (slate)
        terminal: { DEFAULT: "#0F1420", ink: "#C7D2E4" }, // code / environment blocks
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
        caps: "0.12em", // uppercase mono micro-labels
      },
    },
  },
  plugins: [],
} satisfies Config;
