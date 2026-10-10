import { Moon, Sun } from "lucide-react";
import { useTheme } from "@/lib/theme";
import { useT } from "@/lib/i18n";
import { useHydrated } from "@/lib/useHydrated";

/**
 * Light/dark switch for the top bar.
 *
 * The three-way control (follow system / light / dark) lives on the settings page; this is the one-click
 * version people actually reach for, so it only has to answer "the other one, please". Clicking it pins
 * an explicit choice, which is what makes the icon stop following the OS - so the icon shown is the theme
 * that will be applied, never the one being requested.
 *
 * Before hydration the icon is rendered from the server's assumption (light). Rendering the real one
 * would mean reading localStorage during the first client render, which is exactly the mismatch
 * useHydrated exists to prevent; the button stays disabled until then so a click cannot act on a stale
 * theme.
 */
export function ThemeToggle() {
  const { theme, setTheme } = useTheme();
  const t = useT();
  const hydrated = useHydrated();

  const isDark = hydrated && theme === "dark";
  const next = isDark ? "light" : "dark";
  const label = isDark ? t("切换到浅色模式") : t("切换到深色模式");

  return (
    <button
      type="button"
      onClick={() => setTheme(next)}
      disabled={!hydrated}
      aria-label={label}
      title={label}
      className="rounded-lg p-2 text-muted transition-colors hover:bg-muted-surface hover:text-strong disabled:opacity-40"
    >
      {isDark ? <Sun className="size-4" /> : <Moon className="size-4" />}
    </button>
  );
}
