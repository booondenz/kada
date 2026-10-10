// The theme has to be applied before the first frame, which means before any bundle: a dark-theme visitor
// who gets a light first frame sees a flash that is impossible to unsee.
//
// So the script lives here as a string, build-config injects it into <head> (vite.config.ts), and the
// provider in ./context reads the attribute it set rather than recomputing anything. Keeping it in this
// module rather than inline in index.html is what lets web/scripts/check-theme-timing.mjs look for
// one implementation.
export const STORAGE_KEY = "kada.theme";
export const DARK_QUERY = "(prefers-color-scheme: dark)";

/**
 * The script that runs before the bundle, as source text.
 *
 * It is deliberately tiny and dependency-free: it runs while the HTML is still being parsed, so anything
 * it throws would leave the page unstyled. The inner try/catch covers localStorage being unavailable
 * (private mode, blocked cookies), where following the system is still the right answer.
 */
export function themeBootstrapScript(): string {
  return `(function(){try{var s=localStorage.getItem(${JSON.stringify(STORAGE_KEY)});var m=window.matchMedia(${JSON.stringify(DARK_QUERY)}).matches;document.documentElement.setAttribute("data-theme",(s==="dark"||s==="light")?s:(m?"dark":"light"))}catch(e){try{document.documentElement.setAttribute("data-theme",window.matchMedia(${JSON.stringify(DARK_QUERY)}).matches?"dark":"light")}catch(e2){}}})()`;
}
