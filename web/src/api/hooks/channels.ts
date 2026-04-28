// Channel queries + favorites + bulk schedule + the watch-rail beacon.
//
// Two favorite queries (IDs vs full list): the IDs cache backs the ♥
// toggle on every ChannelCard for instant feedback, the list cache
// is what the Favorites tab renders. Both invalidate together on
// mutation so the toggle and the tab can never diverge.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type { Channel, ContinueWatchingChannel, EPGProgram } from "../types";

export function useChannels(
  libraryId?: string,
  options?: Partial<UseQueryOptions<Channel[]>>,
) {
  return useQuery<Channel[]>({
    queryKey: queryKeys.channels(libraryId),
    queryFn: () => api.getChannels(libraryId),
    ...options,
  });
}

// ─── Channel favorites ────────────────────────────────────────────────

export function useChannelFavoriteIDs(
  options?: Partial<UseQueryOptions<string[]>>,
) {
  return useQuery<string[]>({
    queryKey: queryKeys.channelFavoriteIDs,
    queryFn: () => api.getChannelFavoriteIDs(),
    staleTime: 60_000,
    ...options,
  });
}

export function useChannelFavorites(
  options?: Partial<UseQueryOptions<Channel[]>>,
) {
  return useQuery<Channel[]>({
    queryKey: queryKeys.channelFavorites,
    queryFn: () => api.getChannelFavorites(),
    staleTime: 60_000,
    ...options,
  });
}

// useFavoriteMutation factors out the optimistic-update plumbing
// shared by add and remove: same query keys, same rollback context,
// same settled-invalidation. Only the API call and the local-set
// transform differ.
//
// Why a factory rather than two hooks: 80% of the body is identical,
// and the "what changes" part (one line each) reads more clearly when
// you see `apply: (ids, id) => [id, ...]` next to its remove sibling
// than when both flows are typed out in full.
function useFavoriteMutation(
  apiCall: (channelId: string) => Promise<void>,
  apply: (ids: string[], channelId: string) => string[],
) {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string, { previous: string[] | undefined }>({
    mutationFn: apiCall,
    onMutate: async (channelId) => {
      // Optimistic: flip the local ID set before the network round-trip
      // lands so the ♥ stays responsive on slow links.
      await queryClient.cancelQueries({ queryKey: queryKeys.channelFavoriteIDs });
      const previous = queryClient.getQueryData<string[]>(
        queryKeys.channelFavoriteIDs,
      );
      queryClient.setQueryData<string[]>(
        queryKeys.channelFavoriteIDs,
        (old) => apply(old ?? [], channelId),
      );
      return { previous };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.previous) {
        queryClient.setQueryData(queryKeys.channelFavoriteIDs, ctx.previous);
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.channelFavoriteIDs });
      queryClient.invalidateQueries({ queryKey: queryKeys.channelFavorites });
    },
  });
}

export function useAddChannelFavorite() {
  return useFavoriteMutation(
    (channelId) => api.addChannelFavorite(channelId),
    (ids, channelId) => (ids.includes(channelId) ? ids : [channelId, ...ids]),
  );
}

export function useRemoveChannelFavorite() {
  return useFavoriteMutation(
    (channelId) => api.removeChannelFavorite(channelId),
    (ids, channelId) => ids.filter((id) => id !== channelId),
  );
}

// ─── Bulk schedule (EPG payload for the Live TV grid) ──────────────────────

export function useBulkSchedule(
  channelIds: string[],
  options?: Partial<UseQueryOptions<Record<string, EPGProgram[]>>>,
) {
  // Sort the id list before hashing it so cache hits work regardless of
  // channel ordering. The previous implementation sliced to the first 10
  // which caused stale cache hits on libraries larger than 10 channels.
  const cacheKey = [...channelIds].sort().join(",");
  return useQuery<Record<string, EPGProgram[]>>({
    queryKey: ["bulk-schedule", cacheKey] as const,
    queryFn: () => api.getBulkSchedule(channelIds),
    enabled: channelIds.length > 0,
    staleTime: 2 * 60 * 1000,
    refetchInterval: 5 * 60 * 1000,
    ...options,
  });
}

// ─── Continue Watching (livetv rail) ───────────────────────────────────
//
// The beacon fires from useLiveHls on first play. The rail on Discover
// polls a short cache so a user who watches a channel on device A sees
// it update at the top of the rail on device B within the staleTime
// window — useful for the "same household, different TVs" case.

export function useContinueWatchingChannels(
  limit?: number,
  options?: Partial<UseQueryOptions<ContinueWatchingChannel[]>>,
) {
  return useQuery<ContinueWatchingChannel[]>({
    queryKey: queryKeys.continueWatchingChannels,
    queryFn: () => api.listContinueWatchingChannels(limit),
    // Short stale time so the rail stays fresh without polling: the
    // beacon invalidation below is the primary freshness driver.
    staleTime: 60_000,
    ...options,
  });
}

export function useRecordChannelWatch() {
  const queryClient = useQueryClient();
  return useMutation<
    { channel_id: string; last_watched_at: string },
    Error,
    string
  >({
    mutationFn: (channelId) => api.recordChannelWatch(channelId),
    onSuccess: () => {
      // The rail shifts: freshly-watched channel jumps to the top.
      // Invalidate so the next Discover render pulls the updated list.
      queryClient.invalidateQueries({
        queryKey: queryKeys.continueWatchingChannels,
      });
    },
    // Beacon failures are non-fatal UX events. Let the caller swallow
    // them silently — the rail just won't update this time around.
  });
}
