import { useEffect } from "react";
import { LOCALE_LABELS, LOCALES, useI18n, type Locale } from "@/lib/i18n";

/**
 * The marks the capsule prints: "中" and "EN".
 *
 * Short marks rather than the locale's own name ("中文" / "English"): a name is several glyphs wide, and this
 * control lives in a top bar next to a 32px icon button, so it has to stay a chip. Both columns are equal
 * whatever the two labels measure, so the thumb's half-track travel holds either way.
 *
 * "中" rather than "ZH", which is the point of the pair: it is the character its reader is scanning for, and
 * it is what Chinese input methods already print in the space of a letter - the Windows and macOS IME
 * indicator is 中 against 英. The other slot stays a Latin code because "EN" is what its reader is scanning
 * for too - the two are not inconsistent, they are each written in the script that side reads fastest.
 *
 * Neither mark follows the active locale. The capsule shows both options at once, so a translated label
 * would only ever rename one option in front of the reader who is not using it. The full names are still
 * what a screen reader announces and what hovering shows, and those do not have to fit.
 */
const SEGMENT_LABELS: Record<Locale, string> = { zh: "中", en: "EN" };

/**
 * Language switcher: a capsule with one segment per locale and a thumb that slides to the active one.
 *
 * It shows the language you are *in*, not the one you would switch to. A single button has to name its
 * target to read as an action; a segmented control shows both options at once, and there the highlighted
 * segment is the state - which is what everyone already expects it to mean.
 *
 * No flags. A flag names a country, not a language: English is not owned by the United Kingdom, and the
 * marks this replaces had to be hand-drawn SVG to survive Windows, where emoji flags degrade to letters.
 * "中"/"EN" says the same thing in less space, in no country's colors, and is the one pair both locales
 * can read.
 *
 * The thumb is the app's existing "selected" idiom - `bg-indigo-600 text-white shadow-sm`, the same fixed
 * pair the pagination and the AI tabs use. It is deliberately not a themed surface: brand fills are not
 * re-themed (see globals.css), so this keeps its contrast in both themes with no `dark:` variant. Only the
 * track follows the theme.
 *
 * `onDark` is for the landing and sign-in screens, which are dark in both themes. There the track has to be
 * a light wash over the page rather than a themed chip: a themed surface follows the page into black and
 * takes the control with it, which is what happened when the sign-in screen's selected segment became one.
 *
 * Two segments, deliberately. The thumb is half the track and parks at one end or the other, and Tailwind
 * only emits the translate utilities it can read literally - so a third locale means rewriting this, not
 * just adding to LOCALES.
 */
export function LanguageSwitcher({
  className = "",
  onDark = false,
}: {
  className?: string;
  onDark?: boolean;
}) {
  const { locale, setLocale } = useI18n();
  const activeIndex = Math.max(0, LOCALES.indexOf(locale));

  const track = onDark ? "border-white/20 bg-white/10" : "border-line bg-muted-surface";
  const segment = (active: boolean) =>
    active ? "text-white" : onDark ? "text-white/70 hover:text-white" : "text-muted hover:text-strong";

  return (
    <div
      role="group"
      aria-label="Language"
      className={`relative inline-grid grid-cols-2 items-center rounded-full border p-0.5 ${track} ${className}`}
    >
      {/* Half the track minus one padding step per side, so the thumb is flush at either end. */}
      <span
        aria-hidden="true"
        className={`pointer-events-none absolute inset-y-0.5 left-0.5 w-[calc(50%-0.125rem)] rounded-full bg-indigo-600 shadow-sm transition-transform duration-200 ease-out motion-reduce:transition-none ${
          activeIndex === 1 ? "translate-x-full" : "translate-x-0"
        }`}
      />
      {LOCALES.map((loc) => (
        <button
          key={loc}
          type="button"
          onClick={() => setLocale(loc)}
          aria-pressed={locale === loc}
          aria-label={LOCALE_LABELS[loc]}
          title={LOCALE_LABELS[loc]}
          className={`relative min-w-9 rounded-full px-2.5 py-1.5 text-xs font-semibold transition-colors ${segment(
            locale === loc
          )}`}
        >
          {SEGMENT_LABELS[loc]}
        </button>
      ))}
    </div>
  );
}

/**
 * Keeps <html lang> in sync with the active locale. The root layout renders a
 * server-side default which this corrects once the client knows the preference.
 */
export function LocaleHtmlLang() {
  const { locale } = useI18n();

  useEffect(() => {
    document.documentElement.lang = locale === "en" ? "en" : "zh-CN";
  }, [locale]);

  return null;
}
