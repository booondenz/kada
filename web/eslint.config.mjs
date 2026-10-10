import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import { defineConfig, globalIgnores } from "eslint/config";
import globals from "globals";
import tseslint from "typescript-eslint";

// The rules are listed rather than inherited from a framework preset.
//
// The frontend used to be linted with eslint-config-next, which brought the React hooks rules along with a
// pile of Next-specific ones that no longer apply to a Vite single-page app. Naming the two hooks rules
// here keeps what this codebase actually depends on: a hook called conditionally is a bug, and an effect
// whose dependencies are wrong only shows up at runtime.
//
// The globals are declared because no-undef is only useful with them: without a browser environment every
// console, fetch and window in the app is an error, which buries the real ones.
export default defineConfig([
  globalIgnores(["**/node_modules/**", "dist/**", ".next/**", "coverage/**", ".theme-shots/**"]),
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx,mjs,js}"],
    languageOptions: {
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },
  },
  {
    files: ["**/*.{ts,tsx}"],
    plugins: { "react-hooks": reactHooks },
    rules: {
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "warn",
      // TypeScript already refuses an unknown identifier, and it knows about types this rule does not:
      // leaving it on for .ts/.tsx duplicates the compiler with worse information.
      "no-undef": "off",
    },
  },
  {
    // The scripts under scripts/ drive a headless browser over the DevTools protocol. They are tools, not
    // application code: an empty catch around a probe that is allowed to fail is deliberate, and a bare
    // expression statement is how they say the call itself is the effect.
    files: ["scripts/**/*.mjs"],
    rules: {
      "no-empty": "off",
      "@typescript-eslint/no-unused-expressions": "off",
    },
  },
]);