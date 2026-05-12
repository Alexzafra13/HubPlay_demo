package handlers

import (
	"context"
	"net/http"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/iptv"
	"hubplay/internal/library"
	"hubplay/internal/provider"
	"hubplay/internal/scanner"
	"hubplay/internal/setup"
	"hubplay/internal/stream"
)

// ─── Auth service ───────────────────────────────────────────────────────────

// AuthService defines auth operations needed by handlers.
type AuthService interface {
	Login(ctx context.Context, username, password, deviceName, deviceID, ip string) (*auth.AuthToken, error)
	RefreshToken(ctx context.Context, refreshToken, ip string) (*auth.AuthToken, error)
	Logout(ctx context.Context, refreshToken string) error
	Register(ctx context.Context, req auth.RegisterRequest) (*db.User, error)
	ResetPassword(ctx context.Context, userID string) (string, error)
	ChangePassword(ctx context.Context, userID, current, next string) error
	ListProfiles(ctx context.Context, userID string) ([]*db.User, error)
	SwitchProfile(ctx context.Context, currentUserID, targetProfileID, pin, deviceName, deviceID, ip string) (*auth.AuthToken, error)
	SetPIN(ctx context.Context, userID, pin string) error
	ValidateToken(ctx context.Context, tokenStr string) (*auth.Claims, error)
	Middleware(next http.Handler) http.Handler
	ListSessions(ctx context.Context, userID string) ([]*db.Session, error)
	RevokeSession(ctx context.Context, userID, sessionID string) error
	CurrentSessionID(ctx context.Context, refreshToken string) string
}

// ─── User service ───────────────────────────────────────────────────────────

// UserService defines user operations needed by handlers.
type UserService interface {
	GetByID(ctx context.Context, id string) (*db.User, error)
	List(ctx context.Context, limit, offset int) ([]*db.User, int, error)
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int, error)
	SetMaxContentRating(ctx context.Context, id, rating string) error
	SetDisplayName(ctx context.Context, id, name string) error
	SetAvatarColor(ctx context.Context, id, hex string) error
	SetRole(ctx context.Context, id, role string) error
	SetActive(ctx context.Context, id string, active bool) error
	SetAccessExpiresAt(ctx context.Context, id string, expiresAt *time.Time) error
	PrimaryAdminID(ctx context.Context) (string, error)
}

// ─── Library service ────────────────────────────────────────────────────────

// LibraryService defines library and item operations needed by handlers.
type LibraryService interface {
	Create(ctx context.Context, req library.CreateRequest) (*db.Library, error)
	GetByID(ctx context.Context, id string) (*db.Library, error)
	List(ctx context.Context) ([]*db.Library, error)
	ListForUser(ctx context.Context, userID string) ([]*db.Library, error)
	Update(ctx context.Context, id string, req library.UpdateRequest) (*db.Library, error)
	Delete(ctx context.Context, id string) error
	Scan(ctx context.Context, id string, refreshMetadata ...bool) error
	ScanSync(ctx context.Context, id string) (*scanner.ScanResult, error)
	IsScanning(id string) bool
	ListItems(ctx context.Context, filter db.ItemFilter) ([]*db.Item, int, error)
	GetItem(ctx context.Context, id string) (*db.Item, error)
	GetItemChildren(ctx context.Context, id string) ([]*db.Item, error)
	// GetItemChildCounts returns how many direct children each parent
	// id has in one round-trip. Used by the Children handler to inject
	// `episode_count` into season summaries.
	GetItemChildCounts(ctx context.Context, parentIDs []string) (map[string]int, error)
	GetItemStreams(ctx context.Context, itemID string) ([]*db.MediaStream, error)
	GetItemImages(ctx context.Context, itemID string) ([]*db.Image, error)
	LatestItems(ctx context.Context, libraryID string, itemType string, limit int, capRating string) ([]*db.Item, error)
	LatestSeriesByActivity(ctx context.Context, libraryID string, limit int) ([]*db.LatestSeriesActivity, error)
	ItemCount(ctx context.Context, libraryID string) (int, error)
	UserHasAccess(ctx context.Context, userID, libraryID string) (bool, error)
	// GrantAccess / RevokeAccess / ListAccessByUser / ReplaceAccess
	// power the admin user-libraries matrix. The userID MUST be a
	// top-level user (ADR-014): library_access never points at a
	// profile, so the handler resolves the row to its parent before
	// reaching here.
	GrantAccess(ctx context.Context, userID, libraryID string) error
	RevokeAccess(ctx context.Context, userID, libraryID string) error
	ListAccessByUser(ctx context.Context, userID string) ([]string, error)
	ReplaceAccess(ctx context.Context, userID string, libraryIDs []string) error
	// ListGenres returns the genre vocabulary across the catalogue,
	// optionally scoped by item type ("movie", "series", or "" for the
	// union). Used by the /movies and /series filter panel so the
	// available chips reflect the entire library, not just the loaded
	// page.
	ListGenres(ctx context.Context, itemType string) ([]db.GenreCount, error)
}

// LibraryAccessService is the minimal surface the IPTV handler uses to gate
// channel/EPG endpoints on per-library ACLs. Defined separately so tests can
// fake just the one method without pulling in the fat LibraryService mock.
type LibraryAccessService interface {
	UserHasAccess(ctx context.Context, userID, libraryID string) (bool, error)
}

// ─── Stream manager ─────────────────────────────────────────────────────────

// StreamManagerService defines streaming operations needed by handlers.
type StreamManagerService interface {
	StartSession(ctx context.Context, userID, itemID, profileName string, caps *stream.Capabilities, startTime float64, audioStreamIndex, burnSubIndex int) (*stream.ManagedSession, error)
	GetSession(key string) (*stream.ManagedSession, bool)
	// RestartSessionAt re-spawns the ffmpeg behind an active session
	// so it begins encoding at `segmentIndex * segmentDuration`.
	// Used by the segment handler when the player asks for a
	// far-future segment that the existing ffmpeg run hasn't reached.
	RestartSessionAt(key string, segmentIndex int, segmentDuration float64) error
	StopSession(key string)
	// StopSessionsByItem stops every active session for (user, item)
	// across qualities and audio configs. Used by the player-teardown
	// DELETE so a single call frees the whole bag the player accreted
	// during ABR + audio-track switches.
	StopSessionsByItem(userID, itemID string) int
	ActiveSessions() int
}

// ─── IPTV service ───────────────────────────────────────────────────────────

// IPTVService defines IPTV operations needed by handlers.
type IPTVService interface {
	GetChannels(ctx context.Context, libraryID string, activeOnly bool) ([]*db.Channel, error)
	GetChannel(ctx context.Context, id string) (*db.Channel, error)
	GetGroups(ctx context.Context, libraryID string) ([]string, error)
	GetSchedule(ctx context.Context, channelID string, from, to time.Time) ([]*db.EPGProgram, error)
	GetBulkSchedule(ctx context.Context, channelIDs []string, from, to time.Time) (map[string][]*db.EPGProgram, error)
	NowPlaying(ctx context.Context, channelID string) (*db.EPGProgram, error)
	RefreshM3U(ctx context.Context, libraryID string) (int, error)
	// TryAcquireRefresh + RunRefreshM3U + PublishRefreshFailed split
	// the M3U refresh so the HTTP handler can return 202 immediately
	// and run the actual import in a goroutine that survives client
	// disconnect / nginx proxy_read_timeout. See iptv_admin.go.
	TryAcquireRefresh(libraryID string) (func(), error)
	RunRefreshM3U(ctx context.Context, libraryID string) (int, error)
	PublishRefreshFailed(libraryID string, err error)
	RefreshEPG(ctx context.Context, libraryID string) (int, error)
	PreflightCheck(ctx context.Context, m3uURL string, tlsInsecure bool) iptv.PreflightResult

	// Channel favorites (per-user, persisted in user_channel_favorites).
	AddFavorite(ctx context.Context, userID, channelID string) error
	RemoveFavorite(ctx context.Context, userID, channelID string) error
	IsFavorite(ctx context.Context, userID, channelID string) (bool, error)
	ListFavoriteIDs(ctx context.Context, userID string) ([]string, error)
	ListFavoriteChannels(ctx context.Context, userID string) ([]*db.Channel, error)

	// EPG sources (per-library, multi-provider config).
	PublicEPGCatalog() []iptv.PublicEPGSource
	ListEPGSources(ctx context.Context, libraryID string) ([]*db.LibraryEPGSource, error)
	AddEPGSource(ctx context.Context, libraryID, catalogID, customURL string) (*db.LibraryEPGSource, error)
	RemoveEPGSource(ctx context.Context, libraryID, sourceID string) error
	ReorderEPGSources(ctx context.Context, libraryID string, orderedIDs []string) error

	// Channel health — admin surface for the opportunistic probe
	// data the stream proxy records. SetChannelActive already exists;
	// the admin UI pairs it with ResetChannelHealth so an operator
	// can either permanently disable a dead channel or clear its
	// counter if they know it's actually working.
	ListUnhealthyChannels(ctx context.Context, libraryID string, threshold int) ([]*db.Channel, error)
	// ChannelHealthSummary is the lightweight aggregate the admin
	// Bibliotecas panel reads on first paint (counts only) so the
	// page doesn't pull every unhealthy / without-EPG row just to
	// render badges.
	ChannelHealthSummary(ctx context.Context, libraryID string) (db.ChannelHealthSummary, error)
	SetChannelActive(ctx context.Context, id string, active bool) error
	ResetChannelHealth(ctx context.Context, channelID string) error
	// RecordProbeFailure is the same hook the proxy uses; the player
	// beacon endpoint forwards client-side fatal errors here so any
	// failure source bumps the same `consecutive_failures` counter.
	RecordProbeFailure(ctx context.Context, channelID string, err error)

	// Manual channel editing — surfaced as the "canales sin guía"
	// admin panel. The override layer makes SetChannelTvgID survive
	// the next M3U refresh.
	ListChannelsWithoutEPG(ctx context.Context, libraryID string) ([]*db.Channel, error)
	SetChannelTvgID(ctx context.Context, channelID, tvgID string) error

	// Continue watching — per-user recently-played channel rail.
	// The beacon records (user, channel); the list query resolves to
	// current channel rows via stream_url so entries survive M3U
	// refresh cycles.
	RecordWatch(ctx context.Context, userID, channelID string) (time.Time, error)
	ListContinueWatching(ctx context.Context, userID string, limit int, accessibleLibraries map[string]bool) ([]*db.Channel, []time.Time, error)
}

// IPTVStreamProxyService defines IPTV proxy operations needed by handlers.
type IPTVStreamProxyService interface {
	ProxyStream(ctx context.Context, w http.ResponseWriter, channelID, streamURL string) error
	ProxyURL(ctx context.Context, w http.ResponseWriter, channelID, rawURL string) error
}

// IPTVTransmuxer is the minimum surface the channel-stream handler
// needs from the live MPEG-TS → HLS session manager. The handler
// imports iptv anyway, but expressing the dependency as an interface
// here lets tests inject a fake without spinning real ffmpeg
// processes — and keeps the handler from accidentally reaching for
// internal manager state.
type IPTVTransmuxer interface {
	// GetOrStart returns a live session for the channel, spawning a
	// new ffmpeg process if necessary. Blocks until the session has
	// produced its first segment or the manager-side timeout elapses.
	GetOrStart(ctx context.Context, channelID, upstreamURL string) (*iptv.TransmuxSession, error)
	// Touch records that a viewer is still consuming the session,
	// preventing the idle reaper from killing it. Returns
	// iptv.ErrSessionNotFound when the session has expired.
	Touch(channelID string) (*iptv.TransmuxSession, error)
}

// ─── Repository interfaces ──────────────────────────────────────────────────

// ItemRepository defines item data access needed by handlers.
type ItemRepository interface {
	GetByID(ctx context.Context, id string) (*db.Item, error)
	List(ctx context.Context, filter db.ItemFilter) ([]*db.Item, int, error)
}

// MediaStreamRepository defines media stream data access needed by handlers.
type MediaStreamRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]*db.MediaStream, error)
}

// ImageRepository defines image data access needed by handlers.
type ImageRepository interface {
	GetPrimaryURLs(ctx context.Context, itemIDs []string) (map[string]map[string]db.PrimaryImageRef, error)
	ListByItem(ctx context.Context, itemID string) ([]*db.Image, error)
	Create(ctx context.Context, img *db.Image) error
	SetPrimary(ctx context.Context, itemID, imgType, imageID string) error
	SetLocked(ctx context.Context, imageID string, locked bool) error
	GetByID(ctx context.Context, id string) (*db.Image, error)
	DeleteByID(ctx context.Context, id string) error
}

// MetadataRepository defines metadata access needed by handlers.
type MetadataRepository interface {
	GetByItemID(ctx context.Context, itemID string) (*db.Metadata, error)
	GetMetadataBatch(ctx context.Context, itemIDs []string) (map[string]*db.Metadata, error)
}

// ExternalIDsRepository defines the per-item external-id lookup
// needed by the items handler. Used to surface IMDb / TMDb / TVDB
// links in the detail response so the client can render "Open in
// IMDb" / "Open in TMDb" affordances without a second round-trip.
type ExternalIDsRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]*db.ExternalID, error)
	// GetItemIDByExternalID is the reverse lookup used by the
	// recommendations endpoint to mark TMDb candidates that the user
	// already has locally. Returns "" when no item carries that
	// (provider, external_id) pairing.
	GetItemIDByExternalID(ctx context.Context, provider, externalID string) (string, error)
}

// PeopleRepoForItems is the per-item people lookup used by the
// items handler to fold cast/crew into the detail response.
type PeopleRepoForItems interface {
	ListByItem(ctx context.Context, itemID string) ([]*db.ItemPersonCredit, error)
}

// CollectionRepoForItems is the per-collection lookup used by the
// items handler to surface the "Part of: X" affordance on a movie's
// detail page. nil-safe at the handler level so deployments without
// the collections feature wired keep returning the same shape.
type CollectionRepoForItems interface {
	GetByID(ctx context.Context, id string) (*db.Collection, error)
}

// ChapterRepository defines chapter data access needed by handlers.
// Optional dep: when nil, the item-detail handler simply omits the
// `chapters` field — older test environments and bare deployments
// keep working without one wired.
type ChapterRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]*db.Chapter, error)
}

// EpisodeSegmentRepository surfaces skip-intro / skip-credits markers
// to the item handler so the playback page can render the floating
// "Saltar intro" / "Saltar créditos" buttons without a second API
// call. One row per (item_id, kind, source) — a single episode can
// carry chapter-derived AND fingerprint-derived segments in the same
// query result; the handler picks the highest-confidence row per
// kind before serialising.
type EpisodeSegmentRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]db.EpisodeSegment, error)
}

// UserDataRepository defines user data access needed by handlers.
type UserDataRepository interface {
	Get(ctx context.Context, userID, itemID string) (*db.UserData, error)
	GetBatch(ctx context.Context, userID string, itemIDs []string) (map[string]*db.UserData, error)
	UpdateProgress(ctx context.Context, userID, itemID string, positionTicks int64, completed bool) error
	MarkPlayed(ctx context.Context, userID, itemID string) error
	SetFavorite(ctx context.Context, userID, itemID string, favorite bool) error
	ContinueWatching(ctx context.Context, userID string, limit int) ([]*db.ContinueWatchingItem, error)
	Favorites(ctx context.Context, userID string, limit, offset int) ([]*db.FavoriteItem, error)
	NextUp(ctx context.Context, userID string, limit int) ([]*db.NextUpItem, error)
	SeriesEpisodeProgress(ctx context.Context, userID, seriesID string) (total, watched int, err error)
	Delete(ctx context.Context, userID, itemID string) error
	ClearProgress(ctx context.Context, userID, itemID string) error
}

// ImageRefreshService runs the library-wide image refresh. Defined here so
// handlers depend on an interface, not the concrete library.ImageRefresher —
// keeps the handler layer's compile-time surface minimal and tests trivial.
type ImageRefreshService interface {
	RefreshForLibrary(ctx context.Context, libraryID string) (int, error)
}

// EventBusSubscriber defines the event bus subscription needed by handlers.
// Subscribe returns an unsubscribe function; handlers MUST call it when the
// subscriber goes away (e.g. SSE client disconnect) to avoid handler leaks.
type EventBusSubscriber interface {
	Subscribe(eventType event.Type, handler event.Handler) func()
}

// EventBusPublisher is the publish-only side of the bus, used by
// handlers that emit events but never consume them (progress handler
// fans out user-scoped events to other clients of the same user).
type EventBusPublisher interface {
	Publish(e event.Event)
}

// ─── Setup service ──────────────────────────────────────────────────────────

// SetupService defines setup wizard operations needed by handlers.
type SetupService interface {
	NeedsSetup(ctx context.Context) bool
	BrowseDirectories(path string) (*setup.BrowseResult, error)
	DetectCapabilities() *setup.SystemCapabilities
	CompleteSetup(startScan bool) error
}

// ─── Provider interfaces ────────────────────────────────────────────────────

// ProviderManager defines metadata/image/subtitle provider operations.
type ProviderManager interface {
	SearchMetadata(ctx context.Context, query provider.SearchQuery) ([]provider.SearchResult, error)
	FetchMetadata(ctx context.Context, externalID string, itemType provider.ItemType) (*provider.MetadataResult, error)
	FetchImages(ctx context.Context, externalIDs map[string]string, itemType provider.ItemType) ([]provider.ImageResult, error)
	SearchSubtitles(ctx context.Context, query provider.SubtitleQuery) ([]provider.SubtitleResult, error)
	DownloadSubtitle(ctx context.Context, sourceName, fileID string) ([]byte, error)
	// FetchRecommendations powers the "more like this" rail on the
	// detail page. Implementations return (nil, nil) when no provider
	// can resolve recs for the given external id — handlers render
	// an empty rail rather than a 5xx for that case.
	FetchRecommendations(ctx context.Context, externalID string, itemType provider.ItemType, limit int) ([]provider.RecommendationResult, error)
}

// ProviderRepository defines provider config data access.
type ProviderRepository interface {
	ListAll(ctx context.Context) ([]*db.ProviderConfig, error)
	GetByName(ctx context.Context, name string) (*db.ProviderConfig, error)
	Upsert(ctx context.Context, p *db.ProviderConfig) error
}

// LibraryRepository defines library data access for handlers that need direct repo access.
type LibraryRepository interface {
	Create(ctx context.Context, lib *db.Library) error
	// ListForUser returns every library the given user has explicit
	// access to. Used by handlers that need to materialise the
	// library-access set (e.g. continue-watching filter).
	ListForUser(ctx context.Context, userID string) ([]*db.Library, error)
}

// ExternalIDRepository defines external ID data access.
type ExternalIDRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]*db.ExternalID, error)
}
