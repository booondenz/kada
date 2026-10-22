/**
 * The public security filing number, which the site has to display and link once the 公安备案 is granted.
 *
 * It is a second filing from a second office - the ICP number comes from the telecom administration and this
 * one from the public security bureau - and both are checks that the site can be traced to somebody. The
 * rule is the same for both: the number is displayed as filed and links to the office's public query.
 *
 * Like the ICP number it is a constant rather than a translated string: the office checks the characters,
 * not the prose around them, and a number that differed between languages would mean one of them was wrong.
 */
export const PUBLIC_SECURITY_NUMBER = "豫公网安备41138102000392号";

/** The code inside the number, which is what the query page takes. */
const FILING_CODE = "41138102000392";

/** The filing office's public query, which the displayed number has to link to. */
export const PUBLIC_SECURITY_QUERY_URL = `https://beian.mps.gov.cn/#/query/webSearch?code=${FILING_CODE}`;

/**
 * The number as a link.
 *
 * The caller supplies the styling, for the same reason as the ICP notice: it appears in two footers that
 * place it differently.
 */
export function PublicSecurityNotice({ className = "" }: { className?: string }) {
  return (
    <a href={PUBLIC_SECURITY_QUERY_URL} target="_blank" rel="noreferrer noopener" className={className}>
      {PUBLIC_SECURITY_NUMBER}
    </a>
  );
}
