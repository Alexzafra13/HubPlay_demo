import {
  useQuery,
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  type UseQueryOptions,
} from "@tanstack/react-query";
import { api } from "./client";
import type {
  AuthResponse,
  AvailableImage,
  BrowseResponse,
  Channel,
  CreateLibraryRequest,
  HealthResponse,
  ImageInfo,
  ImportPublicIPTVResponse,
  ItemDetail,
  Library,
  MediaItem,
  PaginatedResponse,
  PublicCountry,
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
  publicCountries: ["public-countries"] as const,
  itemImages: (id: string) => ["items", id, "images"] as const,
  availableImages: (id: string, type?: string) => ["items", id, "images", "available", type] as const,
  providers: ["providers"] as const,
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

const PAGE_SIZE = 40;

export function useInfiniteItems(
  params?: {
    library_id?: string;
    type?: string;
    sort_by?: string;
    sort_order?: string;
  },
) {
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

export function useBulkSchedule(
  channelIds: string[],
  options?: Partial<UseQueryOptions<Record<string, import("./types").EPGProgram[]>>>,
) {
  // Sort the id list before hashing it so cache hits work regardless of
  // channel ordering. The previous implementation sliced to the first 10
  // which caused stale cache hits on libraries larger than 10 channels.
  const cacheKey = [...channelIds].sort().join(",");
  return useQuery<Record<string, import("./types").EPGProgram[]>>({
    queryKey: ["bulk-schedule", cacheKey] as const,
    queryFn: () => api.getBulkSchedule(channelIds),
    enabled: channelIds.length > 0,
    staleTime: 2 * 60 * 1000,
    refetchInterval: 5 * 60 * 1000,
    ...options,
  });
}

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
  return useMutation<ImportPublicIPTVResponse, Error, { country: string; name?: string }>({
    mutationFn: ({ country, name }) => api.importPublicIPTV(country, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
    },
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

export function useProviders() {
  return useQuery<
    Array<{
      name: string;
      type: string;
      status: string;
      priority: number;
      has_api_key: boolean;
      config?: Record<string, string>;
    }>
  >({
    queryKey: queryKeys.providers,
    queryFn: () => api.getProviders(),
  });
}

export function useUpdateProvider() {
  const queryClient = useQueryClient();
  return useMutation<
    { name: string; status: string; priority: number },
    Error,
    { name: string; data: { api_key?: string; status?: string; priority?: number; config?: Record<string, string> } }
  >({
    mutationFn: ({ name, data }) => api.updateProvider(name, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.providers });
    },
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

// ─── Image Hooks ───────────────────────────────────────────────────────────

export function useItemImages(itemId: string, options?: Partial<UseQueryOptions<ImageInfo[]>>) {
  return useQuery<ImageInfo[]>({
    queryKey: queryKeys.itemImages(itemId),
    queryFn: () => api.getItemImages(itemId),
    enabled: !!itemId,
    ...options,
  });
}

export function useAvailableImages(itemId: string, type?: string, options?: Partial<UseQueryOptions<AvailableImage[]>>) {
  return useQuery<AvailableImage[]>({
    queryKey: queryKeys.availableImages(itemId, type),
    queryFn: () => api.getAvailableImages(itemId, type),
    enabled: !!itemId,
    staleTime: 5 * 60 * 1000,
    ...options,
  });
}

export function useSelectImage() {
  const queryClient = useQueryClient();
  return useMutation<ImageInfo, Error, { itemId: string; type: string; url: string; width: number; height: number }>({
    mutationFn: ({ itemId, type, ...data }) => api.selectImage(itemId, type, data),
    onSuccess: (_data, { itemId }) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.itemImages(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
    },
  });
}

export function useUploadImage() {
  const queryClient = useQueryClient();
  return useMutation<ImageInfo, Error, { itemId: string; type: string; file: File }>({
    mutationFn: ({ itemId, type, file }) => api.uploadImage(itemId, type, file),
    onSuccess: (_data, { itemId }) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.itemImages(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
    },
  });
}

export function useSetImagePrimary() {
  const queryClient = useQueryClient();
  return useMutation<ImageInfo, Error, { itemId: string; imageId: string }>({
    mutationFn: ({ itemId, imageId }) => api.setImagePrimary(itemId, imageId),
    onSuccess: (_data, { itemId }) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.itemImages(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
    },
  });
}

export function useDeleteImage() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { itemId: string; imageId: string }>({
    mutationFn: ({ itemId, imageId }) => api.deleteImage(itemId, imageId),
    onSuccess: (_data, { itemId }) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.itemImages(itemId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.item(itemId) });
    },
  });
}

export function useRefreshLibraryImages() {
  const queryClient = useQueryClient();
  return useMutation<{ updated: number }, Error, { libraryId: string }>({
    mutationFn: ({ libraryId }) => api.refreshLibraryImages(libraryId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["libraries"] });
    },
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
    onSettled: () => {
      // Always clear cache and redirect, even if the API call fails.
      // The user wants to log out regardless of server response.
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
  return useMutation<void, Error, { id: string; refreshMetadata?: boolean }>({
    mutationFn: ({ id, refreshMetadata }) => api.scanLibrary(id, refreshMetadata),
    onSuccess: (_data, { id }) => {
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
