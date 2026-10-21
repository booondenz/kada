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
    // Saving a file is atomic: the writer creates a dot-prefixed `<name>.<pid>.<uuid>.tmpdir` directory next
    // to the target, writes into it, and renames the file out. For those few milliseconds the temp file is
    // open, and on Windows an open file cannot be watched - the watcher descends into the new directory
    // under src/, `fs.watch` throws EBUSY, and a watcher error is fatal, so the dev server exits in the
    // middle of an edit. Ignoring the pattern stops the two from meeting. Vite appends this to its own
    // ignores rather than replacing them, so node_modules and .git stay unwatched.
    watch: {
      ignored: [/\.(tmpdir|tmp)([\\/]|$)/],
    },
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
