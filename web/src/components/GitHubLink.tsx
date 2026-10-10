import { useT } from "@/lib/i18n";

/**
 * The project's own remote, as `git remote -v` prints it. Written down once here so the bar cannot end up
 * pointing at a fork or a mirror that has moved on.
 */
const REPO_URL = "https://github.com/booondenz/kada";

/**
 * The top bar's way out to the source, on every page.
 *
 * A new tab, not a navigation: the app is what the visitor came for, and a same-tab jump to GitHub is the
 * one link on the page with no way back to the dashboard. The accessible name is the brand itself, the same
 * word in both locales, so its dictionary entry is an identity pair rather than a translation - it is there
 * to keep every visible label in one list, not because there is anything to translate.
 *
 * The mark is drawn here rather than imported. lucide-react is the set every other control is built from,
 * but its 1.x releases dropped the brand marks - the installed 1.24.0 ships no `github` icon at all - and
 * the alternatives were worse: a solid Octocat sits visibly heavier than a 16px stroked glyph beside the
 * theme toggle and the capsule, and a second icon package for one symbol is not worth a dependency. What
 * is vendored is the outline lucide used to ship, so the stroke weight and the round caps are the ones the
 * rest of the bar is drawn with.
 *
 * `onDark` works like the capsule's: the landing and sign-in pages are dark in both themes, so here the
 * control has to be a light wash over the page rather than a themed chip.
 */
export function GitHubLink({ onDark = false }: { onDark?: boolean }) {
  const t = useT();
  const label = t("GitHub");

  return (
    <a
      href={REPO_URL}
      target="_blank"
      rel="noreferrer"
      aria-label={label}
      title={label}
      className={`rounded-lg p-2 transition-colors ${
        onDark
          ? "text-white/70 hover:bg-white/10 hover:text-white"
          : "text-muted hover:bg-muted-surface hover:text-strong"
      }`}
    >
      <GitHubMark />
    </a>
  );
}

/**
 * The mark at the same 16px (`size-4`) as the theme toggle's icons, on the same 24-unit box and 2px
 * round-capped stroke lucide draws with. Decorative - the link carries the accessible name.
 */
function GitHubMark() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="size-4"
      aria-hidden="true"
    >
      <path d="M15 22v-4a4.8 4.8 0 0 0-1-3.5c3 0 6-2 6-5.5.08-1.25-.27-2.48-1-3.5.28-1.15.28-2.35 0-3.5 0 0-1 0-3 1.5-2.64-.5-5.36-.5-8 0C6 2 5 2 5 2c-.3 1.15-.3 2.35 0 3.5A5.403 5.403 0 0 0 4 9c0 3.5 3 5.5 6 5.5-.39.49-.68 1.05-.85 1.65-.17.6-.22 1.23-.15 1.85v4" />
      <path d="M9 18c-4.51 2-5-2-7-2" />
    </svg>
  );
}
