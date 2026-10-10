export function cn(...classes: (string | boolean | undefined | null)[]): string {
  return classes.filter(Boolean).join(" ");
}

/**
 * safeHref only allows http/https links for <a href>.
 * The backend already rejects javascript:/data: protocols; this also covers legacy data,
 * preventing script execution inside the panel origin on click; invalid values return "#".
 */
export function safeHref(url: string | null | undefined): string {
  if (!url) return "#";
  try {
    const u = new URL(url);
    if (u.protocol === "http:" || u.protocol === "https:") {
      return url;
    }
  } catch {
    // Unparseable values are treated as unsafe
  }
  return "#";
}
