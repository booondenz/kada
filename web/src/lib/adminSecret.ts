/**
 * The operator secret, kept in this browser.
 *
 * The site has no accounts, so the operator routes are guarded by a shared secret set on the host
 * (`ADMIN_SECRET`) rather than by a sign-in. It is typed into the console once and kept here; clearing site
 * data forgets it, which is the same as losing a session and is the honest behaviour for a secret.
 */
const KEY = "kada_admin_secret";

export function getAdminSecret(): string {
  try {
    return localStorage.getItem(KEY) ?? "";
  } catch {
    return "";
  }
}

export function setAdminSecret(secret: string): void {
  try {
    localStorage.setItem(KEY, secret);
  } catch {
    // Storage can be unavailable (private mode); the console then asks every time, which still works.
  }
}
