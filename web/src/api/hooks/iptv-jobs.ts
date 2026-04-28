// IPTV scheduled jobs (automated M3U + EPG refresh).
//
// Read is gated by library ACL; all mutations are admin-only at the
// route level. The list endpoint always returns both kinds (m3u_refresh
// + epg_refresh) — persisted rows when they exist, placeholders
// otherwise — so the UI can render a stable two-row table without
// branching.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type {
  IPTVScheduledJob,
  IPTVScheduledJobKind,
  UpsertScheduledJobRequest,
} from "../types";

export function useScheduledJobs(
  libraryId: string,
  options?: Partial<UseQueryOptions<IPTVScheduledJob[]>>,
) {
  return useQuery<IPTVScheduledJob[]>({
    queryKey: queryKeys.scheduledJobs(libraryId),
    queryFn: () => api.listScheduledJobs(libraryId),
    enabled: !!libraryId,
    ...options,
  });
}

export function useUpsertScheduledJob(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<
    IPTVScheduledJob,
    Error,
    { kind: IPTVScheduledJobKind; data: UpsertScheduledJobRequest }
  >({
    mutationFn: ({ kind, data }) =>
      api.upsertScheduledJob(libraryId, kind, data),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: queryKeys.scheduledJobs(libraryId),
      });
    },
  });
}

export function useDeleteScheduledJob(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<void, Error, IPTVScheduledJobKind>({
    mutationFn: (kind) => api.deleteScheduledJob(libraryId, kind),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: queryKeys.scheduledJobs(libraryId),
      });
    },
  });
}

export function useRunScheduledJobNow(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<IPTVScheduledJob | null, Error, IPTVScheduledJobKind>({
    mutationFn: (kind) => api.runScheduledJobNow(libraryId, kind),
    onSuccess: (_data, kind) => {
      // Always invalidate the schedule list so last_run_at refreshes.
      queryClient.invalidateQueries({
        queryKey: queryKeys.scheduledJobs(libraryId),
      });
      // A manual run does the same work as the scheduled one, so
      // piggy-back the cache invalidations from useRefreshM3U /
      // useRefreshEPG — otherwise the UI would show stale data after
      // "Run now" until the user navigates away and back.
      if (kind === "m3u_refresh") {
        queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
        queryClient.invalidateQueries({ queryKey: queryKeys.library(libraryId) });
        queryClient.invalidateQueries({
          queryKey: queryKeys.channels(libraryId),
        });
        queryClient.invalidateQueries({ queryKey: ["bulk-schedule"] });
      } else if (kind === "epg_refresh") {
        queryClient.invalidateQueries({ queryKey: ["bulk-schedule"] });
        queryClient.invalidateQueries({ queryKey: ["channels"] });
        queryClient.invalidateQueries({
          queryKey: queryKeys.libraryEPGSources(libraryId),
        });
        queryClient.invalidateQueries({
          queryKey: queryKeys.channelsWithoutEPG(libraryId),
        });
      }
    },
  });
}
