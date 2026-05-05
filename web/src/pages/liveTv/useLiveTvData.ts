// useLiveTvData — pure data layer for the Live TV page.
//
// Why extract: LiveTV.tsx grew to ~560 LOC mixing 5 concerns (parallel
// fetch fan-out, URL-backed filter state, overlay player, favourites,
// hero spotlight). The fetch fan-out across N libraries is the most
// substantial slice — it owns four parallel queries (libraries,
// channels per library, unhealthy per library, bulk schedule) and the
// associated flatten + filter logic. Lifting it into its own hook
// gives the page a much smaller "data orchestration" surface and
// opens the door to a per-page test harness without booting the
// whole 560-line component.
//
// The hook is a *thin* wrapper over the existing query hooks — no
// caching beyond what TanStack Query already provides, no derived
// state beyond the flatten + filter the page used to do inline.

import { useMemo } from "react";
import { useQueries } from "@tanstack/react-query";

import {
  queryKeys,
  useBulkSchedule,
  useContinueWatchingChannels,
  useLibraries,
} from "@/api/hooks";
import { api } from "@/api/client";
import type {
  Channel,
  ChannelCategory,
  EPGProgram,
  UnhealthyChannel,
  Library,
} from "@/api/types";

export interface LiveTvData {
  /** All libraries the current user can see, narrowed to content_type=livetv. */
  liveTvLibraries: Library[];
  /** Active channels flattened across every livetv library. */
  channels: Channel[];
  /** True while at least one channels query is in flight. */
  channelsLoading: boolean;
  librariesLoading: boolean;
  /** Unhealthy channels surfaced separately for the "Apagados" rail. */
  unhealthyChannels: UnhealthyChannel[];
  /** Map of channelID → schedule row for the bulk-schedule call. */
  scheduleByChannel: Record<string, ChannelCategory[] | EPGProgram[] | undefined>;
  /** Per-user "continue watching" rail entries. */
  continueWatching: Channel[];
}

export function useLiveTvData(): LiveTvData {
  const { data: libraries, isLoading: librariesLoading } = useLibraries();

  // Every livetv library the current user can see. Channels from all
  // of them merge into a single pool for the Discover / Guide
  // surfaces — the admin can have multiple (one per country, one per
  // provider…) and the viewer shouldn't care which library a channel
  // came from.
  const liveTvLibraries = useMemo(
    () => (libraries ?? []).filter((l) => l.content_type === "livetv"),
    [libraries],
  );

  // Parallel channel fetches — one query per library. useQueries
  // returns the same shape as useQuery for each entry; we flatten
  // .data into a single Channel[]. Cache keys match useChannels so a
  // library scan invalidation hits both hooks.
  const channelQueries = useQueries({
    queries: liveTvLibraries.map((lib) => ({
      queryKey: queryKeys.channels(lib.id),
      queryFn: () => api.getChannels(lib.id),
    })),
  });
  const channelsLoading =
    liveTvLibraries.length > 0 && channelQueries.some((q) => q.isLoading);
  const rawChannels = useMemo<Channel[]>(
    () => channelQueries.flatMap((q) => q.data ?? []),
    [channelQueries],
  );

  // Inactive channels 404 on playback — hide them rather than leave
  // dead clicks in the mosaic.
  const channels = useMemo(
    () => rawChannels.filter((c) => c.is_active !== false),
    [rawChannels],
  );

  const channelIds = useMemo(() => channels.map((c) => c.id), [channels]);
  const { data: scheduleData } = useBulkSchedule(channelIds);
  const scheduleByChannel = useMemo(() => scheduleData ?? {}, [scheduleData]);

  // Unhealthy channels per library. The backend filters these out of
  // the main channel list (ListHealthyByLibrary) so Discover stays
  // clean, but we still want to surface them — dimmed — in a
  // dedicated "Apagados" rail so the viewer knows the channel exists
  // and the admin can tell at a glance what's currently off the air
  // without jumping to the admin page.
  const unhealthyQueries = useQueries({
    queries: liveTvLibraries.map((lib) => ({
      queryKey: queryKeys.unhealthyChannels(lib.id),
      queryFn: () => api.listUnhealthyChannels(lib.id),
    })),
  });
  const unhealthyChannels = useMemo<UnhealthyChannel[]>(
    () => unhealthyQueries.flatMap((q) => q.data ?? []),
    [unhealthyQueries],
  );

  // "Continuar viendo" rail — per-user, populated by the beacon the
  // ChannelPlayer fires on first play. The rail only shows up on the
  // "all" category tab; DiscoverView handles the gating.
  const { data: continueWatching = [] } = useContinueWatchingChannels();

  return {
    liveTvLibraries,
    channels,
    channelsLoading,
    librariesLoading,
    unhealthyChannels,
    scheduleByChannel,
    continueWatching,
  };
}
