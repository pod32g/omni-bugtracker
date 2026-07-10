import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Override with OBT_API_TARGET when the backend runs on a non-default port.
const apiTarget = process.env.OBT_API_TARGET ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // Proxy the API so the SPA and backend share an origin in dev.
    proxy: {
      "/api": { target: apiTarget, changeOrigin: true },
      "/auth": { target: apiTarget, changeOrigin: true },
    },
  },
});
