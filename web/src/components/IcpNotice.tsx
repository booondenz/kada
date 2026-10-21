/**
 * The ICP filing number, which the regulator requires the site to display and link.
 *
 * It is a plain constant rather than a translated string: the filing office checks the characters, not the
 * prose around them, and a number that differed between languages would mean one of the two was wrong. It
 * is also deliberately in one place - the string is legal text, and a second copy could only disagree with
 * the first.
 *
 * The number is the one the filing order shows: `豫ICP备2026051248号`. A filing that holds several websites
 * spells each one with a sequence suffix (`号-1`, `号-2`), which the order shows next to the site; if this
 * one is ever spelled that way, this constant is the only line that changes.
 */
export const ICP_NUMBER = "豫ICP备2026051248号";

/** The filing office's public query, which the displayed number has to link to. */
export const ICP_QUERY_URL = "https://beian.miit.gov.cn/";

/**
 * The number as a link.
 *
 * The caller supplies the styling: the two places that show it place it differently - the user page puts it
 * at the end of the page, under the version string, and the privacy policy puts it under an article - and a
 * component that picked its own colour would be wrong in one of those places.
 */
export function IcpNotice({ className = "" }: { className?: string }) {
  return (
    <a href={ICP_QUERY_URL} target="_blank" rel="noreferrer noopener" className={className}>
      {ICP_NUMBER}
    </a>
  );
}
