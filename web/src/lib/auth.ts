const TOKEN_KEY = "kada_token";
const USER_KEY = "kada_user";

/**
 * The account the app runs as, as the API publishes it.
 *
 * A copy of the last session response, and the phone number is empty until the guest account is claimed -
 * which is the difference between "we do not know who you are" and "there is no account", and the reason
 * it is optional here.
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
  localStorage.removeItem(USER_KEY);
}

export function getUser(): User | null {
  if (typeof window === "undefined") return null;
  const raw = localStorage.getItem(USER_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

export function setUser(user: User) {
  localStorage.setItem(USER_KEY, JSON.stringify(user));
}
