import {
  useQuery,
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  type UseQueryOptions,
} from "@tanstack/react-query";
import { api } from "./client";
import type {
  AddEPGSourceRequest,
  AuthResponse,
  AvailableImage,
  BrowseResponse,
  Channel,
  ChannelWithoutEPG,
  CreateLibraryRequest,
  HealthResponse,
  ImageInfo,
  ImportPublicIPTVResponse,
  ItemDetail,
  Library,
  LibraryEPGSource,
  MediaItem,
  PaginatedResponse,
  PatchChannelRequest,
  PublicCountry,
  PublicEPGSource,
  SetupStatus,
  SystemCapabilities,
  UnhealthyChannel,
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
  channelFavoriteIDs: ["channel-favorites", "ids"] as const,
  channelFavorites: ["channel-favorites", "list"] as const,
  channelGroups: (libraryId?: string) => ["channels", "groups", libraryId] as const,
  publicCountries: ["public-countries"] as const,
  epgCatalog: ["epg-catalog"] as const,
  libraryEPGSources: (libraryId: string) =>
    ["library-epg-sources", libraryId] as const,
  unhealthyChannels: (libraryId: string) =>
    ["unhealthy-channels", libraryId] as const,
  channelsWithoutEPG: (libraryId: string) =>
    ["channels-without-epg", libraryId] as const,
  myPreferences: ["my-preferences"] as const,
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

// ─── Channel favorites ────────────────────────────────────────────────
//
// Two queries because the frontend has two needs:
//   - a Set of IDs for instant toggle feedback on ChannelCard ♥ buttons
//     (cheap payload, invalidated on every mutation)
//   - a full list of favorite Channels for the Favorites tab
//     (heavier, same invalidation)
//
// Mutations optimistically update the IDs cache so the ♥ flips immediately,
// and then invalidate both caches to reconcile with server state.

export function useChannelFavoriteIDs(
  options?: Partial<UseQueryOptions<string[]>>,
) {
  return useQuery<string[]>({
    queryKey: queryKeys.channelFavoriteIDs,
    queryFn: () => api.getChannelFavoriteIDs(),
    staleTime: 60_000,
    ...options,
  });
}

export function useChannelFavorites(
  options?: Partial<UseQueryOptions<Channel[]>>,
) {
  return useQuery<Channel[]>({
    queryKey: queryKeys.channelFavorites,
    queryFn: () => api.getChannelFavorites(),
    staleTime: 60_000,
    ...options,
  });
}

export function useAddChannelFavorite() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string, { previous: string[] | undefined }>({
    mutationFn: (channelId) => api.addChannelFavorite(channelId),
    onMutate: async (channelId) => {
      // Optimistic: assume success and flip the local ID set before the
      // network round-trip lands. Keeps the ♥ responsive on slow links.
      await queryClient.cancelQueries({ queryKey: queryKeys.channelFavoriteIDs });
      const previous = queryClient.getQueryData<string[]>(
        queryKeys.channelFavoriteIDs,
      );
      queryClient.setQueryData<string[]>(
        queryKeys.channelFavoriteIDs,
        (old) => (old?.includes(channelId) ? old : [channelId, ...(old ?? [])]),
      );
      return { previous };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.previous) {
        queryClient.setQueryData(queryKeys.channelFavoriteIDs, ctx.previous);
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.channelFavoriteIDs });
      queryClient.invalidateQueries({ queryKey: queryKeys.channelFavorites });
    },
  });
}

export function useRemoveChannelFavorite() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string, { previous: string[] | undefined }>({
    mutationFn: (channelId) => api.removeChannelFavorite(channelId),
    onMutate: async (channelId) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.channelFavoriteIDs });
      const previous = queryClient.getQueryData<string[]>(
        queryKeys.channelFavoriteIDs,
      );
      queryClient.setQueryData<string[]>(
        queryKeys.channelFavoriteIDs,
        (old) => (old ?? []).filter((id) => id !== channelId),
      );
      return { previous };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.previous) {
        queryClient.setQueryData(queryKeys.channelFavoriteIDs, ctx.previous);
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.channelFavoriteIDs });
      queryClient.invalidateQueries({ queryKey: queryKeys.channelFavorites });
    },
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

// ─── EPG Sources ────────────────────────────────────────────────────────
//
// The catalog is static data shipped with the binary — fetch once, cache
// forever. The per-library source list mutates on add/remove/reorder; we
// invalidate surgically so the admin UI updates without a full refetch.

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
      queryClient.invalidateQueries({ queryKey: queryKeys.libraryEPGSources(libraryId) });
    },
  });
}

export function useRemoveEPGSource(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (sourceId) => api.removeEPGSource(libraryId, sourceId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.libraryEPGSources(libraryId) });
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

// ─── Channel Health ─────────────────────────────────────────────────────
//
// Admin-only surface over the opportunistic probe data. The list polls
// so the badge updates as viewers try channels; actions (reset / disable
// / enable) invalidate both the health list AND the regular channel list
// (a newly-enabled channel reappears there, a disabled one vanishes).

export function useUnhealthyChannels(
  libraryId: string,
  options?: Partial<UseQueryOptions<UnhealthyChannel[]>>,
) {
  return useQuery<UnhealthyChannel[]>({
    queryKey: queryKeys.unhealthyChannels(libraryId),
    queryFn: () => api.listUnhealthyChannels(libraryId),
    enabled: !!libraryId,
    // Poll every 30s so the admin sees live signal without hammering.
    refetchInterval: 30_000,
    ...options,
  });
}

export function useResetChannelHealth(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (channelId) => api.resetChannelHealth(channelId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.unhealthyChannels(libraryId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.channels(libraryId) });
    },
  });
}

export function useDisableChannel(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (channelId) => api.disableChannel(channelId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.unhealthyChannels(libraryId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.channels(libraryId) });
    },
  });
}

export function useEnableChannel(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (channelId) => api.enableChannel(channelId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.unhealthyChannels(libraryId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.channels(libraryId) });
    },
  });
}

// ─── Channels without EPG ───────────────────────────────────────────────
//
// Admin surface that pairs with the "canales sin guía" panel. The
// list comes from the backend filtered to active channels with no
// programmes in the 24h window. Editing `tvg_id` via PATCH invalidates
// both this list and the library channel list so the UI refreshes
// right away (the orphan disappears once the next EPG refresh matches it).

export function useChannelsWithoutEPG(
  libraryId: string,
  options?: Partial<UseQueryOptions<ChannelWithoutEPG[]>>,
) {
  return useQuery<ChannelWithoutEPG[]>({
    queryKey: queryKeys.channelsWithoutEPG(libraryId),
    queryFn: () => api.listChannelsWithoutEPG(libraryId),
    enabled: !!libraryId,
    ...options,
  });
}

export function usePatchChannel(libraryId: string) {
  const queryClient = useQueryClient();
  return useMutation<
    ChannelWithoutEPG,
    Error,
    { channelId: string; patch: PatchChannelRequest }
  >({
    mutationFn: ({ channelId, patch }) => api.patchChannel(channelId, patch),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.channelsWithoutEPG(libraryId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.channels(libraryId) });
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

// ─── User preferences ───────────────────────────────────────────────────
//
// Per-user key/value store backed by the /me/preferences endpoint. Every
// component that persists a UI choice across sessions AND devices uses
// `useUserPreference(key, default)` instead of localStorage so the user's
// laptop and phone stay in sync.
//
// The hook is generic over the stored value's shape: callers give a
// JSON-serialisable type and a default. Values are stored as strings
// server-side; the hook encodes/decodes JSON transparently.

export function useMyPreferences() {
  return useQuery<Record<string, string>>({
    queryKey: queryKeys.myPreferences,
    queryFn: () => api.getMyPreferences(),
    staleTime: 30_000,
  });
}

export function useSetMyPreference() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, { key: string; value: string }>({
    mutationFn: ({ key, value }) => api.setMyPreference(key, value),
    // Optimistic update: write the new value into the cache immediately so
    // the UI doesn't flash the old value while the request flies. Roll
    // back to the prior map on failure.
    onMutate: async ({ key, value }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.myPreferences });
      const previous = queryClient.getQueryData<Record<string, string>>(
        queryKeys.myPreferences,
      );
      queryClient.setQueryData<Record<string, string>>(
        queryKeys.myPreferences,
        (old) => ({ ...(old ?? {}), [key]: value }),
      );
      return { previous };
    },
    onError: (_err, _vars, ctx) => {
      const prev = (ctx as { previous?: Record<string, string> } | undefined)?.previous;
      if (prev) {
        queryClient.setQueryData(queryKeys.myPreferences, prev);
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.myPreferences });
    },
  });
}

/**
 * useUserPreference — typed wrapper over the key/value store. JSON-encodes
 * the value when setting and JSON-decodes when reading. Defaults to
 * `fallback` while the preferences query is still loading or when the key
 * is unset; never returns undefined so callers can render unconditionally.
 */
export function useUserPreference<T>(key: string, fallback: T) {
  const { data } = useMyPreferences();
  const setter = useSetMyPreference();

  let value: T = fallback;
  const raw = data?.[key];
  if (raw !== undefined) {
    try {
      value = JSON.parse(raw) as T;
    } catch {
      // Corrupt value (hand-edited DB, bad migration, etc.): fall back
      // silently rather than crash the surface that depends on it.
      value = fallback;
    }
  }

  const setValue = (next: T) => {
    setter.mutate({ key, value: JSON.stringify(next) });
  };

  return [value, setValue] as const;
}

// IPTV refresh mutations. Separate from useScanLibrary because filesystem
// scan doesn't apply to livetv libraries — these hit /iptv/refresh-m3u and
// /iptv/refresh-epg instead, and invalidate the channel + EPG caches so
// the Live TV page reflects the refresh without a page reload.

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
      queryClient.invalidateQueries({ queryKey: queryKeys.libraryEPGSources(libraryId) });
      // A successful refresh may have turned orphans into matched
      // channels; refresh the "canales sin guía" list too.
      queryClient.invalidateQueries({ queryKey: queryKeys.channelsWithoutEPG(libraryId) });
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
