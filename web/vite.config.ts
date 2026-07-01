import { writeFileSync } from "node:fs";
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";

// keepDist re-creates dist/.gitkeep after every build. The Go binary embeds
// web/dist via go:embed, which needs at least one file present even on a fresh
// checkout that has not run the frontend build; .gitkeep is that file, and
// vite's emptyOutDir would otherwise delete it on each build.
function keepDist(): Plugin {
  return {
    name: "aeman-keep-dist",
    closeBundle() {
      // vite runs the build from the web/ directory, so this is web/dist.
      writeFileSync("dist/.gitkeep", "");
    },
  };
}

// During `vite dev`, /api/* is proxied to the aeman Go binary so the GitHub
// proxy and /api/config work the same as in the embedded production build.
export default defineConfig({
  plugins: [react(), keepDist()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://127.0.0.1:8765", ws: true },
    },
  },
});
