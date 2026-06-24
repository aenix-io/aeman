import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// During `vite dev`, /api/* is proxied to the aeman Go binary so the GitHub
// proxy and /api/config work the same as in the embedded production build.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      "/api": "http://127.0.0.1:8765",
    },
  },
});
