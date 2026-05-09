import { useSyncExternalStore } from "react";

/**
 * useIsMobile subscribes to a `matchMedia` query via
 * useSyncExternalStore, which is the React-18+ canonical way to
 * mirror browser state into React without an effect. This avoids
 * the cascading render `setState-in-effect` would produce (initial
 * state from innerWidth, effect then reconciles with matchMedia).
 *
 * Default breakpoint is 768px — Tailwind's `md` boundary, the same
 * one used across the project's `md:` utilities. Caller can pass a
 * different number when a specific component needs to flip earlier
 * or later (e.g. an editorial header that breaks at 1024px).
 */
export function useIsMobile(breakpoint = 768): boolean {
  const query = `(max-width: ${breakpoint - 1}px)`;
  return useSyncExternalStore(
    (onChange) => {
      const mql = window.matchMedia(query);
      mql.addEventListener("change", onChange);
      return () => mql.removeEventListener("change", onChange);
    },
    () => window.matchMedia(query).matches,
    () => false, // SSR fallback — we're a mobile-last client app.
  );
}
