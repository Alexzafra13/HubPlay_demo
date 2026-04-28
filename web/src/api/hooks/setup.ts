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
    ...options,
  });
}

export function useBrowseLibraryDirectories(
  path?: string,
  options?: Partial<UseQueryOptions<BrowseResponse>>,
) {
  return useQuery<BrowseResponse>({
    queryKey: ["browse-library", path] as const,
    queryFn: () => api.browseLibraryDirectories(path),
    ...options,
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
