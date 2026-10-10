/**
 * Where the top bar's controls sit, shared by the three shells that show them.
 *
 * TopBarControls is what spends these numbers; a shell only places it. The app shell's bar holds it, and
 * the landing and sign-in pages - which have no bar, being dark in both themes - pin it to the viewport
 * corner instead. All three have to land on the same pixel, or the capsule visibly jumps sideways and up
 * as you sign in, which is exactly what the hand-written copies of these numbers did (16px against 24px on
 * the horizontal, and four pixels higher on the dark pages).
 *
 * The pinned vertical offset is not a second rhythm: it is the bar's own centring with the bar removed,
 * (56px bar - cluster height) / 2. The cluster is 34px because its tallest control is the capsule, which
 * draws a 1px border and a 2px inset around a 28px button row; the 32px icon controls inside it are centred
 * by the cluster, so they land on 12px either way and the capsule is what the offset has to agree with.
 */
export const TOP_BAR_HEIGHT = "h-14";

/** Horizontal padding of the bar. The pinned copy matches its right edge. */
export const TOP_BAR_GUTTER = "px-4 sm:px-6";

/**
 * The capsule sits one step past that gutter - 12px from the edge on phones, 20px from `sm` up.
 *
 * A pill is all curve, so on a straight margin it reads as inset: the eye measures from where the shape
 * looks solid, and a rounded end leaves a wedge of page beside the border. One 4px step buys that back. It
 * is taken here, once, rather than per page, because three shells hand-placing the same control is how the
 * copies drifted apart the first time.
 *
 * Only the pill. The GitHub link and the theme toggle are rounded rectangles whose straight sides have no
 * such wedge, and the title inside the bar keeps the gutter it shares with the content column below. So
 * inside the cluster it is the last child's margin, which is what TopBarControls applies. The pinned copy
 * needs no second number: pinning the corner and letting the capsule overhang it lands on the same pixel
 * as the bar does.
 */
export const TOP_BAR_CAPSULE_OVERHANG = "-mr-1";

/** The same corner, for a page that has no bar to put the controls in. */
export const TOP_BAR_CONTROLS_PINNED = "fixed right-4 top-[11px] z-50 sm:right-6";
