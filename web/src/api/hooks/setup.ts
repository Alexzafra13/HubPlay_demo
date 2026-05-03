// Setup wizard hooks. Used only during the first-run experience —
// once setupStatus reports completion, none of these run again.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import { api } from "../client";
import { queryKeys } from "../queryKeys";
import type {
  AuthResponse,
  BrowseResponse,
  Library,
  SetupStatus,
  SystemCapabilities,
} from "../types";

export function useSetupStatus(options?: Partial<UseQueryOptions<SetupStatus>>) {
  return useQuery<SetupStatus>({
    queryKey: queryKeys.setupStatus,
    queryFn: () => api.getSetupStatus(),
    ...options,
  });
}

export function useSystemCapabilities(
  options?: Partial<UseQueryOptions<SystemCapabilities>>,
) {
  return useQuery<SystemCapabilities>({
    queryKey: queryKeys.systemCapabilities,
    queryFn: () => api.getSystemCapabilities(),
    ...options,
  });
}

export function useBrowseDirectories(
  path?: string,
  options?: Partial<UseQueryOptions<BrowseResponse>>,
) {
  return useQuery<BrowseResponse>({
    queryKey: queryKeys.browseDirectories(path),
    queryFn: () => api.browseDirectories(path),
    // Keep the previously rendered listing visible while the next
    // path's response is in flight — readdir over the Windows-Docker
    // bind-mount can take 1-2s for large directories, and going
    // briefly blank in between makes the modal feel broken. With
    // placeholderData the user keeps seeing the previous listing
    // (greyed out via isFetching) until the new one lands.
    placeholderData: (prev) => prev,
    // Listings are immutable from the client's perspective for the
    // duration of the modal. Cache for the session so navigating
    // back to a previously visited path is instant.
    staleTime: 5 * 60 * 1000,
    ...options,
  });
}

export function useBrowseLibraryDirectories(
  path?: string,
  options?: Partial<UseQueryOptions<BrowseResponse>>,
) {
  return useQuery<BrowseResponse>({
    queryKey: queryKeys.browseLibraryDirectories(path),
    queryFn: () => api.browseLibraryDirectories(path),
    placeholderData: (prev) => prev,
    staleTime: 5 * 60 * 1000,
    ...options,
  });
}

// usePrefetchBrowseLibraryDirectories warms the TanStack cache for the
// admin folder picker so the modal renders against an already-resolved
// listing instead of starting from a cold spinner. Callers fire this
// when the parent ("Add library" / "Edit library" modal) opens — by
// the time the user pulls up the folder picker the network round-trip
// is already done. Same staleTime as the live query so a warm cache
// is reused, not refetched.
export function usePrefetchBrowseLibraryDirectories() {
  const queryClient = useQueryClient();
  return (path?: string) =>
    queryClient.prefetchQuery({
      queryKey: queryKeys.browseLibraryDirectories(path),
      queryFn: () => api.browseLibraryDirectories(path),
      staleTime: 5 * 60 * 1000,
    });
}

export function useSetupCreateAdmin() {
  const queryClient = useQueryClient();
  return useMutation<
    AuthResponse,
    Error,
    { username: string; password: string; display_name?: string }
  >({
    mutationFn: ({ username, password, display_name }) =>
      api.setupCreateAdmin(username, password, display_name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.setupStatus });
      queryClient.invalidateQueries({ queryKey: queryKeys.me });
    },
  });
}

export function useSetupCreateLibraries() {
  const queryClient = useQueryClient();
  return useMutation<
    Library[],
    Error,
    Array<{ name: string; content_type: string; paths: string[] }>
  >({
    mutationFn: (libraries) => api.setupCreateLibraries(libraries),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
    },
  });
}

export function useSetupSettings() {
  return useMutation<void, Error, Record<string, unknown>>({
    mutationFn: (settings) => api.setupSettings(settings),
  });
}

export function useSetupComplete() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, boolean | undefined>({
    mutationFn: (startScan) => api.setupComplete(startScan),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.setupStatus });
      queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
    },
  });
}
