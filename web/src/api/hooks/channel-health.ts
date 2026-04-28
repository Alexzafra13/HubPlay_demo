// Channel health admin surface: the unhealthy list, the orphans-without-
// EPG list, and the actions that mutate either (reset / disable / enable
// / patch tvg_id).
//
// Mutations invalidate both the health list AND the regular channel
// list because a freshly-enabled channel reappears there and a disabled
// one vanishes.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type {
  ChannelWithoutEPG,
  PatchChannelRequest,
  UnhealthyChannel,
} from "../types";

export function useUnhealthyChannels(
  libraryId: string,
  options?: Partial<UseQueryOptions<UnhealthyChannel[]>>,
) {
  return useQuery<UnhealthyChannel[]>({
    queryKey: queryKeys.unhealthyChannels(libraryId),
    queryFn: () => api.listUnhealthyChannels(libraryId),
    enabled: !!libraryId,
    // No polling: admin pages mount useEventStream("channel.health.changed")
    // which invalidates this query on the SSE push. The 30s background
    // refetch we ran before turned into a steady stream of empty
    // responses — push is cheaper and more responsive.
    ...options,
  });
}

export function useResetChannelHealth(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (channelId) => api.resetChannelHealth(channelId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: queryKeys.unhealthyChannels(libraryId),
      });
      queryClient.invalidateQueries({ queryKey: queryKeys.channels(libraryId) });
    },
  });
}

export function useDisableChannel(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (channelId) => api.disableChannel(channelId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: queryKeys.unhealthyChannels(libraryId),
      });
      queryClient.invalidateQueries({ queryKey: queryKeys.channels(libraryId) });
    },
  });
}

export function useEnableChannel(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (channelId) => api.enableChannel(channelId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: queryKeys.unhealthyChannels(libraryId),
      });
      queryClient.invalidateQueries({ queryKey: queryKeys.channels(libraryId) });
    },
  });
}

// ─── Channels without EPG ───────────────────────────────────────────────
//
// Admin surface that pairs with the "canales sin guía" panel. The list
// comes from the backend filtered to active channels with no programmes
// in the 24h window. Editing `tvg_id` via PATCH invalidates both this
// list and the library channel list so the UI refreshes right away
// (the orphan disappears once the next EPG refresh matches it).

export function useChannelsWithoutEPG(
  libraryId: string,
  options?: Partial<UseQueryOptions<ChannelWithoutEPG[]>>,
) {
  return useQuery<ChannelWithoutEPG[]>({
    queryKey: queryKeys.channelsWithoutEPG(libraryId),
    queryFn: () => api.listChannelsWithoutEPG(libraryId),
    enabled: !!libraryId,
    ...options,
  });
}

export function usePatchChannel(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<
    ChannelWithoutEPG,
    Error,
    { channelId: string; patch: PatchChannelRequest }
  >({
    mutationFn: ({ channelId, patch }) => api.patchChannel(channelId, patch),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: queryKeys.channelsWithoutEPG(libraryId),
      });
      queryClient.invalidateQueries({ queryKey: queryKeys.channels(libraryId) });
    },
  });
}
