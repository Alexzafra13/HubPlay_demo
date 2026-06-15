import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { MediaSourceType, TorrentSearchResult } from "@/api/types";

export interface UseMediaSourcesResult {
  /** Normalised, sorted sources (seeders → quality → size). */
  sources: TorrentSearchResult[];
  isLoading: boolean;
  isError: boolean;
  error: unknown;
  /** Re-run the query (bypasses the React Query cache). */
  refetch: () => void;
}

/**
 * useMediaSources fetches the streamable sources for a title by IMDb id
 * from the backend Torznab aggregator (/torrent/sources/{type}/{imdbId}).
 *
 * The query is disabled until both `type` and a non-empty `imdbId` are
 * present (so it's safe to call before the item's metadata has loaded),
 * and can be further gated with `options.enabled`. Results are kept fresh
 * for 5 minutes client-side on top of the server-side cache.
 */
export function useMediaSources(
  type: MediaSourceType | undefined,
  imdbId: string | null | undefined,
  options?: { enabled?: boolean },
): UseMediaSourcesResult {
  const enabled = Boolean(type && imdbId) && (options?.enabled ?? true);

  const query = useQuery({
    queryKey: ["media-sources", type, imdbId],
    queryFn: () =>
      api.getMediaSources(type as MediaSourceType, imdbId as string),
    enabled,
    retry: false,
    staleTime: 5 * 60_000,
    // Indexers can be rate-limited; don't re-hit them on tab focus/reconnect.
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

  return {
    sources: query.data ?? [],
    isLoading: query.isLoading && enabled,
    isError: query.isError,
    error: query.error,
    refetch: () => {
      void query.refetch();
    },
  };
}

/**
 * useDiscoverSources resolves the sources for a TMDb pick (by imdbid,
 * server-side) — the reliable, Torrentio-style path. Disabled until a
 * tmdbId is present.
 */
export function useDiscoverSources(
  type: MediaSourceType,
  tmdbId: string | null,
): UseMediaSourcesResult {
  const enabled = Boolean(tmdbId);
  const result = useQuery({
    queryKey: ["discover-sources", type, tmdbId],
    queryFn: () => api.discoverSources(type, tmdbId as string),
    enabled,
    retry: false,
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
  return {
    sources: result.data ?? [],
    isLoading: result.isLoading && enabled,
    isError: result.isError,
    error: result.error,
    refetch: () => {
      void result.refetch();
    },
  };
}

/**
 * useMediaSearch runs a free-text search against the configured indexers
 * (the general torrent search box) — /torrent/sources/search. Disabled
 * until a non-empty query is present.
 */
export function useMediaSearch(
  type: MediaSourceType,
  query: string,
): UseMediaSourcesResult {
  const q = query.trim();
  const enabled = q.length > 0;

  const result = useQuery({
    queryKey: ["media-search", type, q],
    queryFn: () => api.searchMediaSources(type, q),
    enabled,
    retry: false,
    staleTime: 5 * 60_000,
    // Indexers can be rate-limited; don't re-hit them on tab focus/reconnect.
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

  return {
    sources: result.data ?? [],
    isLoading: result.isLoading && enabled,
    isError: result.isError,
    error: result.error,
    refetch: () => {
      void result.refetch();
    },
  };
}
