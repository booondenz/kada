const TOKEN_KEY = "kada_token";

/**
 * The account the app runs as, as the API publishes it.
 *
 * A copy of the session response. The phone number is null for every account this build can create: the
 * id is the whole identity, and the field is here because the API still publishes it.
 */
export interface User {
  id: number;
  phone?: string;
}

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token);
}

export function removeToken() {
  localStorage.removeItem(TOKEN_KEY);
}
