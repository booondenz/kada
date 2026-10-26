import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";

export type Locale = "zh" | "en";

const STORAGE_KEY = "kada.locale";

type I18nValue = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  toggleLocale: () => void;
};

const I18nContext = createContext<I18nValue | null>(null);

/** Reads the persisted language. Safe outside the browser (SSR returns "zh"). */
export function readStoredLocale(): Locale {
  if (typeof window === "undefined") return "zh";
  try {
    return window.localStorage.getItem(STORAGE_KEY) === "en" ? "en" : "zh";
  } catch {
    return "zh";
  }
}

/**
 * What the document says about itself in the chosen language.
 *
 * The title in `index.html` is the Chinese one, because that is what a crawler reads before any JavaScript
 * runs; this only keeps a visitor who chose English from sitting under a Chinese tab.
 */
const TITLES: Record<Locale, string> = {
  zh: "Kada 短链接 · 免费短网址生成与点击统计",
  en: "Kada · Free short links with click analytics",
};

function applyDocumentLang(locale: Locale) {
  if (typeof document === "undefined") return;
  document.documentElement.lang = locale === "en" ? "en" : "zh-CN";
  document.title = TITLES[locale];
}

/**
 * Holds the active UI language.
 *
 * The value is initialised straight from the persisted preference via a lazy
 * state initialiser, so the first render is already in the right language. The
 * document, though, is not state: `lang` and the tab title are synced from an
 * effect, which also covers the first load - otherwise a visitor who chose
 * English would get an English UI under a `lang="zh-CN"` document.
 */
export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(readStoredLocale);

  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next);
    try {
      window.localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // Storage can be unavailable (private mode); the in-memory value still applies.
    }
  }, []);

  const toggleLocale = useCallback(() => {
    setLocaleState((current) => {
      const next: Locale = current === "en" ? "zh" : "en";
      try {
        window.localStorage.setItem(STORAGE_KEY, next);
      } catch {
        // See above.
      }
      return next;
    });
  }, []);

  useEffect(() => {
    applyDocumentLang(locale);
  }, [locale]);

  const value = useMemo<I18nValue>(
    () => ({ locale, setLocale, toggleLocale }),
    [locale, setLocale, toggleLocale]
  );

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nValue {
  const ctx = useContext(I18nContext);
  if (!ctx) {
    throw new Error("useI18n must be used within an I18nProvider");
  }
  return ctx;
}
