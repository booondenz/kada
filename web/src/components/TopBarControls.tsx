import { GitHubLink } from "./GitHubLink";
import { LanguageSwitcher } from "./LanguageSwitcher";
import { ThemeToggle } from "./ThemeToggle";
import { TOP_BAR_CAPSULE_OVERHANG, TOP_BAR_CONTROLS_PINNED } from "./topBar";

/**
 * The top bar's right-hand cluster, composed in one place for the three shells that show it: the app
 * shell's bar, the landing page's floating header and the sign-in page's pinned corner.
 *
 * The order is load-bearing. The capsule is the last child because it is the one control all three pages
 * share and the one that has to land on the same pixel: anything placed to its right would push it left by
 * that control's width, and a page with one control fewer would push it by a different amount. The GitHub
 * link sits immediately beside it, which is the one position that is the same on all three pages. The
 * theme toggle takes the outside seat, because it is the only one of the three a page can omit - the dark
 * pages have no use for it - and dropping the outermost control leaves the two shared ones where they were.
 *
 * `onDark` is the flag the capsule takes - this page is dark in both themes - and it also decides whether
 * there is a toggle: on those pages an explicit light choice would change nothing, so the control that
 * offers it is absent rather than present and inert. One fact with two consequences, so one prop, and no
 * second flag that could disagree with the first.
 *
 * `pinned` is the sign-in page, which has no bar to sit in. It is the same cluster on the viewport corner
 * the bar would have put it in; the corner lives here too, so a shell cannot half-copy it.
 */
export function TopBarControls({ onDark = false, pinned = false }: { onDark?: boolean; pinned?: boolean }) {
  const controls = (
    <div className="flex items-center gap-2">
      {!onDark && <ThemeToggle />}
      <GitHubLink onDark={onDark} />
      <LanguageSwitcher onDark={onDark} className={TOP_BAR_CAPSULE_OVERHANG} />
    </div>
  );

  return pinned ? <div className={TOP_BAR_CONTROLS_PINNED}>{controls}</div> : controls;
}
