import { GitHubLink } from "./GitHubLink";
import { LanguageSwitcher } from "./LanguageSwitcher";
import { ThemeToggle } from "./ThemeToggle";
import { TOP_BAR_CAPSULE_OVERHANG, TOP_BAR_CONTROLS_PINNED } from "./topBar";

/**
 * The top bar's right-hand cluster, composed in one place for the three shells that show it: the app
 * shell's bar, the landing page's header and the sign-in page's pinned corner.
 *
 * The order is load-bearing. The capsule is the last child because it is the one control all three pages
 * share and the one that has to land on the same pixel: anything placed to its right would push it left by
 * that control's width, and a page with one control fewer would push it by a different amount. The GitHub
 * link sits immediately beside it, which is the one position that is the same on all three pages. The
 * theme toggle takes the outside seat, because it is the only one of the three a page could omit.
 *
 * There used to be an `onDark` flag, because the landing and sign-in screens were dark in both themes and
 * their controls had to be a light wash rather than a themed chip. Both pages follow the theme now, so the
 * cluster is drawn from the tokens everywhere and the flag is gone - along with the third set of styles it
 * made possible.
 *
 * `pinned` is the sign-in page, which has no bar to sit in. It is the same cluster on the viewport corner
 * the bar would have put it in; the corner lives here too, so a shell cannot half-copy it.
 */
export function TopBarControls({ pinned = false }: { pinned?: boolean }) {
  const controls = (
    <div className="flex items-center gap-2">
      <ThemeToggle />
      <GitHubLink />
      <LanguageSwitcher className={TOP_BAR_CAPSULE_OVERHANG} />
    </div>
  );

  return pinned ? <div className={TOP_BAR_CONTROLS_PINNED}>{controls}</div> : controls;
}
