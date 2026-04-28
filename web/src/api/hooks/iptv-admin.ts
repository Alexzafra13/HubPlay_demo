// Manual M3U/EPG refresh + the public-IPTV catalogue import flow.
//
// The other two IPTV admin sub-domains live next door:
//   - iptv-sources.ts — EPG source CRUD + the static catalogue
//   - iptv-jobs.ts    — scheduled M3U/EPG refresh jobs
//
// All mutations here are admin-gated at the route level on the
// backend; this file is just the client-side query/invalidate
// plumbing. They live separately from `media.ts` because filesystem
// scan doesn't apply to livetv libraries — these hit /iptv/refresh-*
// endpoints and invalidate the channel + EPG caches so the Live TV
// page reflects the refresh without a page reload.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type { ImportPublicIPTVResponse, PublicCountry } from "../types";

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
