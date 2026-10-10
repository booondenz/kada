import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { useHydrated } from "@/lib/useHydrated";
import { DARK_QUERY, STORAGE_KEY } from "./bootstrap";

/**
 * Theme handling.
 *
 * `data-theme` on <html> is the single source of truth, so the theme an inline script applied before the
 * first paint and the theme React knows about cannot disagree. This provider reads that attribute rather
 * than keeping a second copy of the answer, and only decides *what* it should be.
 *
 * Resolution order:
 *   1. an explicit choice, stored in localStorage;
 *   2. otherwise the browser's `prefers-color-scheme`, re-read when it changes.
 */

export type Theme = "light" | "dark";


type ThemeValue = {
  /** The theme currently applied. */
  theme: Theme;
  /** True while following the OS setting, false once the user has chosen explicitly. */
  followsSystem: boolean;
  setTheme: (theme: Theme) => void;
  /** Drops the stored choice and goes back to following the OS. */
  useSystemTheme: () => void;
};

const ThemeContext = createContext<ThemeValue | null>(null);

function systemTheme(): Theme {
  return window.matchMedia(DARK_QUERY).matches ? "dark" : "light";
}

/** The stored choice, or null when the user has never chosen. */
function readStoredTheme(): Theme | null {
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    return stored === "dark" || stored === "light" ? stored : null;
  } catch {
    return null;
  }
}

/**
 * The theme actually in the DOM.
 *
 * The script in index.html has already set it, so reading the attribute back keeps this in step with the painted page -
 * including the "following the system" case, which this component then never has to compute at all.
 */
function appliedTheme(): Theme {
  return document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light";
}

function applyTheme(theme: Theme) {
  document.documentElement.setAttribute("data-theme", theme);
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const hydrated = useHydrated();
  const [, forceRender] = useState(0);

  const theme: Theme = hydrated ? appliedTheme() : "light";
  const followsSystem = hydrated && readStoredTheme() === null;

  // Follow the OS while there is no explicit choice, so the app switches with the system (or with
  // devtools' emulation) instead of only on load.
  useEffect(() => {
    if (!followsSystem) return;

    const media = window.matchMedia(DARK_QUERY);
    const onChange = () => {
      applyTheme(media.matches ? "dark" : "light");
      forceRender((n) => n + 1);
    };

    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, [followsSystem]);

  const setTheme = useCallback((next: Theme) => {
    applyTheme(next);
    try {
      window.localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // Storage can be unavailable (private mode); the applied attribute still holds for this session.
    }
    forceRender((n) => n + 1);
  }, []);

  const useSystemTheme = useCallback(() => {
    try {
      window.localStorage.removeItem(STORAGE_KEY);
    } catch {
      // See above.
    }
    applyTheme(systemTheme());
    forceRender((n) => n + 1);
  }, []);

  const value = useMemo<ThemeValue>(
    () => ({ theme, followsSystem, setTheme, useSystemTheme }),
    [theme, followsSystem, setTheme, useSystemTheme]
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) {
    throw new Error("useTheme must be used within a ThemeProvider");
  }
  return ctx;
}
