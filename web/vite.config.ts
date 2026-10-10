import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

import { themeBootstrapScript } from "./src/lib/theme/bootstrap";

/**
 * Injects the theme script into <head>, from the one implementation in src/lib/theme/bootstrap.ts.
 *
 * It has to be inline and it has to run before the bundle; a React component that renders a <script> runs
 * after the first frame, which is exactly the flash the script exists to prevent.
 */
const themeBootstrap = {
  name: "kada-theme-bootstrap",
  transformIndexHtml(html: string) {
    return html.replace("<!--theme-->", `<script>${themeBootstrapScript()}</script>`);
  },
};

export default defineConfig({
  plugins: [react(), tailwindcss(), themeBootstrap],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    port: 3000,
    // Development serves /api from the same origin, the way nginx does in production, so the frontend never
    // deals with a second origin. This replaces the rewrites next.config.ts used to declare.
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "dist",
    // The bundle is served by nginx from a directory, so source maps ship next to it; they are small and
    // the alternative is debugging minified code in production.
    sourcemap: true,
  },
});
