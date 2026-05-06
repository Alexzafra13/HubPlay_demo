import { useCallback } from "react";
import { useQueryClient, type QueryClient, type QueryKey } from "@tanstack/react-query";
import { useUserEventStream } from "./useUserEventStream";
import { queryKeys } from "@/api/queryKeys";

// Per-item keys (item(id), progress(id)) are only worth invalidating when
// some component actually fetched them — otherwise we are scheduling a
// refetch on a query nobody observes, which still costs a predicate match
// per pending invalidation. Global rails (continue-watching, next-up,
// favorites) are different: Home consumes them on every login, so they
// must always be marked stale regardless of the active route.
function invalidateIfCached(qc: QueryClient, queryKey: QueryKey) {
  if (qc.getQueryData(queryKey) !== undefined) {
    qc.invalidateQueries({ queryKey });
  }
}

// Cross-device sync orchestrator. Mounts the three user-scoped SSE
// subscriptions (progress / played / favourite) and translates each
// arriving event into the right TanStack Query invalidations so the
// UI catches up to whatever this user just did on another device.
//
// What gets invalidated:
//
//   user.progress.updated:
//     - items/{item_id}        — the item detail page reads progress
//                                from user_data.
//     - continue-watching      — the rail re-orders / surfaces the
//                                item if it crossed the partial-progress
//                                threshold.
//     - progress/{item_id}     — the dedicated progress query the
//                                player polls.
//
//   user.played.toggled:
//     - items/{item_id}
//     - continue-watching      — played items leave the rail.
//     - next-up                — series watch chain advances on
//                                episode play.
//
//   user.favorite.toggled:
//     - items/{item_id}
//     - favorites              — the favourites rail.
//
// Why mount this once at the app shell instead of per-page: the events
// are useful regardless of which page the user is currently on
// (favourites toggled while looking at /home should refresh the
// home-page favourites rail too). One mount + global QueryClient
// invalidation is the minimal correct shape; per-page hooks would
// either miss events fired while another page was active or fan out
// duplicate connections.
//
// Pause hook: when the user logs out (or before login), the SSE
// connection would 401-loop trying to reconnect with no cookie. Pass
// `enabled = false` to suspend the subscription cleanly.
export function useUserDataSync({ enabled = true }: { enabled?: boolean } = {}) {
  const queryClient = useQueryClient();

  // The three handlers parse the JSON payload defensively — a
  // malformed event from the server (corrupted JSON, missing
  // item_id) is a no-op rather than an exception that bubbles into
  // EventSource's listener and breaks future deliveries.
  const onProgress = useCallback(
    (raw: string) => {
      const data = parsePayload(raw);
      const itemId = typeof data?.item_id === "string" ? data.item_id : null;
      if (!itemId) return;
      invalidateIfCached(queryClient, queryKeys.item(itemId));
      invalidateIfCached(queryClient, queryKeys.progress(itemId));
      queryClient.invalidateQueries({ queryKey: queryKeys.continueWatching });
    },
    [queryClient],
  );

  const onPlayed = useCallback(
    (raw: string) => {
      const data = parsePayload(raw);
      const itemId = typeof data?.item_id === "string" ? data.item_id : null;
      if (!itemId) return;
      invalidateIfCached(queryClient, queryKeys.item(itemId));
      queryClient.invalidateQueries({ queryKey: queryKeys.continueWatching });
      queryClient.invalidateQueries({ queryKey: queryKeys.nextUp });
    },
    [queryClient],
  );

  const onFavorite = useCallback(
    (raw: string) => {
      const data = parsePayload(raw);
      const itemId = typeof data?.item_id === "string" ? data.item_id : null;
      if (!itemId) return;
      invalidateIfCached(queryClient, queryKeys.item(itemId));
      queryClient.invalidateQueries({ queryKey: queryKeys.favorites });
    },
    [queryClient],
  );

  useUserEventStream("user.progress.updated", onProgress, enabled);
  useUserEventStream("user.played.toggled", onPlayed, enabled);
  useUserEventStream("user.favorite.toggled", onFavorite, enabled);
}

// SSE event payloads come in as the JSON string the server wrote;
// EventSource doesn't auto-parse. The wire envelope is
// `{type: "...", data: { user_id, item_id, ... }}` — we hand the
// inner `data` to the caller, which is the bit that varies per event.
function parsePayload(raw: string): Record<string, unknown> | null {
  try {
    const parsed = JSON.parse(raw);
    if (
      parsed &&
      typeof parsed === "object" &&
      "data" in parsed &&
      parsed.data &&
      typeof parsed.data === "object"
    ) {
      return parsed.data as Record<string, unknown>;
    }
    return null;
  } catch {
    return null;
  }
}
