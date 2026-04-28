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
import type {
  ImportPublicIPTVResponse,
  PreflightResult,
  PublicCountry,
} from "../types";

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

/**
 * Probe an M3U URL for reachability + format BEFORE saving the
 * library. Returns within ~12 s with a verdict. Used by the
 * "Test connection" button in the library Add/Edit modals.
 *
 * Not invalidating any cache — preflight is read-only and should
 * never affect what the rest of the app sees.
 */
export function usePreflightM3U() {
  return useMutation<
    PreflightResult,
    Error,
    { m3u_url: string; tls_insecure: boolean }
  >({
    mutationFn: (input) => api.preflightM3U(input),
  });
}

// REFRESH_M3U_TIMEOUT_MS matches the backend's detached import budget
// (refreshM3UAsyncTimeout in iptv_admin.go). If the SSE event never
// arrives — server crash mid-import, SSE proxy dropping the stream —
// the mutation rejects with a timeout instead of pinning the spinner
// forever. Slightly looser than the backend so a clean failure event
// wins the race against the local timeout.
const REFRESH_M3U_TIMEOUT_MS = 11 * 60 * 1000;

/**
 * useRefreshM3U — kicks off an async M3U import and resolves when the
 * server publishes the matching SSE completion event.
 *
 * The endpoint returns 202 Accepted; the import runs on a detached
 * server-side context because realistic M3U_PLUS feeds (~98k lines)
 * exceed any reasonable HTTP timeout. The mutation stays `isPending`
 * for the full import — that drives the per-library spinner in
 * LibraryCard, which would otherwise flash off after the 202 and
 * leave the operator wondering whether anything is happening.
 *
 * Each call opens its own EventSource scoped to a single library and
 * tears it down on resolve / reject / timeout. We don't share a
 * singleton stream because parallel imports across libraries are
 * rare, and an isolated stream per call keeps the failure handling
 * trivial.
 */
export function useRefreshM3U() {
  const queryClient = useQueryClient();
  return useMutation<{ channels_imported: number }, Error, string>({
    mutationFn: async (libraryId) => {
      // Open the SSE stream BEFORE issuing the POST so we don't miss
      // the completion event for a fast import (small playlist that
      // finishes between the 202 returning and the listener
      // attaching).
      const source = new EventSource("/api/v1/events", {
        withCredentials: true,
      });

      try {
        return await new Promise<{ channels_imported: number }>(
          (resolve, reject) => {
            let settled = false;
            const cleanup = () => {
              settled = true;
              clearTimeout(timer);
              source.removeEventListener(
                "playlist.refreshed",
                onRefreshed,
              );
              source.removeEventListener(
                "playlist.refresh_failed",
                onFailed,
              );
              source.close();
            };

            const timer = setTimeout(() => {
              if (settled) return;
              cleanup();
              reject(
                new Error(
                  "M3U refresh timed out — check server logs for details.",
                ),
              );
            }, REFRESH_M3U_TIMEOUT_MS);

            const onRefreshed = (e: MessageEvent) => {
              if (settled) return;
              try {
                const evt = JSON.parse(e.data) as {
                  data?: { library_id?: string; channels_count?: number };
                };
                if (evt.data?.library_id !== libraryId) return;
                cleanup();
                resolve({ channels_imported: evt.data.channels_count ?? 0 });
              } catch {
                /* malformed event payload — ignore and keep waiting */
              }
            };

            const onFailed = (e: MessageEvent) => {
              if (settled) return;
              try {
                const evt = JSON.parse(e.data) as {
                  data?: { library_id?: string; error?: string };
                };
                if (evt.data?.library_id !== libraryId) return;
                cleanup();
                reject(
                  new Error(evt.data?.error ?? "M3U refresh failed"),
                );
              } catch {
                /* malformed event payload — ignore and keep waiting */
              }
            };

            source.addEventListener("playlist.refreshed", onRefreshed);
            source.addEventListener("playlist.refresh_failed", onFailed);

            // Fire the POST after both listeners are attached. If
            // the 202 itself fails (auth, 409 already-running, ...)
            // we reject early and clean up.
            api.refreshM3U(libraryId).catch((err) => {
              if (settled) return;
              cleanup();
              reject(err);
            });
          },
        );
      } finally {
        // Defensive: the resolve/reject paths already close the
        // source via cleanup(), but a thrown synchronous error
        // before the promise is constructed would leak the stream.
        if (source.readyState !== EventSource.CLOSED) {
          source.close();
        }
      }
    },
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
