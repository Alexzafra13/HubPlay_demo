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
	RefreshToken(ctx context.Context, refreshToken string) (*auth.AuthToken, error)
	Logout(ctx context.Context, refreshToken string) error
	Register(ctx context.Context, req auth.RegisterRequest) (*db.User, error)
	ValidateToken(ctx context.Context, tokenStr string) (*auth.Claims, error)
	Middleware(next http.Handler) http.Handler
}

// ─── User service ───────────────────────────────────────────────────────────

// UserService defines user operations needed by handlers.
type UserService interface {
	GetByID(ctx context.Context, id string) (*db.User, error)
	List(ctx context.Context, limit, offset int) ([]*db.User, int, error)
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int, error)
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
	// id has in one round-trip. Used by the Children handler to dedupe
	// duplicate season rows: when two seasons share the same series +
	// season_number, the one with episodes attached wins.
	GetItemChildCounts(ctx context.Context, parentIDs []string) (map[string]int, error)
	GetItemStreams(ctx context.Context, itemID string) ([]*db.MediaStream, error)
	GetItemImages(ctx context.Context, itemID string) ([]*db.Image, error)
	LatestItems(ctx context.Context, libraryID string, itemType string, limit int) ([]*db.Item, error)
	ItemCount(ctx context.Context, libraryID string) (int, error)
	UserHasAccess(ctx context.Context, userID, libraryID string) (bool, error)
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
	StartSession(ctx context.Context, userID, itemID, profileName string, startTime float64) (*stream.ManagedSession, error)
	GetSession(key string) (*stream.ManagedSession, bool)
	StopSession(key string)
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
	RefreshEPG(ctx context.Context, libraryID string) (int, error)

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
	GetPrimaryURLs(ctx context.Context, itemIDs []string) (map[string]map[string]string, error)
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

// ChapterRepository defines chapter data access needed by handlers.
// Optional dep: when nil, the item-detail handler simply omits the
// `chapters` field — older test environments and bare deployments
// keep working without one wired.
type ChapterRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]*db.Chapter, error)
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
	Delete(ctx context.Context, userID, itemID string) error
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
