const TOKEN_KEY = "kada_token";
const USER_KEY = "kada_user";

/**
 * The signed-in account, as the API publishes it.
 *
 * The phone number and nothing else: it is the sign-in method, the unique key, and the only field
 * `/api/me` returns (see domain.UserInfo on the backend). What is kept in localStorage is a copy of the
 * last login response - something immutable, cached, rather than state that can drift.
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

export function isAuthenticated(): boolean {
  return !!getToken();
}
