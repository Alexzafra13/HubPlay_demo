import { useQueryClient } from "@tanstack/react-query";
import { queryKeys } from "@/api/queryKeys";
import { useEventStream } from "./useEventStream";

/**
 * usePlaylistRefreshEvents — app-wide listener for IPTV M3U refresh
 * completion events.
 *
 * Why this lives outside `useRefreshM3U`: the per-mutation listener
 * inside the hook only stays alive while the mutation's component is
 * mounted. That breaks three real-world flows:
 *
 *   1. Operator triggers "Refresh M3U" on a 1k-channel Xtream feed
 *      (~90 s import) and navigates to Live TV before it finishes —
 *      the modal unmounts, its EventSource closes, the channel cache
 *      is never invalidated, the page shows the empty list it cached
 *      pre-import.
 *
 *   2. Page reloads mid-import (browser refresh, dev HMR) — the
 *      mutation is gone but the backend is still chugging through
 *      the playlist. Without this listener, no one invalidates the
 *      cache when the import eventually completes.
 *
 *   3. The scheduler fires an unattended refresh (cron) — there is
 *      no mutation at all. The channel list, unhealthy list, and
 *      EPG-source status would silently go stale until the user
 *      navigates somewhere that triggers a refetch on its own.
 *
 * Mounted once at the AppLayout level so it's active for every
 * authenticated route. Unauthenticated /login, /setup don't need it
 * (the SSE endpoint requires auth anyway).
 *
 * Two separate `useEventStream` calls (one per event type) match the
 * existing pattern in LivetvAdminPanel — each opens its own
 * EventSource. If we ever stack many of these on a single page,
 * promote useEventStream to a singleton with refcounts.
 */
export function usePlaylistRefreshEvents() {
  const queryClient = useQueryClient();

  useEventStream("playlist.refreshed", (raw) => {
    try {
      const evt = JSON.parse(raw) as {
        data?: { library_id?: string };
      };
      const libraryId = evt.data?.library_id;
      if (!libraryId) return;
      // Invalidate every cache that the import could have written to.
      // The library row gets a touched updated_at + maybe a discovered
      // EPG URL; channels/* are obviously affected; the unhealthy and
      // without-epg lists are derived from channels and the EPG-source
      // status row gets bumped if a fresh URL was discovered.
      queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
      queryClient.invalidateQueries({ queryKey: queryKeys.library(libraryId) });
      queryClient.invalidateQueries({
        queryKey: queryKeys.channels(libraryId),
      });
      queryClient.invalidateQueries({
        queryKey: queryKeys.channelGroups(libraryId),
      });
      queryClient.invalidateQueries({
        queryKey: queryKeys.unhealthyChannels(libraryId),
      });
      queryClient.invalidateQueries({
        queryKey: queryKeys.channelsWithoutEPG(libraryId),
      });
      queryClient.invalidateQueries({
        queryKey: queryKeys.libraryEPGSources(libraryId),
      });
      // bulk-schedule sits under a different key but channels feed it,
      // so a fresh import shifts which programs/EPG join the listing.
      queryClient.invalidateQueries({ queryKey: ["bulk-schedule"] });
    } catch {
      /* malformed payload — server-side bug; ignore for end-user UX */
    }
  });

  useEventStream("playlist.refresh_failed", (raw) => {
    // Failure path doesn't touch caches: whatever channels were
    // already in place stay in place. We log for devtools visibility
    // so an operator can correlate a stuck spinner with a backend
    // failure; toast/error UI hangs off the per-mutation listener
    // (where the operator's intent is fresh) and is the right place
    // to surface a user-facing error.
    try {
      const evt = JSON.parse(raw) as {
        data?: { library_id?: string; error?: string };
      };
      // eslint-disable-next-line no-console
      console.warn("[playlist] refresh failed", evt.data);
    } catch {
      /* ignore malformed payload */
    }
  });
}
