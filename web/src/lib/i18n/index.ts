import { useCallback } from "react";
import { useI18n, type Locale } from "./context";
import { en, zh, type Messages } from "./dictionary";

export { useI18n } from "./context";
export { I18nProvider } from "./context";
export type { Locale };
export const LOCALES: Locale[] = ["zh", "en"];

/**
 * Full name of each locale. The language capsule prints the short marks instead; these are its accessible
 * name and its tooltip, where the full name has room.
 */
export const LOCALE_LABELS: Record<Locale, string> = {
  zh: "中文",
  en: "English",
};

const MESSAGES: Record<Locale, Messages> = { zh, en };

/** Dictionary lookup outside React (used by non-component helpers). */
export function translate(locale: Locale, key: string): string {
  const table = MESSAGES[locale] as Record<string, string | undefined>;
  const fallback = MESSAGES.zh as Record<string, string | undefined>;
  return table[key] ?? fallback[key] ?? key;
}

/**
 * Resolves translated messages.
 *
 * `t("链接")` looks the Chinese source string up in the active locale's
 * dictionary. Unknown keys fall back to the Chinese source, so a missing
 * translation degrades to the original copy instead of showing a raw key.
 * Placeholders are positional: `t("{n} 条", { n: 5 })` -> "5 items".
 */
export function useT() {
  const { locale } = useI18n();
  return useCallback(
    (source: string, vars?: Record<string, string | number>): string => {
      let text = translate(locale, source);
      if (vars) {
        for (const [k, v] of Object.entries(vars)) {
          text = text.split(`{${k}}`).join(String(v));
        }
      }
      return text;
    },
    [locale]
  );
}
