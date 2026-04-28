// EPG source CRUD + the static catalogue.
//
// The catalog is data shipped with the binary — fetch once, cache
// forever. The per-library source list mutates on add/remove/reorder;
// each mutation invalidates surgically so the admin UI updates without
// a full refetch.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type {
  AddEPGSourceRequest,
  LibraryEPGSource,
  PublicEPGSource,
} from "../types";

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
