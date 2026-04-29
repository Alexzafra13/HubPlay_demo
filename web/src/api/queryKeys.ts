// Centralised TanStack Query keys.
//
// Lives outside `hooks.ts` so the same keys can be referenced from
// every per-domain hooks file (e.g. media.ts invalidates
// queryKeys.libraries from a mutation it owns; iptv-admin.ts
// invalidates the same key after refreshing an M3U). Mutations cross
// domain boundaries on purpose — keeping keys central avoids the
// "two truths, one stale" problem that comes from each domain holding
// its own copy.
//
// Keys are `as const` tuples so TanStack's prefix-matching invalidation
// works as expected: `queryClient.invalidateQueries({ queryKey: ["items"] })`
// hits everything below items/* without listing each variant.

export const queryKeys = {
  me: ["me"] as const,
  users: ["users"] as const,
  libraries: ["libraries"] as const,
  library: (id: string) => ["libraries", id] as const,
  items: (params?: Record<string, unknown>) => ["items", params] as const,
  item: (id: string) => ["items", id] as const,
  itemChildren: (id: string) => ["items", id, "children"] as const,
  person: (id: string) => ["people", id] as const,
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
  scheduledJobs: (libraryId: string) =>
    ["iptv-scheduled-jobs", libraryId] as const,
  continueWatchingChannels: ["continue-watching-channels"] as const,
  myPreferences: ["my-preferences"] as const,
  itemImages: (id: string) => ["items", id, "images"] as const,
  availableImages: (id: string, type?: string) =>
    ["items", id, "images", "available", type] as const,
  providers: ["providers"] as const,
  federationIdentity: ["federation", "identity"] as const,
  federationPeers: ["federation", "peers"] as const,
  federationInvites: ["federation", "invites"] as const,
  federationPeerShares: (peerID: string) => ["federation", "peers", peerID, "shares"] as const,
  health: ["health"] as const,
  systemStats: ["system-stats"] as const,
  systemSettings: ["system-settings"] as const,
  authKeys: ["auth-keys"] as const,
  setupStatus: ["setup-status"] as const,
  systemCapabilities: ["system-capabilities"] as const,
  browseDirectories: (path?: string) => ["browse", path] as const,
  progress: (itemId: string) => ["progress", itemId] as const,
} as const;
