package handlers

import (
	"context"
	"net/http"
	"time"

	authmodel "hubplay/internal/auth/model"
	iptvmodel "hubplay/internal/iptv/model"
	librarymodel "hubplay/internal/library/model"
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
	Register(ctx context.Context, req auth.RegisterRequest) (*authmodel.User, error)
	ResetPassword(ctx context.Context, userID string) (string, error)
	ChangePassword(ctx context.Context, userID, current, next string) error
	ListProfiles(ctx context.Context, userID string) ([]*authmodel.User, error)
	SwitchProfile(ctx context.Context, currentUserID, targetProfileID, pin, deviceName, deviceID, ip string) (*auth.AuthToken, error)
	SetPIN(ctx context.Context, userID, pin string) error
	ValidateToken(ctx context.Context, tokenStr string) (*auth.Claims, error)
	Middleware(next http.Handler) http.Handler
	ListSessions(ctx context.Context, userID string) ([]*authmodel.Session, error)
	RevokeSession(ctx context.Context, userID, sessionID string) error
	CurrentSessionID(ctx context.Context, refreshToken string) string
}

// ─── User service ───────────────────────────────────────────────────────────

// UserService defines user operations needed by handlers.
type UserService interface {
	GetByID(ctx context.Context, id string) (*authmodel.User, error)
	List(ctx context.Context, limit, offset int) ([]*authmodel.User, int, error)
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int, error)
	SetMaxContentRating(ctx context.Context, id, rating string) error
	SetDisplayName(ctx context.Context, id, name string) error
	SetAvatarColor(ctx context.Context, id, hex string) error
	SetRole(ctx context.Context, id, role string) error
	SetActive(ctx context.Context, id string, active bool) error
	SetAccessExpiresAt(ctx context.Context, id string, expiresAt *time.Time) error
	PrimaryAdminID(ctx context.Context) (string, error)
	UploadAvatar(ctx context.Context, userID string, data []byte, contentType string) (string, error)
	DeleteAvatar(ctx context.Context, userID string) error
	AvatarsDir() string
	AvatarFilePath(relName string) (string, error)
	// EnsureOwner promueve a userID como owner si no hay owner aún
	// (migración 055). Idempotente — la llama el setup wizard tras
	// crear el primer admin para que un install fresh no quede sin
	// owner. Devuelve promoted=true sólo cuando la fila se modificó.
	EnsureOwner(ctx context.Context, userID string) (bool, error)
}

// ─── Library service ────────────────────────────────────────────────────────

// LibraryService defines library and item operations needed by handlers.
type LibraryService interface {
	Create(ctx context.Context, req library.CreateRequest) (*librarymodel.Library, error)
	GetByID(ctx context.Context, id string) (*librarymodel.Library, error)
	List(ctx context.Context) ([]*librarymodel.Library, error)
	ListForUser(ctx context.Context, userID string) ([]*librarymodel.Library, error)
	Update(ctx context.Context, id string, req library.UpdateRequest) (*librarymodel.Library, error)
	Delete(ctx context.Context, id string) error
	Scan(ctx context.Context, id string, refreshMetadata ...bool) error
	ScanSync(ctx context.Context, id string) (*scanner.ScanResult, error)
	IsScanning(id string) bool
	ListItems(ctx context.Context, filter librarymodel.ItemFilter) ([]*librarymodel.Item, int, error)
	GetItem(ctx context.Context, id string) (*librarymodel.Item, error)
	GetItemChildren(ctx context.Context, id string) ([]*librarymodel.Item, error)
	// GetItemChildCounts returns how many direct children each parent
	// id has in one round-trip. Used by the Children handler to inject
	// `episode_count` into season summaries.
	GetItemChildCounts(ctx context.Context, parentIDs []string) (map[string]int, error)
	GetItemStreams(ctx context.Context, itemID string) ([]*librarymodel.MediaStream, error)
	GetItemImages(ctx context.Context, itemID string) ([]*librarymodel.Image, error)
	LatestItems(ctx context.Context, libraryID string, itemType string, limit int, capRating string) ([]*librarymodel.Item, error)
	LatestSeriesByActivity(ctx context.Context, libraryID string, limit int) ([]*librarymodel.LatestSeriesActivity, error)
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
	// CreatePersonalIPTV creates a livetv library + a grant for the
	// owner in one tx. Powers the admin "add personal IPTV list to
	// user X" shortcut so the operator can skip the two-navigation
	// dance of creating a library and then ticking a checkbox.
	CreatePersonalIPTV(ctx context.Context, ownerUserID string, req library.CreateRequest) (*librarymodel.Library, error)
	// ListGenres returns the genre vocabulary across the catalogue,
	// optionally scoped by item type ("movie", "series", or "" for the
	// union). Used by the /movies and /series filter panel so the
	// available chips reflect the entire library, not just the loaded
	// page.
	ListGenres(ctx context.Context, itemType string) ([]librarymodel.GenreCount, error)
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
	GetChannels(ctx context.Context, libraryID string, activeOnly bool) ([]*iptvmodel.Channel, error)
	GetChannel(ctx context.Context, id string) (*iptvmodel.Channel, error)
	GetGroups(ctx context.Context, libraryID string) ([]string, error)
	GetSchedule(ctx context.Context, channelID string, from, to time.Time) ([]*iptvmodel.EPGProgram, error)
	GetBulkSchedule(ctx context.Context, channelIDs []string, from, to time.Time) (map[string][]*iptvmodel.EPGProgram, error)
	NowPlaying(ctx context.Context, channelID string) (*iptvmodel.EPGProgram, error)
	RefreshM3U(ctx context.Context, libraryID string) (int, error)
	// TryAcquireRefresh + RunRefreshM3U + PublishRefreshFailed split
	// the M3U refresh so the HTTP handler can return 202 immediately
	// and run the actual import in a goroutine that survives client
	// disconnect / nginx proxy_read_timeout. See iptv_admin.go.
	TryAcquireRefresh(libraryID string) (func(), error)
	RunRefreshM3U(ctx context.Context, libraryID string) (int, error)
	PublishRefreshFailed(libraryID string, err error)
	// SpawnBackground lanza una goroutine cuyo ctx (1er argumento de
	// fn) se cancela en Service.Shutdown y se contabiliza en el WG
	// interno. Los handlers iptv_admin lo usan para no perder writes
	// si el shutdown llega durante un refresh async (audit olores
	// DD + GGGG). El ctx que recibe fn ya está atado al lifecycle
	// del service — los callers pueden envolverlo con WithTimeout.
	SpawnBackground(fn func(ctx context.Context))
	RefreshEPG(ctx context.Context, libraryID string) (int, error)
	PreflightCheck(ctx context.Context, m3uURL string, tlsInsecure bool) iptv.PreflightResult

	// Channel favorites (per-user, persisted in user_channel_favorites).
	AddFavorite(ctx context.Context, userID, channelID string) error
	RemoveFavorite(ctx context.Context, userID, channelID string) error
	IsFavorite(ctx context.Context, userID, channelID string) (bool, error)
	ListFavoriteIDs(ctx context.Context, userID string) ([]string, error)
	ListFavoriteChannels(ctx context.Context, userID string) ([]*iptvmodel.Channel, error)

	// EPG sources (per-library, multi-provider config).
	PublicEPGCatalog() []iptv.PublicEPGSource
	ListEPGSources(ctx context.Context, libraryID string) ([]*iptvmodel.LibraryEPGSource, error)
	AddEPGSource(ctx context.Context, libraryID, catalogID, customURL string) (*iptvmodel.LibraryEPGSource, error)
	RemoveEPGSource(ctx context.Context, libraryID, sourceID string) error
	ReorderEPGSources(ctx context.Context, libraryID string, orderedIDs []string) error

	// Channel health — admin surface for the opportunistic probe
	// data the stream proxy records. SetChannelActive already exists;
	// the admin UI pairs it with ResetChannelHealth so an operator
	// can either permanently disable a dead channel or clear its
	// counter if they know it's actually working.
	ListUnhealthyChannels(ctx context.Context, libraryID string, threshold int) ([]*iptvmodel.Channel, error)
	// ChannelHealthSummary is the lightweight aggregate the admin
	// Bibliotecas panel reads on first paint (counts only) so the
	// page doesn't pull every unhealthy / without-EPG row just to
	// render badges.
	ChannelHealthSummary(ctx context.Context, libraryID string) (iptvmodel.ChannelHealthSummary, error)
	SetChannelActive(ctx context.Context, id string, active bool) error
	ResetChannelHealth(ctx context.Context, channelID string) error
	// RecordProbeFailure is the same hook the proxy uses; the player
	// beacon endpoint forwards client-side fatal errors here so any
	// failure source bumps the same `consecutive_failures` counter.
	RecordProbeFailure(ctx context.Context, channelID string, err error)

	// Manual channel editing — surfaced as the "canales sin guía"
	// admin panel. The override layer makes SetChannelTvgID survive
	// the next M3U refresh.
	ListChannelsWithoutEPG(ctx context.Context, libraryID string) ([]*iptvmodel.Channel, error)
	SetChannelTvgID(ctx context.Context, channelID, tvgID string) error

	// Continue watching — per-user recently-played channel rail.
	// The beacon records (user, channel); the list query resolves to
	// current channel rows via stream_url so entries survive M3U
	// refresh cycles.
	RecordWatch(ctx context.Context, userID, channelID string) (time.Time, error)
	ListContinueWatching(ctx context.Context, userID string, limit int, accessibleLibraries map[string]bool) ([]*iptvmodel.Channel, []time.Time, error)

	// Per-user channel ordering + visibility. The overlay onto a
	// library's channel list is applied by GetChannelsForUser;
	// ReplaceChannelOrder / SetChannelVisibility / ResetChannelOrder
	// drive the personalisation panel's mutations.
	GetChannelsForUser(ctx context.Context, libraryID, userID string, activeOnly bool) ([]*iptvmodel.Channel, error)
	// GetChannelsForUserPersonalisation devuelve la vista del panel
	// /live-tv/customize: todas las channels (incluso hidden por user)
	// ordenadas con SU overlay personal aplicado, para que el panel
	// pueda renderizar la lista que el usuario está editando.
	GetChannelsForUserPersonalisation(ctx context.Context, libraryID, userID string) ([]*iptvmodel.Channel, error)
	ListChannelOverrides(ctx context.Context, userID string) ([]iptvmodel.UserChannelOrderEntry, error)
	ReplaceChannelOrder(ctx context.Context, userID string, orderedIDs []string, hiddenIDs map[string]bool) error
	SetChannelVisibility(ctx context.Context, userID, channelID string, hidden bool) error
	ResetChannelOrder(ctx context.Context, userID string) error

	// Admin channel curation. The admin overlay (library_channel_order)
	// composes BEFORE the per-user overlay in GetChannelsForUser; admin-
	// hidden channels are a hard constraint that users cannot un-hide.
	GetChannelsForLibraryAdmin(ctx context.Context, libraryID string, includeHidden bool) ([]*iptvmodel.Channel, []iptvmodel.LibraryChannelOrderEntry, error)
	ListLibraryChannelOverrides(ctx context.Context, libraryID string) ([]iptvmodel.LibraryChannelOrderEntry, error)
	ReplaceLibraryChannelOrder(ctx context.Context, libraryID string, orderedIDs []string, hiddenIDs map[string]bool) error
	SetLibraryChannelVisibility(ctx context.Context, libraryID, channelID string, hidden bool) error
	ResetLibraryChannelOrder(ctx context.Context, libraryID string) error

	// GetChannelEPGIcon devuelve el icono que el EPG haya recolectado
	// para programas de este canal (XMLTV `<icon src=...>`). Lo usa el
	// proxy /channels/{id}/logo como último fallback cuando ni hay
	// override admin ni tvg-logo en el M3U. "" sin error = no hay.
	GetChannelEPGIcon(ctx context.Context, channelID string) (string, error)

	// Admin channel logo overrides — manual replacement of the M3U
	// tvg-logo with either an external URL or an uploaded file.
	// Survives M3U refreshes (keyed by stream_url, not channel UUID).
	// SetChannelLogoFile + ClearChannelLogo return the previous
	// file basename so the handler can delete the orphaned file
	// from imageDir without a second round trip.
	SetChannelLogoURL(ctx context.Context, channelID, logoURL string) error
	SetChannelLogoFile(ctx context.Context, channelID, basename string) (previousFile string, err error)
	ClearChannelLogo(ctx context.Context, channelID string) (previousFile string, err error)
	GetChannelLogoOverride(ctx context.Context, channelID string) (*iptvmodel.ChannelLogoOverride, error)
	// RefreshLogosFromIPTVOrg busca logos en la base pública de
	// iptv-org y rellena los canales sin logo. Devuelve el número
	// de canales actualizados.
	RefreshLogosFromIPTVOrg(ctx context.Context, libraryID string) (iptv.IPTVOrgRefreshSummary, error)
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
	GetByID(ctx context.Context, id string) (*librarymodel.Item, error)
	List(ctx context.Context, filter librarymodel.ItemFilter) ([]*librarymodel.Item, int, error)
}

// MediaStreamRepository defines media stream data access needed by handlers.
type MediaStreamRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.MediaStream, error)
}

// ImageRepository defines image data access needed by handlers.
type ImageRepository interface {
	GetPrimaryURLs(ctx context.Context, itemIDs []string) (map[string]map[string]librarymodel.PrimaryImageRef, error)
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.Image, error)
	Create(ctx context.Context, img *librarymodel.Image) error
	SetPrimary(ctx context.Context, itemID, imgType, imageID string) error
	SetLocked(ctx context.Context, imageID string, locked bool) error
	GetByID(ctx context.Context, id string) (*librarymodel.Image, error)
	DeleteByID(ctx context.Context, id string) error
}

// MetadataRepository defines metadata access needed by handlers.
type MetadataRepository interface {
	GetByItemID(ctx context.Context, itemID string) (*librarymodel.Metadata, error)
	GetMetadataBatch(ctx context.Context, itemIDs []string) (map[string]*librarymodel.Metadata, error)
}

// ExternalIDsRepository defines the per-item external-id lookup
// needed by the items handler. Used to surface IMDb / TMDb / TVDB
// links in the detail response so the client can render "Open in
// IMDb" / "Open in TMDb" affordances without a second round-trip.
type ExternalIDsRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.ExternalID, error)
	// GetItemIDByExternalID is the reverse lookup used by the
	// recommendations endpoint to mark TMDb candidates that the user
	// already has locally. Returns "" when no item carries that
	// (provider, external_id) pairing.
	GetItemIDByExternalID(ctx context.Context, provider, externalID string) (string, error)
}

// PeopleRepoForItems is the per-item people lookup used by the
// items handler to fold cast/crew into the detail response.
type PeopleRepoForItems interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.ItemPersonCredit, error)
}

// CollectionRepoForItems is the per-collection lookup used by the
// items handler to surface the "Part of: X" affordance on a movie's
// detail page. nil-safe at the handler level so deployments without
// the collections feature wired keep returning the same shape.
type CollectionRepoForItems interface {
	GetByID(ctx context.Context, id string) (*librarymodel.Collection, error)
}

// ChapterRepository defines chapter data access needed by handlers.
// Optional dep: when nil, the item-detail handler simply omits the
// `chapters` field — older test environments and bare deployments
// keep working without one wired.
type ChapterRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.Chapter, error)
}

// EpisodeSegmentRepository surfaces skip-intro / skip-credits markers
// to the item handler so the playback page can render the floating
// "Saltar intro" / "Saltar créditos" buttons without a second API
// call. One row per (item_id, kind, source) — a single episode can
// carry chapter-derived AND fingerprint-derived segments in the same
// query result; the handler picks the highest-confidence row per
// kind before serialising.
type EpisodeSegmentRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]librarymodel.EpisodeSegment, error)
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

// UploadAuditLister es la mínima superficie del repo de auditoría que
// el handler /api/uploads/mine usa. Interface en vez del concreto para
// que tests pasen un fake sin DB.
type UploadAuditLister interface {
	ListByUser(ctx context.Context, userID string, limit int) ([]db.UploadAuditRow, error)
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
	Create(ctx context.Context, lib *librarymodel.Library) error
	// ListForUser returns every library the given user has explicit
	// access to. Used by handlers that need to materialise the
	// library-access set (e.g. continue-watching filter).
	ListForUser(ctx context.Context, userID string) ([]*librarymodel.Library, error)
}

// ExternalIDRepository defines external ID data access.
type ExternalIDRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.ExternalID, error)
}
