import { useEffect, useLayoutEffect, useRef } from "react";

/**
 * useEventStream — subscribe to one Server-Sent Events type from the
 * backend's `/api/v1/events` stream and run a handler each time a
 * matching event arrives.
 *
 * Why SSE instead of polling: this hook backs the admin "live signal"
 * UX (channel health, library scans, EPG refreshes). The events
 * happen rarely but the admin wants to see the change *now*, not in
 * 30s. SSE matches that shape cleanly: unidirectional server→client,
 * one HTTP/1.1 long-lived connection, browser handles reconnect with
 * exponential backoff for free, traverses every CDN/proxy because it
 * is plain HTTP.
 *
 * Why not WebSocket here: we have no need for client→server messages
 * on this channel (commands go through the REST API). Reserving WS
 * for the future "now playing / sync" use case keeps the surface
 * area honest — WS is the right tool for bidirectional state, SSE
 * for fan-out notifications.
 *
 * Connection sharing: each call opens its own EventSource. That is
 * acceptable today because admin pages mount at most a handful of
 * subscriptions. If we ever need multiplexing (a big page with N
 * useEventStream calls), promote this to a singleton with refcounts.
 *
 * Auth: EventSource sends cookies automatically (HTTP/1.1 GET with
 * credentials), so it inherits whatever cookie-based session the
 * rest of the app uses. No header plumbing required.
 *
 * Cleanup: returns an explicit close on unmount so a fast nav between
 * admin pages doesn't leak connections (each open EventSource holds
 * a server-side handler subscription).
 */
export function useEventStream(
  /** Event type as published by the backend (e.g. "channel.health.changed"). */
  type: string,
  /**
   * Called on each matching event. The data string is the raw JSON
   * payload — caller parses if it cares; pass-through is fine for the
   * common "just invalidate a query" case.
   */
  onEvent: (data: string) => void,
  /** Pause the subscription without unmounting the component. */
  enabled = true,
) {
  // Stash the latest handler in a ref so we don't tear down and
  // recreate the EventSource every time the parent re-renders with
  // a new closure. useLayoutEffect (instead of plain assignment in
  // render) keeps the React-Hooks linter happy and guarantees the
  // ref is updated before any committed effect reads it.
  const handlerRef = useRef(onEvent);
  useLayoutEffect(() => {
    handlerRef.current = onEvent;
  }, [onEvent]);

  useEffect(() => {
    if (!enabled) return;

    const source = new EventSource("/api/v1/events", { withCredentials: true });
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
