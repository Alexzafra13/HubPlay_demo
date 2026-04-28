// IPTV admin surface: public catalog imports, EPG sources, scheduled
// jobs, and the M3U/EPG refresh mutations.
//
// All mutations are admin-gated at the route level on the backend;
// this file is just the client-side query/invalidate plumbing.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type {
  AddEPGSourceRequest,
  ImportPublicIPTVResponse,
  IPTVScheduledJob,
  IPTVScheduledJobKind,
  LibraryEPGSource,
  PublicCountry,
  PublicEPGSource,
  UpsertScheduledJobRequest,
} from "../types";

// ─── Public catalog import ────────────────────────────────────────────────

export function usePublicCountries(
  options?: Partial<UseQueryOptions<PublicCountry[]>>,
) {
  return useQuery<PublicCountry[]>({
    queryKey: queryKeys.publicCountries,
    queryFn: () => api.getPublicCountries(),
    ...options,
  });
}

export function useImportPublicIPTV() {
  const queryClient = useQueryClient();
  return useMutation<
    ImportPublicIPTVResponse,
    Error,
    { country: string; name?: string }
  >({
    mutationFn: ({ country, name }) => api.importPublicIPTV(country, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
    },
  });
}

// ─── EPG Sources ────────────────────────────────────────────────────────
//
// The catalog is static data shipped with the binary — fetch once, cache
// forever. The per-library source list mutates on add/remove/reorder; we
// invalidate surgically so the admin UI updates without a full refetch.

export function useEPGCatalog(
  options?: Partial<UseQueryOptions<PublicEPGSource[]>>,
) {
  return useQuery<PublicEPGSource[]>({
    queryKey: queryKeys.epgCatalog,
    queryFn: () => api.getEPGCatalog(),
    staleTime: Infinity, // Catalog is static per-binary; no need to refetch.
    ...options,
  });
}

export function useLibraryEPGSources(
  libraryId: string,
  options?: Partial<UseQueryOptions<LibraryEPGSource[]>>,
) {
  return useQuery<LibraryEPGSource[]>({
    queryKey: queryKeys.libraryEPGSources(libraryId),
    queryFn: () => api.listEPGSources(libraryId),
    enabled: !!libraryId,
    ...options,
  });
}

export function useAddEPGSource(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<LibraryEPGSource, Error, AddEPGSourceRequest>({
    mutationFn: (req) => api.addEPGSource(libraryId, req),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: queryKeys.libraryEPGSources(libraryId),
      });
    },
  });
}

export function useRemoveEPGSource(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (sourceId) => api.removeEPGSource(libraryId, sourceId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: queryKeys.libraryEPGSources(libraryId),
      });
    },
  });
}

export function useReorderEPGSources(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<LibraryEPGSource[], Error, string[]>({
    mutationFn: (sourceIds) => api.reorderEPGSources(libraryId, sourceIds),
    onSuccess: (data) => {
      // The reorder endpoint returns the new list, so we can seed the
      // cache directly and skip an unnecessary round-trip.
      queryClient.setQueryData(queryKeys.libraryEPGSources(libraryId), data);
    },
  });
}

// ─── IPTV Scheduled Jobs ────────────────────────────────────────────────
//
// Automated refreshes (M3U + EPG). Read is gated by library ACL; all
// mutations are admin-only at the route level. The list endpoint
// always returns both kinds — persisted rows when they exist,
// placeholders otherwise — so the UI can render a stable two-row
// table without branching.

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

// ─── M3U / EPG manual refresh ──────────────────────────────────────────
//
// Separate from `useScanLibrary` because filesystem scan doesn't apply
// to livetv libraries — these hit /iptv/refresh-m3u and /iptv/refresh-
// epg instead, and invalidate the channel + EPG caches so the Live TV
// page reflects the refresh without a page reload.

export function useRefreshM3U() {
  const queryClient = useQueryClient();
  return useMutation<{ channels_imported: number }, Error, string>({
    mutationFn: (libraryId) => api.refreshM3U(libraryId),
    onSuccess: (_data, libraryId) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
      queryClient.invalidateQueries({ queryKey: queryKeys.library(libraryId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.channels(libraryId) });
      queryClient.invalidateQueries({ queryKey: ["bulk-schedule"] });
    },
  });
}

export function useRefreshEPG() {
  const queryClient = useQueryClient();
  return useMutation<{ programs_imported: number }, Error, string>({
    mutationFn: (libraryId) => api.refreshEPG(libraryId),
    onSuccess: (_data, libraryId) => {
      // EPG refresh touches every channel's schedule; just invalidate the
      // whole bulk-schedule keyspace rather than cherry-picking.
      queryClient.invalidateQueries({ queryKey: ["bulk-schedule"] });
      queryClient.invalidateQueries({ queryKey: ["channels"] });
      // Per-source status (last_refreshed_at / last_status / program count)
      // is written by the backend during the refresh — the admin panel
      // otherwise stays on "nunca refrescada" until a manual reload.
      queryClient.invalidateQueries({
        queryKey: queryKeys.libraryEPGSources(libraryId),
      });
      // A successful refresh may have turned orphans into matched
      // channels; refresh the "canales sin guía" list too.
      queryClient.invalidateQueries({
        queryKey: queryKeys.channelsWithoutEPG(libraryId),
      });
    },
  });
}
