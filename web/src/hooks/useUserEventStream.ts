import { useEffect, useLayoutEffect, useRef } from "react";

/**
 * useUserEventStream — subscribe to one Server-Sent Events type from
 * the user-scoped `/api/v1/me/events` stream.
 *
 * Sibling of useEventStream (which targets the global /events feed).
 * The split is on purpose:
 *
 *   - /events carries admin-style notifications (library scans,
 *     channel health, EPG refreshes) that anyone can see.
 *   - /me/events carries per-user state (watch progress, played,
 *     favourites). The server filters by the authenticated user
 *     before fan-out so device A's state never leaks to user B.
 *
 * The cross-device sync use case: I start an episode on the laptop,
 * the server publishes user.progress.updated; my phone receives the
 * event and invalidates Continue Watching. ~50ms instead of waiting
 * 60s for the next staleTime.
 *
 * Why a second hook instead of parameterising useEventStream: the
 * URL different is the obvious bit, but more importantly the contract
 * differs — /me/events requires auth and the EventSource opens behind
 * cookie credentials; admin code calling this hook would silently get
 * an unauth'd connection that closes on first ping. Naming the URL in
 * the hook surface makes the intent explicit at the call site.
 *
 * Connection sharing: each call opens its own EventSource. Acceptable
 * because most pages mount one or two subscriptions; if a page ever
 * needs many, promote both useEventStream variants to a singleton
 * with refcounts.
 */
export function useUserEventStream(
  /** Event type as published by the backend (e.g. "user.progress.updated"). */
  type: string,
  /**
   * Called on each matching event. The data string is the raw JSON
   * payload — caller parses if it cares.
   */
  onEvent: (data: string) => void,
  /** Pause the subscription without unmounting the component. */
  enabled = true,
) {
  const handlerRef = useRef(onEvent);
  useLayoutEffect(() => {
    handlerRef.current = onEvent;
  }, [onEvent]);

  useEffect(() => {
    if (!enabled) return;

    const source = new EventSource("/api/v1/me/events", { withCredentials: true });
    const listener = (e: MessageEvent) => {
      handlerRef.current(e.data);
    };
    source.addEventListener(type, listener);

    return () => {
      source.removeEventListener(type, listener);
      source.close();
    };
  }, [type, enabled]);
}
