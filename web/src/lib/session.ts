import { authAPI } from "./api";
import { getToken, removeToken, setToken } from "./auth";

/**
 * The session the app runs in.
 *
 * There is no sign-in screen any more: opening the site is enough to use it. The first page load asks the
 * API for a guest session, which is an ordinary account row with no phone number on it (see
 * `AuthService.CreateGuest`), and every request after that carries the token it minted. Nothing in the app
 * has to know that the account has no identity: the links it creates have an owner, and the id in the
 * token is that owner - there is nothing to claim and nobody to claim it with.
 *
 * The trade-off is stated where the user can see it rather than here: a guest account lives in this
 * browser's storage, so clearing the browser loses it, and an expiring token starts a new one.
 */

/**
 * True when the token's own `exp` claim is in the past, or the token cannot be read as a JWT.
 *
 * The check is deliberately local and unsigned: this is not on the trust path - the server verifies the
 * signature on every request, and a forged `exp` buys nothing. It only decides whether to *reuse* a stored
 * token, and without it an expired one would make every page fail with 401s and no way back, since the app
 * has no sign-in form to recover through.
 */
export function isExpired(token: string, nowMs: number = Date.now()): boolean {
  const payload = token.split(".")[1];
  if (!payload) return true;

  try {
    // base64url to base64, and back to a length `atob` accepts: JWT payloads drop their `=` padding.
    const base64 = payload.replace(/-/g, "+").replace(/_/g, "/");
    const padded = base64.padEnd(Math.ceil(base64.length / 4) * 4, "=");
    const claims = JSON.parse(atob(padded)) as { exp?: unknown };

    return typeof claims.exp !== "number" || claims.exp * 1000 <= nowMs;
  } catch {
    return true;
  }
}

// One request in flight per page, shared by every caller. Two components mounting at once would otherwise
// open two accounts and keep whichever token was written last, orphaning the other.
let pending: Promise<string> | null = null;

/**
 * Returns the session token, opening a guest session if this browser has none.
 *
 * `pending` is reset on failure so a retry is possible, and never on success: after the first call the
 * stored token answers every subsequent one.
 */
export function ensureSession(): Promise<string> {
  const existing = getToken();
  if (existing && !isExpired(existing)) {
    return Promise.resolve(existing);
  }

  // An expired token is worse than no token: it would be sent on every request and 401 every time.
  if (existing) removeToken();

  pending ??= authAPI
    .guest()
    .then((data) => {
      setToken(data.token);
      return data.token;
    })
    .catch((err: unknown) => {
      pending = null;
      throw err;
    });

  return pending;
}
