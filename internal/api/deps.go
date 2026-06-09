package api

import (
	"io/fs"
	"log/slog"
	"net/http"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/federation"
	"hubplay/internal/iptv"
	"hubplay/internal/library"
	"hubplay/internal/logging"
	"hubplay/internal/notification"
	"hubplay/internal/observability"
	"hubplay/internal/provider"
	"hubplay/internal/scanner"
	"hubplay/internal/setup"
	"hubplay/internal/stream"
	"hubplay/internal/sysmetrics"
	"hubplay/internal/user"
)

// Este fichero agrupa los campos de `Dependencies` en sub-structs por
// dominio (infra, server, auth, catalog, streaming, iptv, federation,
// providers, admin, setup, uploads). Sustituye la struct monolítica
// de ~70 campos del audit 2026-05-14 (olor MM) por un grafo navegable:
// `deps.IPTV.Service` en lugar de `deps.IPTV` + `deps.IPTVProxy` +
// `deps.IPTVTransmux` + ...
//
// El composition root (cmd/hubplay/main.go) construye cada sub-struct
// explícitamente; el router y los mountXxx leen siempre via path
// anidado para que `gopls find-references` sobre un sub-struct
// devuelva exactamente quién depende de él.

// InfraDeps es el subset de cross-cutting infra (logger, métricas,
// event bus, audit, notificaciones, build metadata, assets web). Casi
// todos son opcionales — los handlers caen a no-op o devuelven 503
// cuando una pieza no está cableada.
type InfraDeps struct {
	Logger        *slog.Logger
	Metrics       *observability.Metrics
	EventBus      *event.Bus
	Audit         handlers.AuditEmitter
	AuditLog      handlers.AuditLogStore
	LogBuffer     *logging.Buffer
	HostMetrics   *sysmetrics.Sampler
	SSELimiter    *handlers.SSELimiter
	Notifications *notification.Service
	Version       string
	Commit        string
	BuildDate     string
	WebAssets     fs.FS
}

// ServerDeps captura todo lo relativo a HTTP/red/CORS/proxy-trust + la
// config live. Es la "envoltura" del binario — paths, puertos, mdns,
// proxies de confianza, hwaccel defaults, CORS registry. `Config` y
// `ConfigPath` sólo los consumen los handlers que MUTAN el fichero
// (setup wizard + panel admin DB); el resto del router lee los
// primitivos materializados (DataDir, ServerBaseURL, etc.) que main.go
// pasa explícitos.
type ServerDeps struct {
	Config           *config.Config
	ConfigPath       string
	AuthConfig       config.AuthConfig
	DataDir          string
	DatabasePath     string
	DatabaseDriver   string
	ServerAddr       string
	ServerBaseURL    string
	ServerPort       int
	MDNSEnabled      bool
	MDNSHostname     string
	HWAccelDefault   config.HWAccelConfig
	AllowedOrigins   []string
	TrustedProxies   []string
	MetricsEnabled   bool
	MetricsPath      string
	MetricsToken     string
	PprofEnabled     bool
	CorsRegistry     *CorsRegistry
	CorsOriginsRepo  handlers.CorsOriginStore
	RestartRequester *config.RestartRequester
}

// AuthDeps agrupa autenticación, usuarios y permisos. `Permissions` y
// `UserRepo` son nil-safe (en tests minimalistas se cae a
// auth.RequireAdmin para todo).
type AuthDeps struct {
	Auth        *auth.Service
	DeviceCode  *auth.DeviceCodeService
	Users       *user.Service
	UserRepo    handlers.PermissionsStore
	Permissions *auth.PermissionChecker
}

// CatalogDeps reúne biblioteca + repos relacionados con contenido +
// scanner. Es el bloque más voluminoso (movies, series, episodios,
// imágenes, metadatos, capítulos, segmentos, external IDs, gente,
// estudios, colecciones, user-data, home, preferencias por usuario).
type CatalogDeps struct {
	Libraries                *library.Service
	LibraryRepo              LibrariesRepo
	Items                    ItemsRepo
	MediaStreams             MediaStreamsRepo
	Images                   ImagesRepo
	Metadata                 MetadataRepo
	Chapters                 ChaptersRepo
	EpisodeSegments          EpisodeSegmentsRepo
	ExternalIDs              ExternalIDsRepo
	People                   PeopleRepo
	Studios                  StudiosRepo
	Collections              CollectionsRepo
	CollectionImageOverrides CollectionImageOverridesRepo
	UserData                 UserDataRepo
	Home                     HomeRepo
	UserPreferences          UserPreferencesRepoForDeps
	Scanner                  *scanner.Scanner
}

// StreamingDeps es el stream manager (transcoding HLS / direct play /
// direct stream). nil deshabilita /stream/* completo.
type StreamingDeps struct {
	StreamManager *stream.Manager
}

// IPTVDeps agrupa todo Live TV: service principal, proxy de stream,
// transmux MPEG-TS→HLS, logo cache, scheduler de refresh M3U/EPG y el
// repo del schedule.
type IPTVDeps struct {
	Service   *iptv.Service
	Proxy     *iptv.StreamProxy
	Transmux  *iptv.TransmuxManager
	LogoCache *iptv.LogoCache
	Scheduler *iptv.Scheduler
	Schedules IPTVSchedulesRepo
}

// FederationDeps es el manager de peer-to-peer sharing. nil = todo el
// surface /federation/* + /peer/* + /me/peers/* + /admin/peers/* no se
// monta.
type FederationDeps struct {
	Manager *federation.Manager
}

// ProvidersDeps reúne el manager de providers de metadatos (TMDb,
// Fanart, OpenSubtitles) + el repo de su config admin.
type ProvidersDeps struct {
	Manager *provider.Manager
	Repo    ProvidersConfigRepo
}

// AdminDeps son recursos admin-only: wrapper de DB para ops de mantenimiento
// (backup, ping, vacuum), repo de Activity para el dashboard, repo de
// settings y el provider del update checker.
type AdminDeps struct {
	DB       *db.Maintenance
	Activity ActivityRepo
	Settings SettingsRepo
	Updates  handlers.UpdatesProvider
}

// SetupDeps es el wizard de primer arranque. nil cuando el setup ya
// está completo — todas las rutas /setup/* devuelven 404.
type SetupDeps struct {
	Service *setup.Service
}

// UploadsDeps es el handler tus + el audit log de subidas. Ambos
// requeridos juntos (si uno es nil el surface /uploads/* no se monta).
type UploadsDeps struct {
	Handler http.Handler
	Audit   handlers.UploadAuditLister
}
