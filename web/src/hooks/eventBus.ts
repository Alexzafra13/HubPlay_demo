// eventBus — process-wide multiplexer over EventSource connections.
//
// Why this exists:
//   useUserDataSync mounts three useUserEventStream calls (progress /
//   played / favourite). Before the bus, each call opened its own
//   EventSource against the same /me/events URL — three TCP/HTTP
//   connections per logged-in tab to receive the exact same payload.
//   Chrome caps SSE at ~6 connections per origin, so combined with
//   admin streams (channel health, library scans) a power-user could
//   silently saturate the limit and watch later subscriptions stall
//   without any error surfaced to the UI.
//
//   The bus replaces N connections per URL with 1. Every subscriber
//   gets its handler invoked from a single EventSource; refcounts
//   close the connection when the last listener unsubscribes.
//
// Why this is not in a global store:
//   The bus owns no React state — it is a pure side-effect cache. The
//   hooks below wrap subscribe() in useEffect so React owns the
//   lifecycle. Tests can reset the bus via __resetEventBusForTests.
//
// Failure semantics:
//   A handler that throws does not poison the dispatch loop; the
//   error is swallowed (already true of the previous per-call
//   implementation). EventSource handles reconnect on its own with
//   exponential backoff — we do not re-dispatch missed events.

export type SseHandler = (data: string) => void;

interface Channel {
  source: EventSource;
  // Per-type dispatcher registered on the EventSource. Stored so
  // unsubscribe can remove it once the last handler for that type
  // goes away (avoids a slow leak of dead listeners).
  dispatchers: Map<string, (e: MessageEvent) => void>;
  handlers: Map<string, Set<SseHandler>>;
  refcount: number;
  withCredentials: boolean;
}

const channels = new Map<string, Channel>();

function channelKey(url: string, withCredentials: boolean): string {
  // Two channels with different credential modes against the same URL
  // would conflict on the EventSource constructor; key by both so we
  // never reuse a connection that was opened with different cookies.
  return `${withCredentials ? "creds" : "anon"}|${url}`;
}

function getOrCreateChannel(url: string, withCredentials: boolean): Channel {
  const key = channelKey(url, withCredentials);
  const existing = channels.get(key);
  if (existing) return existing;

  const source = new EventSource(url, { withCredentials });
  const ch: Channel = {
    source,
    dispatchers: new Map(),
    handlers: new Map(),
    refcount: 0,
    withCredentials,
  };
  channels.set(key, ch);
  return ch;
}

/**
 * Subscribe `handler` to events of `type` on `url`. Returns an
 * unsubscribe function. The same URL is multiplexed across
 * subscribers, so calling subscribe() N times opens at most one
 * EventSource per URL+credentials pair.
 */
export function subscribeSse(
  url: string,
  withCredentials: boolean,
  type: string,
  handler: SseHandler,
): () => void {
  const key = channelKey(url, withCredentials);
  const ch = getOrCreateChannel(url, withCredentials);
  ch.refcount += 1;

  let typeHandlers = ch.handlers.get(type);
  if (!typeHandlers) {
    typeHandlers = new Set<SseHandler>();
    ch.handlers.set(type, typeHandlers);

    const dispatch = (e: MessageEvent) => {
      // Snapshot the set before dispatch so a handler that
      // unsubscribes synchronously does not mutate what we iterate.
      const snapshot = Array.from(ch.handlers.get(type) ?? []);
      for (const h of snapshot) {
        try {
          h(typeof e.data === "string" ? e.data : String(e.data));
        } catch {
          // A bad handler must not break siblings or the underlying
          // EventSource. Errors here typically mean malformed JSON
          // upstream, which the caller should already defend against.
        }
      }
    };
    ch.dispatchers.set(type, dispatch);
    ch.source.addEventListener(type, dispatch);
  }
  typeHandlers.add(handler);

  return function unsubscribe() {
    const handlers = ch.handlers.get(type);
    if (handlers) {
      handlers.delete(handler);
      if (handlers.size === 0) {
        const dispatch = ch.dispatchers.get(type);
        if (dispatch) {
          ch.source.removeEventListener(type, dispatch);
        }
        ch.dispatchers.delete(type);
        ch.handlers.delete(type);
      }
    }

    ch.refcount -= 1;
    if (ch.refcount <= 0) {
      ch.source.close();
      channels.delete(key);
    }
  };
}

// Test-only helpers. Exported as a nested namespace so production
// code that imports subscribeSse cannot accidentally call them.
export const __eventBusTestHelpers = {
  reset() {
    for (const ch of channels.values()) {
      ch.source.close();
    }
    channels.clear();
  },
  channelCount(): number {
    return channels.size;
  },
  refcount(url: string, withCredentials: boolean): number {
    return channels.get(channelKey(url, withCredentials))?.refcount ?? 0;
  },
};
