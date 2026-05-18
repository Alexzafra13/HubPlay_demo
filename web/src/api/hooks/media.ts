// Media browsing + library mutations.
//
// The query side of the media domain (libraries, items, search,
// continue-watching, next-up, favourites) and the library mutation
// side (create / update / delete / scan) live together because the
// invalidations cross-talk: a `useScanLibrary` success has to invalidate
// both the library detail and the libraries list, and a
// `useCreateLibrary` has to invalidate the libraries list before the
// admin UI re-renders.

import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type {
  CreateLibraryRequest,
  ItemDetail,
  Library,
  MediaItem,
  PaginatedResponse,
  PersonDetail,
  UpdateLibraryRequest,
} from "../types";

// ─── Queries ───────────────────────────────────────────────────────────────

export function useLibraries(options?: Partial<UseQueryOptions<Library[]>>) {
  return useQuery<Library[]>({
    queryKey: queryKeys.libraries,
    queryFn: () => api.getLibraries(),
    ...options,
  });
}

export function useLibrary(
  id: string,
  options?: Partial<UseQueryOptions<Library>>,
) {
  return useQuery<Library>({
    queryKey: queryKeys.library(id),
    queryFn: () => api.getLibrary(id),
    enabled: !!id,
    ...options,
  });
}

export function useItems(
  params?: {
    library_id?: string;
    type?: string;
    genre?: string;
    sort_by?: string;
    sort_order?: string;
    offset?: number;
    limit?: number;
  },
  options?: Partial<UseQueryOptions<PaginatedResponse<MediaItem>>>,
) {
  return useQuery<PaginatedResponse<MediaItem>>({
    queryKey: queryKeys.items(params as Record<string, unknown>),
    queryFn: () => api.getItems(params),
    ...options,
  });
}

const PAGE_SIZE = 40;

export interface InfiniteItemsParams {
  library_id?: string;
  type?: string;
  sort_by?: string;
  sort_order?: string;
  /** Full-text query — when set, the backend FTS-keys the result. */
  q?: string;
  /** Single genre name (case-insensitive). Multi-genre is not supported server-side yet. */
  genre?: string;
  year_from?: number;
  year_to?: number;
  /** 0..10. 0 disables. */
  min_rating?: number;
}

export function useInfiniteItems(params?: InfiniteItemsParams) {
  return useInfiniteQuery<PaginatedResponse<MediaItem>>({
    queryKey: ["items-infinite", params] as const,
    queryFn: ({ pageParam }) =>
      api.getItems({
        // Server-side sort defaults to sort_title ASC. Pass an explicit
        // value when the caller asked for a different one — keeps the
        // default behaviour alphabetical (movies/series browse) without
        // forcing every callsite to repeat it.
        sort_by: "sort_title",
        sort_order: "asc",
        ...params,
        offset: (pageParam as number) * PAGE_SIZE,
        limit: PAGE_SIZE,
      }),
    initialPageParam: 0,
    getNextPageParam: (lastPage, _allPages, lastPageParam) => {
      const loaded = ((lastPageParam as number) + 1) * PAGE_SIZE;
      return loaded < lastPage.total ? (lastPageParam as number) + 1 : undefined;
    },
  });
}

// Catalogue-wide genre vocabulary for the filter panel.
//
// The legacy panel derived genres from already-loaded items, which
// silently broke once /items got paginated: a 100-movie library
// would only ever show genres for the first 40. The hook hits a
// dedicated endpoint that aggregates over the entire catalogue, so
// the chip list is correct on first paint.
export function useGenres(itemType?: string) {
  return useQuery<{ name: string; count: number }[]>({
    queryKey: queryKeys.itemGenres(itemType),
    queryFn: () => api.getGenres(itemType),
    // Genre vocabulary changes only when content is added/removed.
    // 5 min staleness covers a normal browsing session without
    // refetching mid-scroll.
    staleTime: 5 * 60_000,
  });
}

export function useItem(
  id: string,
  options?: Partial<UseQueryOptions<ItemDetail>>,
) {
  return useQuery<ItemDetail>({
    queryKey: queryKeys.item(id),
    queryFn: () => api.getItem(id),
    enabled: !!id,
    ...options,
  });
}

export function useItemChildren(
  id: string,
  options?: Partial<UseQueryOptions<MediaItem[]>>,
) {
  return useQuery<MediaItem[]>({
    queryKey: queryKeys.itemChildren(id),
    queryFn: () => api.getItemChildren(id),
    enabled: !!id,
    ...options,
  });
}

export function useItemRecommendations(id: string) {
  return useQuery<{ items: import("../types").Recommendation[] }>({
    queryKey: queryKeys.itemRecommendations(id),
    queryFn: () => api.getItemRecommendations(id),
    enabled: !!id,
    // TMDb recommendations don't change minute-to-minute. 10-minute
    // staleness covers a session of bouncing between detail pages
    // without re-querying TMDb (the backend round-trip is what costs).
    staleTime: 10 * 60_000,
    // 503 (no provider configured) and 5xx are non-fatal — the rail
    // just hides itself. No point in auto-retrying mid-session.
    retry: false,
  });
}

// Identify (admin rematch). Disabled hasta que el modal entra en su
// estado "searching" — la query trae candidatos TMDb. Sin `enabled`
// gate la lista TMDb saltaría en cuanto el menú del detalle se abre.
export function useIdentifyCandidates(
  id: string,
  options: { query?: string; year?: number; enabled?: boolean } = {},
) {
  const { query, year, enabled } = options;
  return useQuery<import("../types").IdentifyCandidate[]>({
    queryKey: ["items", id, "identify", "candidates", { query, year }] as const,
    queryFn: () => api.getIdentifyCandidates(id, { query, year }),
    enabled: !!id && (enabled ?? false),
    // Una búsqueda en TMDb dura segundos. No la re-disparamos por
    // window focus — sería ruidoso y costoso.
    refetchOnWindowFocus: false,
    retry: false,
  });
}

export function useApplyIdentify(itemId: string) {
  const queryClient = useQueryClient();
  return useMutation<
    { item_id: string; provider: string; external_id: string },
    Error,
    { provider?: string; external_id: string }
  >({
    mutationFn: (payload) => api.applyIdentify(itemId, payload),
    onSuccess: () => {
      // Hard refresh: el item entero, sus imágenes, y los recommendations
      // (la rail de "more like this" cuelga del tmdb id que acabamos de
      // cambiar). Invalidamos también listados que muestran el título —
      // página de biblioteca, latest, búsquedas — pero no inventamos un
      // tag genérico: con item + listados de items + images cubrimos los
      // sitios donde el cambio se ve.
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.itemImages(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.itemRecommendations(itemId) });
      queryClient.invalidateQueries({ queryKey: ["items"] });
    },
  });
}

export function usePerson(
  id: string,
  options?: Partial<UseQueryOptions<PersonDetail>>,
) {
  return useQuery<PersonDetail>({
    queryKey: queryKeys.person(id),
    queryFn: () => api.getPerson(id),
    enabled: !!id,
    ...options,
  });
}

// Studios browse + detail. The detail page (/studios/{slug}) is one
// of the click-through targets from the studio mark on a movie /
// series detail page; cache 5 min so revisits inside a session feel
// instant without going stale on a fresh scan.
export function useStudios(
  options?: Partial<UseQueryOptions<{ studios: import("@/api/types").StudioListEntry[] }>>,
) {
  return useQuery<{ studios: import("@/api/types").StudioListEntry[] }>({
    queryKey: queryKeys.studios,
    queryFn: () => api.getStudios(),
    staleTime: 5 * 60_000,
    ...options,
  });
}

export function useStudio(
  slug: string,
  options?: Partial<UseQueryOptions<import("@/api/types").StudioDetail>>,
) {
  return useQuery<import("@/api/types").StudioDetail>({
    queryKey: queryKeys.studio(slug),
    queryFn: () => api.getStudio(slug),
    enabled: !!slug,
    staleTime: 5 * 60_000,
    ...options,
  });
}

// Movie-collection (saga) browse + detail. Same 5-min cache so
// jumping between a movie page and its parent collection feels
// instant. The detail endpoint accepts the canonical
// "collection:<tmdb_id>" id directly so no slug encoding is needed.
export function useCollections(
  options?: Partial<
    UseQueryOptions<{ collections: import("@/api/types").CollectionListEntry[] }>
  >,
) {
  return useQuery<{ collections: import("@/api/types").CollectionListEntry[] }>({
    queryKey: queryKeys.collections,
    queryFn: () => api.getCollections(),
    staleTime: 5 * 60_000,
    ...options,
  });
}

export function useCollection(
  id: string,
  options?: Partial<UseQueryOptions<import("@/api/types").CollectionDetail>>,
) {
  return useQuery<import("@/api/types").CollectionDetail>({
    queryKey: queryKeys.collection(id),
    queryFn: () => api.getCollection(id),
    enabled: !!id,
    staleTime: 5 * 60_000,
    ...options,
  });
}

export function useSearch(
  q: string,
  options?: Partial<UseQueryOptions<MediaItem[]>>,
) {
  return useQuery<MediaItem[]>({
    queryKey: queryKeys.search(q),
    queryFn: () => api.searchItems(q),
    enabled: q.length > 0,
    ...options,
  });
}

export function useLatestItems(
  libraryId?: string,
  options?: Partial<UseQueryOptions<MediaItem[]>> & {
    type?: "movie" | "series" | "season" | "episode";
  },
) {
  // The "Reciente en <library>" rail on a shows library uses this
  // hook with type="series" so the SQL query already filters out
  // episodes (which dominate `added_at DESC` because new episodes are
  // the most common write to the library, leaving the rail with
  // almost no series-row hits otherwise).
  const type = options?.type;
  return useQuery<MediaItem[]>({
    queryKey: type
      ? [...queryKeys.latestItems(libraryId), type]
      : queryKeys.latestItems(libraryId),
    queryFn: () => api.getLatestItems(libraryId, undefined, type),
    ...options,
  });
}

export function useContinueWatching(
  options?: Partial<UseQueryOptions<MediaItem[]>>,
) {
  return useQuery<MediaItem[]>({
    queryKey: queryKeys.continueWatching,
    queryFn: () => api.getContinueWatching(),
    ...options,
  });
}

export function useNextUp(options?: Partial<UseQueryOptions<MediaItem[]>>) {
  return useQuery<MediaItem[]>({
    queryKey: queryKeys.nextUp,
    queryFn: () => api.getNextUp(),
    ...options,
  });
}

export function useFavorites(options?: Partial<UseQueryOptions<MediaItem[]>>) {
  return useQuery<MediaItem[]>({
    queryKey: queryKeys.favorites,
    queryFn: () => api.getFavorites(),
    ...options,
  });
}

// ─── Library mutations ─────────────────────────────────────────────────────

export function useCreateLibrary() {
  const queryClient = useQueryClient();
  return useMutation<Library, Error, CreateLibraryRequest>({
    mutationFn: (data) => api.createLibrary(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
    },
  });
}

export function useUpdateLibrary() {
  const queryClient = useQueryClient();
  return useMutation<Library, Error, { id: string; data: UpdateLibraryRequest }>({
    mutationFn: ({ id, data }) => api.updateLibrary(id, data),
    onSuccess: (_data, { id }) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.library(id) });
      queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
    },
  });
}

export function useDeleteLibrary() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (id) => api.deleteLibrary(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
    },
  });
}

export function useScanLibrary() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { id: string; refreshMetadata?: boolean }>({
    mutationFn: ({ id, refreshMetadata }) => api.scanLibrary(id, refreshMetadata),
    onSuccess: (_data, { id }) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.library(id) });
      queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
    },
  });
}
