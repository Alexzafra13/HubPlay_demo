package handlers

import (
	"context"
	"net/http"
	"time"

	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/iptv"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/provider"
	providermodel "hubplay/internal/provider/model"
	"hubplay/internal/setup"
	"hubplay/internal/stream"
)

// ─── Auth service ───────────────────────────────────────────────────────────

// AuthService define las operaciones de auth que necesitan los handlers.
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

// UserService define las operaciones de usuario que necesitan los handlers.
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

// LibraryAccessService es la superficie mínima que el handler IPTV usa para
// gatear los endpoints de canal/EPG según las ACLs por-biblioteca. Definida
// aparte para que los tests puedan fakear sólo ese método sin arrastrar el
// mock gordo de LibraryService.
type LibraryAccessService interface {
	UserHasAccess(ctx context.Context, userID, libraryID string) (bool, error)
}

// ─── Stream manager ─────────────────────────────────────────────────────────

// StreamManagerService define las operaciones de streaming que necesitan los handlers.
type StreamManagerService interface {
	StartSession(ctx context.Context, req stream.StartSessionRequest) (*stream.ManagedSession, error)
	GetSession(key string) (*stream.ManagedSession, bool)
	// RestartSessionAt re-lanza el ffmpeg detrás de una sesión activa
	// para que empiece a codificar en `segmentIndex * segmentDuration`.
	// Lo usa el handler de segmentos cuando el player pide un segmento
	// lejano en el futuro que el ffmpeg actual aún no ha alcanzado.
	RestartSessionAt(key string, segmentIndex int, segmentDuration float64) error
	StopSession(key string)
	// StopSessionsByItem para toda sesión activa de (user, item) a
	// través de calidades y configs de audio. Lo usa el DELETE de
	// teardown del player para que una sola llamada libere todo el
	// conjunto que el player acumuló durante los cambios de ABR + pista de audio.
	StopSessionsByItem(userID, itemID string) int
	ActiveSessions() int
}

// IPTVStreamProxyService define las operaciones de proxy IPTV que necesitan los handlers.
type IPTVStreamProxyService interface {
	ProxyStream(ctx context.Context, w http.ResponseWriter, channelID, streamURL string) error
	ProxyURL(ctx context.Context, w http.ResponseWriter, channelID, rawURL string) error
	// VerifyProxySig valida que (canal, URL upstream) fueron firmados por el
	// propio proxy al reescribir la playlist — cierra el relay abierto (A3).
	VerifyProxySig(channelID, rawURL, sig string) bool
}

// IPTVTransmuxer es la superficie mínima que el handler de channel-stream
// necesita del gestor de sesiones MPEG-TS → HLS en vivo. El handler
// importa iptv de todos modos, pero expresar la dependencia como interface
// aquí permite a los tests inyectar un fake sin levantar procesos ffmpeg
// reales — y evita que el handler toque accidentalmente estado interno
// del manager.
type IPTVTransmuxer interface {
	// GetOrStart devuelve una sesión en vivo para el canal, lanzando un
	// nuevo proceso ffmpeg si es necesario. Bloquea hasta que la sesión
	// haya producido su primer segmento o expire el timeout del manager.
	GetOrStart(ctx context.Context, channelID, upstreamURL string) (*iptv.TransmuxSession, error)
	// Touch registra que un espectador sigue consumiendo la sesión,
	// evitando que el reaper de inactividad la mate. Devuelve
	// iptv.ErrSessionNotFound cuando la sesión ha expirado.
	Touch(channelID string) (*iptv.TransmuxSession, error)
	// JoinViewer/LeaveViewer mantienen el refcount de players activos
	// de la sesión (PB-28): el último Leave libera el slot al instante
	// en vez de esperar al idle reap — el zapping ya no agota
	// MaxSessions. Ambos son no-op para ids vacíos o sin sesión viva.
	JoinViewer(channelID, viewerID string)
	LeaveViewer(channelID, viewerID string)
}

// ─── Repository interfaces ──────────────────────────────────────────────────

// ItemRepository define el acceso a datos de items que necesitan los handlers.
type ItemRepository interface {
	GetByID(ctx context.Context, id string) (*librarymodel.Item, error)
	List(ctx context.Context, filter librarymodel.ItemFilter) ([]*librarymodel.Item, int, error)
}

// MediaStreamRepository define el acceso a datos de media streams que necesitan los handlers.
type MediaStreamRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.MediaStream, error)
}

// ImageRepository define el acceso a datos de imágenes que necesitan los handlers.
type ImageRepository interface {
	GetPrimaryURLs(ctx context.Context, itemIDs []string) (map[string]map[string]librarymodel.PrimaryImageRef, error)
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.Image, error)
	Create(ctx context.Context, img *librarymodel.Image) error
	SetPrimary(ctx context.Context, itemID, imgType, imageID string) error
	SetLocked(ctx context.Context, imageID string, locked bool) error
	GetByID(ctx context.Context, id string) (*librarymodel.Image, error)
	DeleteByID(ctx context.Context, id string) error
}

// MetadataRepository define el acceso a metadata que necesitan los handlers.
type MetadataRepository interface {
	GetByItemID(ctx context.Context, itemID string) (*librarymodel.Metadata, error)
	GetMetadataBatch(ctx context.Context, itemIDs []string) (map[string]*librarymodel.Metadata, error)
}

// ExternalIDsRepository define el lookup de external-id por-item que
// necesita el handler de items. Se usa para exponer los enlaces IMDb /
// TMDb / TVDB en la respuesta de detalle, de modo que el cliente pueda
// renderizar los affordances "Abrir en IMDb" / "Abrir en TMDb" sin un
// segundo round-trip.
type ExternalIDsRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.ExternalID, error)
	// GetItemIDByExternalID es el lookup inverso que usa el endpoint de
	// recomendaciones para marcar los candidatos TMDb que el usuario ya
	// tiene en local. Devuelve "" cuando ningún item lleva ese par
	// (provider, external_id).
	GetItemIDByExternalID(ctx context.Context, provider, externalID string) (string, error)
}

// PeopleRepoForItems es el lookup de personas por-item que usa el
// handler de items para incorporar el cast/crew a la respuesta de detalle.
type PeopleRepoForItems interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.ItemPersonCredit, error)
}

// CollectionRepoForItems es el lookup por-colección que usa el handler
// de items para exponer el affordance "Parte de: X" en la página de
// detalle de una película. nil-safe a nivel de handler para que los
// deployments sin la feature de colecciones cableada sigan devolviendo
// la misma shape.
type CollectionRepoForItems interface {
	GetByID(ctx context.Context, id string) (*librarymodel.Collection, error)
}

// ChapterRepository define el acceso a datos de capítulos que necesitan
// los handlers. Dep opcional: cuando es nil, el handler de item-detail
// simplemente omite el campo `chapters` — los entornos de test antiguos y
// los deployments básicos siguen funcionando sin uno cableado.
type ChapterRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.Chapter, error)
}

// EpisodeSegmentRepository expone los marcadores skip-intro /
// skip-credits al handler de items para que la página de reproducción
// pueda renderizar los botones flotantes "Saltar intro" / "Saltar
// créditos" sin una segunda llamada API. Una fila por (item_id, kind,
// source) — un mismo episodio puede llevar segmentos derivados de
// capítulos Y derivados de fingerprint en el mismo resultado de query;
// el handler elige la fila de mayor confianza por kind antes de serializar.
type EpisodeSegmentRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]librarymodel.EpisodeSegment, error)
}

// UserDataRepository define el acceso a user data que necesitan los handlers.
type UserDataRepository interface {
	Get(ctx context.Context, userID, itemID string) (*librarymodel.UserData, error)
	GetBatch(ctx context.Context, userID string, itemIDs []string) (map[string]*librarymodel.UserData, error)
	UpdateProgress(ctx context.Context, userID, itemID string, positionTicks int64, completed bool) error
	MarkPlayed(ctx context.Context, userID, itemID string) error
	SetFavorite(ctx context.Context, userID, itemID string, favorite bool) error
	ContinueWatching(ctx context.Context, userID string, limit int) ([]*librarymodel.ContinueWatchingItem, error)
	Favorites(ctx context.Context, userID string, limit, offset int) ([]*librarymodel.FavoriteItem, error)
	NextUp(ctx context.Context, userID string, limit int) ([]*librarymodel.NextUpItem, error)
	SeriesEpisodeProgress(ctx context.Context, userID, seriesID string) (total, watched int, err error)
	Delete(ctx context.Context, userID, itemID string) error
	ClearProgress(ctx context.Context, userID, itemID string) error
}

// ImageRefreshService corre el refresh de imágenes de toda la biblioteca.
// Definida aquí para que los handlers dependan de una interface, no del
// concreto library.ImageRefresher — mantiene mínima la superficie de
// compile-time de la capa de handlers y triviales los tests.
type ImageRefreshService interface {
	RefreshForLibrary(ctx context.Context, libraryID string) (int, error)
}

// EventBusSubscriber define la suscripción al event bus que necesitan los
// handlers. Subscribe devuelve una función de unsubscribe; los handlers
// DEBEN llamarla cuando el suscriptor desaparece (ej. desconexión de cliente
// SSE) para evitar leaks de handlers.
type EventBusSubscriber interface {
	Subscribe(eventType event.Type, handler event.Handler) func()
}

// UploadAuditLister es la mínima superficie del repo de auditoría que
// el handler /api/uploads/mine usa. Interface en vez del concreto para
// que tests pasen un fake sin DB.
type UploadAuditLister interface {
	ListByUser(ctx context.Context, userID string, limit int) ([]db.UploadAuditRow, error)
}

// EventBusPublisher es el lado publish-only del bus, usado por los
// handlers que emiten eventos pero nunca los consumen (el handler de
// progreso difunde eventos con scope de usuario a otros clientes del mismo usuario).
type EventBusPublisher interface {
	Publish(e event.Event)
}

// ─── Setup service ──────────────────────────────────────────────────────────

// SetupService define las operaciones del wizard de setup que necesitan los handlers.
type SetupService interface {
	NeedsSetup(ctx context.Context) bool
	BrowseDirectories(path string) (*setup.BrowseResult, error)
	DetectCapabilities() *setup.SystemCapabilities
	CompleteSetup(startScan bool) error
}

// ─── Provider interfaces ────────────────────────────────────────────────────

// ProviderManager define las operaciones de provider de metadata/imágenes/subtítulos.
type ProviderManager interface {
	SearchMetadata(ctx context.Context, query provider.SearchQuery) ([]provider.SearchResult, error)
	FetchMetadata(ctx context.Context, externalID string, itemType provider.ItemType) (*provider.MetadataResult, error)
	FetchImages(ctx context.Context, externalIDs map[string]string, itemType provider.ItemType) ([]provider.ImageResult, error)
	SearchSubtitles(ctx context.Context, query provider.SubtitleQuery) ([]provider.SubtitleResult, error)
	DownloadSubtitle(ctx context.Context, sourceName, fileID string) ([]byte, error)
	// FetchRecommendations alimenta el rail "más como esto" en la página
	// de detalle. Las implementaciones devuelven (nil, nil) cuando ningún
	// provider puede resolver recomendaciones para el external id dado —
	// los handlers renderizan un rail vacío en vez de un 5xx en ese caso.
	FetchRecommendations(ctx context.Context, externalID string, itemType provider.ItemType, limit int) ([]provider.RecommendationResult, error)
}

// ProviderRepository define el acceso a datos de config de providers.
type ProviderRepository interface {
	ListAll(ctx context.Context) ([]*providermodel.ProviderConfig, error)
	GetByName(ctx context.Context, name string) (*providermodel.ProviderConfig, error)
	Upsert(ctx context.Context, p *providermodel.ProviderConfig) error
}

// LibraryRepository define el acceso a datos de biblioteca para los handlers que necesitan acceso directo al repo.
type LibraryRepository interface {
	Create(ctx context.Context, lib *librarymodel.Library) error
	// ListForUser devuelve toda biblioteca a la que el usuario dado tiene
	// acceso explícito. Lo usan los handlers que necesitan materializar el
	// conjunto de library-access (ej. el filtro de continue-watching).
	ListForUser(ctx context.Context, userID string) ([]*librarymodel.Library, error)
}

// ExternalIDRepository define el acceso a datos de external IDs.
type ExternalIDRepository interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.ExternalID, error)
}
