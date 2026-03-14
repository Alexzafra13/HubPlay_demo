import {
  useQuery,
  useMutation,
  useQueryClient,
  type UseQueryOptions,
} from "@tanstack/react-query";
import { api } from "./client";
import type {
  AuthResponse,
  BrowseResponse,
  Channel,
  CreateLibraryRequest,
  HealthResponse,
  ItemDetail,
  Library,
  MediaItem,
  PaginatedResponse,
  SetupStatus,
  SystemCapabilities,
  UpdateLibraryRequest,
  User,
  UserData,
} from "./types";

// ─── Query Keys ─────────────────────────────────────────────────────────────

export const queryKeys = {
  me: ["me"] as const,
  users: ["users"] as const,
  libraries: ["libraries"] as const,
  library: (id: string) => ["libraries", id] as const,
  items: (params?: Record<string, unknown>) => ["items", params] as const,
  item: (id: string) => ["items", id] as const,
  itemChildren: (id: string) => ["items", id, "children"] as const,
  search: (q: string) => ["search", q] as const,
  latestItems: (libraryId?: string) => ["items", "latest", libraryId] as const,
  continueWatching: ["continue-watching"] as const,
  nextUp: ["next-up"] as const,
  favorites: ["favorites"] as const,
  channels: (libraryId?: string) => ["channels", libraryId] as const,
  channel: (id: string) => ["channels", id] as const,
  channelSchedule: (id: string) => ["channels", id, "schedule"] as const,
  channelGroups: (libraryId?: string) => ["channels", "groups", libraryId] as const,
  health: ["health"] as const,
  setupStatus: ["setup-status"] as const,
  systemCapabilities: ["system-capabilities"] as const,
  browseDirectories: (path?: string) => ["browse", path] as const,
  progress: (itemId: string) => ["progress", itemId] as const,
} as const;

// ─── Query Hooks ────────────────────────────────────────────────────────────

export function useMe(
  options?: Partial<UseQueryOptions<User>>,
) {
  return useQuery<User>({
    queryKey: queryKeys.me,
    queryFn: () => api.getMe(),
    ...options,
  });
}

export function useLibraries(
  options?: Partial<UseQueryOptions<Library[]>>,
) {
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

export function useNextUp(
  options?: Partial<UseQueryOptions<MediaItem[]>>,
) {
  return useQuery<MediaItem[]>({
    queryKey: queryKeys.nextUp,
    queryFn: () => api.getNextUp(),
    ...options,
  });
}

export function useFavorites(
  options?: Partial<UseQueryOptions<MediaItem[]>>,
) {
  return useQuery<MediaItem[]>({
    queryKey: queryKeys.favorites,
    queryFn: () => api.getFavorites(),
    ...options,
  });
}

export function useChannels(
  libraryId?: string,
  options?: Partial<UseQueryOptions<Channel[]>>,
) {
  return useQuery<Channel[]>({
    queryKey: queryKeys.channels(libraryId),
    queryFn: () => api.getChannels(libraryId),
    ...options,
  });
}

export function useUsers(
  options?: Partial<UseQueryOptions<User[]>>,
) {
  return useQuery<User[]>({
    queryKey: queryKeys.users,
    queryFn: () => api.getUsers(),
    ...options,
  });
}

export function useHealth(
  options?: Partial<UseQueryOptions<HealthResponse>>,
) {
  return useQuery<HealthResponse>({
    queryKey: queryKeys.health,
    queryFn: () => api.getHealth(),
    ...options,
  });
}

export function useSetupStatus(
  options?: Partial<UseQueryOptions<SetupStatus>>,
) {
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

// ─── Mutation Hooks ─────────────────────────────────────────────────────────

export function useLogin() {
  const queryClient = useQueryClient();
  return useMutation<AuthResponse, Error, { username: string; password: string }>({
    mutationFn: ({ username, password }) => api.login(username, password),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.me });
    },
  });
}

export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, void>({
    mutationFn: () => api.logout(),
    onSuccess: () => {
      queryClient.clear();
    },
  });
}

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
  return useMutation<void, Error, string>({
    mutationFn: (id) => api.scanLibrary(id),
    onSuccess: (_data, id) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.library(id) });
      queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
    },
  });
}

export function useCreateUser() {
  const queryClient = useQueryClient();
  return useMutation<
    User,
    Error,
    { username: string; password: string; display_name?: string; role?: string }
  >({
    mutationFn: (data) => api.createUser(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
    },
  });
}

export function useDeleteUser() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (id) => api.deleteUser(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.users });
    },
  });
}

export function useUpdateProgress() {
  const queryClient = useQueryClient();
  return useMutation<
    UserData,
    Error,
    {
      itemId: string;
      data: {
        position_ticks?: number;
        audio_stream_index?: number;
        subtitle_stream_index?: number;
      };
    }
  >({
    mutationFn: ({ itemId, data }) => api.updateProgress(itemId, data),
    onSuccess: (_data, { itemId }) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.progress(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.continueWatching });
    },
  });
}

export function useToggleFavorite() {
  const queryClient = useQueryClient();
  return useMutation<UserData, Error, string>({
    mutationFn: (itemId) => api.toggleFavorite(itemId),
    onSuccess: (_data, itemId) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.favorites });
    },
  });
}

export function useMarkPlayed() {
  const queryClient = useQueryClient();
  return useMutation<UserData, Error, string>({
    mutationFn: (itemId) => api.markPlayed(itemId),
    onSuccess: (_data, itemId) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.continueWatching });
      queryClient.invalidateQueries({ queryKey: queryKeys.nextUp });
    },
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
