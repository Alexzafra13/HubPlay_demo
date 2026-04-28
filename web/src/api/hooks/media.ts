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

export function useInfiniteItems(params?: {
  library_id?: string;
  type?: string;
  sort_by?: string;
  sort_order?: string;
}) {
  return useInfiniteQuery<PaginatedResponse<MediaItem>>({
    queryKey: ["items-infinite", params] as const,
    queryFn: ({ pageParam }) =>
      api.getItems({
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

export function useSearch(
  q: string,
  options?: Partial<UseQueryOptions<PaginatedResponse<MediaItem>>>,
) {
  return useQuery<PaginatedResponse<MediaItem>>({
    queryKey: queryKeys.search(q),
    queryFn: () => api.searchItems(q),
    enabled: q.length > 0,
    ...options,
  });
}

export function useLatestItems(
  libraryId?: string,
  options?: Partial<UseQueryOptions<MediaItem[]>>,
) {
  return useQuery<MediaItem[]>({
    queryKey: queryKeys.latestItems(libraryId),
    queryFn: () => api.getLatestItems(libraryId),
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
