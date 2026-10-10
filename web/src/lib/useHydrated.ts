import { useSyncExternalStore } from "react";

/** Nothing to subscribe to: this flips exactly once, when React first runs in the browser. */
const subscribeToNothing = () => () => {};

/**
 * True only once running in the browser; false during server rendering and the first client render.
 *
 * Any component that reads `localStorage` (a token, a stored user, a preference) has to gate on this.
 * The value is not available on the server, so rendering it directly makes the server's output disagree
 * with the client's first render - React then reports a hydration mismatch and throws away the tree.
 *
 * `useSyncExternalStore` expresses that distinction without setting state in an effect, which would
 * cascade a second render just to say "we are in the browser now".
 */
export function useHydrated(): boolean {
  return useSyncExternalStore(
    subscribeToNothing,
    () => true,
    () => false
  );
}
