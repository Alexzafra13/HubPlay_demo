package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"hubplay/internal/api/handlers"
	authhandler "hubplay/internal/api/handlers/auth"
	"hubplay/internal/api/handlers/media"
	"hubplay/internal/api/handlers/system"
	"hubplay/internal/api/handlers/users"
	"hubplay/internal/auth"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/federation"
	"hubplay/internal/imaging/pathmap"
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

type Dependencies struct {
	Auth          *auth.Service
	DeviceCode    *auth.DeviceCodeService
	Users         *user.Service
	Libraries     *library.Service
	StreamManager *stream.Manager
	IPTV          *iptv.Service
	IPTVProxy     *iptv.StreamProxy
	IPTVTransmux  *iptv.TransmuxManager
	IPTVLogoCache *iptv.LogoCache
	IPTVScheduler *iptv.Scheduler
	// Repos expuestos como interfaces (olor H fase 2 del audit
	// 2026-05-14): los handlers ya consumían contratos estrechos
	// localmente, así que el `*db.XRepository` concreto sólo añadía
	// "doble expresión" del contrato. Los repos concretos siguen
	// satisfaciendo estas interfaces — composition root pasa
	// `repos.X` sin cambios.
	IPTVSchedules   IPTVSchedulesRepo
	Items           ItemsRepo
	MediaStreams    MediaStreamsRepo
	Images          ImagesRepo
	Metadata        MetadataRepo
	UserData        UserDataRepo
	Chapters        ChaptersRepo
	EpisodeSegments EpisodeSegmentsRepo
	People          PeopleRepo
	Studios         StudiosRepo
	Collections     CollectionsRepo
	// CollectionImageOverrides es opcional. nil deshabilita los
	// endpoints de edición de carátula/fondo de colección con 503;
	// el listado y el detail siguen funcionando con la imagen TMDb
	// original.
	CollectionImageOverrides CollectionImageOverridesRepo
	UserPreferences          UserPreferencesRepoForDeps
	Home                     HomeRepo
	Providers                *provider.Manager
	// Scanner expone SearchCandidates + IdentifyAndApply para el flujo
	// admin de "Identify" (rematch manual contra TMDb). Opcional — si
	// nil los endpoints /items/{id}/identify devuelven 503 y el resto
	// del item handler sigue funcionando. Comparte instancia con la
	// que dispara los scans periódicos: una sola fuente de verdad para
	// la aplicación de metadatos en disco.
	Scanner      *scanner.Scanner
	ExternalIDs  ExternalIDsRepo
	LibraryRepo  LibrariesRepo
	ProviderRepo ProvidersConfigRepo
	Settings     SettingsRepo
	SetupService *setup.Service
	EventBus     *event.Bus
	Federation   *federation.Manager
	// Notifications es el inbox por usuario (migration 049). Cualquier
	// feature emite con svc.Create / FanOutToAdmins; los handlers
	// /me/notifications + el SSE de /me/events consumen. Opcional:
	// si nil, los endpoints devuelven 503 — tests que no quieren
	// notif lo pasan asi.
	Notifications *notification.Service
	// DB es el wrapper *db.Maintenance con las capacidades estrechas
	// que necesitan los handlers admin: PingContext (HealthChecker),
	// Stats (PoolStatsReporter), VacuumInto (BackupOperator) y
	// MigrationSource() (solo para el migrator sqlite→pg). Sustituye
	// al antiguo `Database *sql.DB`; cierra los olores K + T (handlers
	// no reciben raw `*sql.DB`).
	DB *db.Maintenance
	// Activity expone DailyWatchActivity + TopItems para el admin
	// SystemHandler. Sustituye las queries raw inline en system.go.
	Activity ActivityRepo
	Version  string
	// Commit es el short SHA inyectado por el linker. Se renderiza en
	// el panel system → server.commit. Vacío en dev builds.
	Commit string
	// BuildDate es la fecha de compilación (RFC3339). Vacío en dev.
	BuildDate string
	WebAssets fs.FS
	// Config es el live `*config.Config`. **Sólo** lo consumen los dos
	// handlers que mutan el fichero on-the-fly: setup wizard
	// (`SetupHandler.Config`) y panel admin DB (`AdminDBHandler`); ambos
	// llaman a `config.Save` tras editar campos del struct, así que
	// necesitan la referencia viva. El resto del router NO lee
	// `Config.X.Y` directo — se cablea por los campos primitivos
	// inmediatamente abajo (`DataDir`, `ServerBaseURL`, etc.). Cierra el
	// olor V del audit 2026-05-14 ("router lee deps.Config.* directo").
	Config *config.Config
	// Valores primitivos derivados de Config, materializados una vez en
	// composition root (`main.go`). Si `Config != nil` y un primitivo
	// está a zero, `NewRouter` los rellena desde Config como
	// retro-compat (tests minimalistas que sólo pasan `Config: cfg`).
	MetricsEnabled bool
	MetricsPath    string
	AuthConfig     config.AuthConfig
	// DataDir es el directorio padre de la DB. Source of truth para
	// imageDir + fedImageDir + trickplayDir, que se derivan colgando
	// "images" / "images/trickplay" debajo.
	DataDir        string
	DatabasePath   string
	DatabaseDriver string
	ServerAddr     string
	ServerBaseURL  string
	ServerPort     int
	MDNSEnabled    bool
	MDNSHostname   string
	HWAccelDefault config.HWAccelConfig
	// AllowedOrigins es la lista estática CORS desde el YAML. Sólo se
	// usa cuando `CorsRegistry == nil` (tests minimalistas o flag de
	// compatibilidad); en producción el middleware dynamic lo consulta
	// con statics+dynamics combinados.
	AllowedOrigins []string
	// TrustedProxies es la lista de CIDRs que pertenecen a proxies de
	// confianza delante del server. Wirea el client-IP middleware:
	//   - lista vacía ⇒ no se honra X-Forwarded-For (client IP =
	//     RemoteAddr de la conexión TCP; seguro si el server está
	//     expuesto directo a Internet).
	//   - 1+ CIDRs ⇒ ClientIPFromXFF camina XFF saltándose entradas
	//     dentro de esos prefixes; la primera no-trusted es el cliente.
	// Cierra la migración de middleware.RealIP (deprecated en chi
	// v5.3.0 por 3 advisories de IP spoofing — GHSA-3fxj-6jh8-hvhx,
	// GHSA-rjr7-jggh-pgcp, GHSA-9g5q-2w5x-hmxf).
	TrustedProxies []string
	Logger         *slog.Logger
	Metrics        *observability.Metrics
	// LogBuffer is the in-memory ring the admin "Logs" surface
	// tails. Optional — tests pass nil and the admin /logs
	// endpoint short-circuits to "logs not available" rather than
	// 500. Production builds wire it up via logging.NewWithBuffer.
	LogBuffer *logging.Buffer
	// SSELimiter bounds concurrent Server-Sent Events connections
	// across all SSE surfaces (events, me_events, admin_logs). Optional
	// — tests pass nil and handlers skip enforcement; production wires
	// a single shared instance so global + per-user counts are unified.
	SSELimiter *handlers.SSELimiter
	// HostMetrics samples host-level introspection (CPU%, RAM, GPU
	// model). Optional — tests pass nil and the admin /system/stats
	// response carries a zero-value host section, which the panel
	// renders as dashes. Production wires a single instance, started
	// at boot, lifetime bound to the process context.
	HostMetrics *sysmetrics.Sampler
	// ConfigPath is the absolute path to hubplay.yaml. Used by the
	// admin Database panel to persist driver / DSN changes via
	// config.Save without making the handler re-derive the path
	// from the binary args. Empty in tests; production sets it.
	ConfigPath string
	// RestartRequester triggers a graceful self-shutdown when the
	// admin DB panel or wizard saves a new driver. nil-safe — the
	// handlers degrade to "saved, restart manually" when missing.
	RestartRequester *config.RestartRequester
	// Uploads sirve el protocolo tus + endpoints custom de uploads
	// (PR2 feature upload). nil-safe: si está apagado en config, el
	// binario arranca sin él y las rutas /api/v1/uploads* simplemente
	// no se montan (cliente recibe 404).
	Uploads      http.Handler
	UploadsAudit handlers.UploadAuditLister
	// Permissions enforza los flags granulares de admin (migración 055).
	// nil = router cae al gate de RequireAdmin para todo (comportamiento
	// pre-migración); en producción se pasa siempre y los endpoints
	// owner-only + can_manage_admins lo aprovechan.
	Permissions *auth.PermissionChecker
	// UserRepo expone GetByID + SetPermission + TransferOwnership al
	// PermissionsHandler. Interface estrecha definida en el paquete
	// handlers; el *db.UserRepository concreto la satisface.
	UserRepo handlers.PermissionsStore
	// CorsRegistry — combinador atómico statics(YAML) + dynamics(DB)
	// que el middleware CORS consulta en cada preflight. nil = router
	// cae al handler estático de chi-cors con la lista del YAML
	// (comportamiento pre-PR4).
	CorsRegistry *CorsRegistry
	// CorsOriginsRepo expone List/Insert/Delete/ListOrigins al handler
	// del panel admin de CORS. nil = los endpoints /admin/cors-origins
	// no se montan.
	CorsOriginsRepo handlers.CorsOriginStore
	// AuditLog expone Query + DistinctEventTypes al panel admin de
	// auditoría (PR5). nil = los endpoints /admin/audit-log no se
	// montan (tests minimalistas).
	AuditLog handlers.AuditLogStore
	// Audit es el productor de eventos al audit log unificado (PR5).
	// nil-safe en los handlers (caen a un sink no-op).
	Audit handlers.AuditEmitter
	// Updates expone el estado del update checker al panel admin. nil
	// deja los endpoints /admin/system/updates devolviendo "feature
	// no disponible" en lugar de 500. Esperado en dev builds.
	Updates handlers.UpdatesProvider
}

// NewRouter compone el chi.Router de toda la API. La función mantiene
// dos responsabilidades:
//
//  1. Configurar el middleware stack global + el endpoint /metrics
//     fuera de /api/v1 (Prometheus scrapers esperan top-level).
//  2. Construir los handlers compartidos por varios mountXxx
//     (authHandler, userHandler, healthHandler, deviceHandler,
//     fedImgSrv) y delegar el registro de rutas a las funciones
//     mountXxx — cada una en su fichero `mount_*.go`.
//
// El cuerpo de cada mountXxx vivía antes embebido en el callback
// monolítico `r.Route("/api/v1", ...)` (~1100 LoC). El split por
// feature (olor H del audit 2026-05-14) deja NewRouter en ~80 LoC y
// cada mount con un solo motivo de cambio.
func NewRouter(deps Dependencies) http.Handler {
	// Retro-compat con tests minimalistas que sólo pasan `Config: cfg`
	// — si los campos primitivos no llegan rellenados, los derivamos
	// aquí desde Config. main.go los pasa siempre explícitos (path
	// idiomático). Una vez derivados, ningún sitio downstream lee
	// `deps.Config.X.Y` excepto los dos handlers que mutan el fichero
	// (setup wizard + admin DB). Cierra olor V del audit 2026-05-14.
	deps.fillFromConfig()

	r := chi.NewRouter()

	// Wire the observability hook into the handlers package so every
	// rendered AppError gets counted. Kept out of NewRouter's return
	// path so tests that never pass Metrics stay on the no-op recorder.
	if deps.Metrics != nil {
		handlers.SetErrorRecorder(func(code string) {
			deps.Metrics.HTTPErrors.WithLabelValues(code).Inc()
		})
	}

	applyGlobalMiddleware(r, deps)
	mountMetricsEndpoint(r, deps)

	// Handlers compartidos por varios mountXxx — construidos una vez.
	authHandler := authhandler.NewAuthHandler(deps.Auth, deps.Users, deps.Libraries, deps.AuthConfig, deps.Audit, deps.Logger)
	userHandler := users.NewUserHandler(deps.Users, deps.Libraries, deps.Audit, deps.Logger)

	// Avoid wrapping a nil concrete pointer in a non-nil interface.
	var streamSvc handlers.StreamManagerService
	if deps.StreamManager != nil {
		streamSvc = deps.StreamManager
	}
	healthHandler := system.NewHealthHandler(deps.DB, streamSvc, deps.Version, deps.DatabasePath)

	// Device auth handler: construido aquí porque vive en dos mounts
	// distintos — start/poll/events públicos (mountAuthPublic) y
	// approve auth-gated (mountAuthProtected). Stateless internamente
	// — la sesión y los códigos viven en deps.DeviceCode.
	var deviceHandler *authhandler.DeviceAuthHandler
	if deps.DeviceCode != nil {
		deviceHandler = authhandler.NewDeviceAuthHandler(
			deps.DeviceCode, nil, deps.AuthConfig, deps.EventBus, deps.SSELimiter, deps.Logger)
	}

	// Image handler is constructed early so the federation peer surface
	// (under /api/v1/peer/*, mounted BEFORE the user-auth middleware
	// group below) can reuse the same path-mapping store + thumbnail
	// cache as the local /images/file/{id} endpoint. Both share one
	// instance and stay perfectly cache-coherent.
	var (
		fedImgSrv   *media.ImageHandler
		fedImageDir string
	)
	if deps.DB != nil && deps.DataDir != "" && deps.Images != nil && deps.ExternalIDs != nil && deps.Items != nil && deps.Providers != nil {
		fedImageDir = filepath.Join(deps.DataDir, "images")
		fedImgSrv = media.NewImageHandler(
			deps.Images, deps.ExternalIDs, deps.Items, deps.Providers,
			library.NewImageRefresher(
				deps.Items, deps.ExternalIDs, deps.Images, deps.Providers,
				pathmap.New(fedImageDir), fedImageDir, deps.Logger,
			),
			fedImageDir, deps.Audit, deps.Logger,
		)
	}

	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no auth required).
		mountHealthAndOpenAPI(r, healthHandler)
		mountAuthPublic(r, authHandler, deviceHandler)
		mountSetupWizard(r, deps)
		mountFederationPublic(r, deps, fedImgSrv)

		// Protected routes (deps.Auth.Middleware enforza sesión).
		r.Group(func(r chi.Router) {
			r.Use(deps.Auth.Middleware)

			mountAuthProtected(r, authHandler, deviceHandler)
			mountSSEEvents(r, deps)
			mountUploads(r, deps)
			mountMeIdentity(r, authHandler, userHandler)
			mountMeNotificationsAndPreferences(r, deps)
			mountUsers(r, authHandler, userHandler, deps)
			mountAdminAuthAndFederation(r, deps)
			mountAdminSystem(r, deps)
			mountWatchProgress(r, deps)
			mountHome(r, deps)
			mountStreaming(r, deps)
			mountLibrariesItemsAndIPTV(r, deps, fedImageDir)
			mountImagesPeopleStudiosCollections(r, deps, fedImgSrv, fedImageDir)
			mountProviders(r, deps)
		})
	})

	// Serve embedded web frontend (SPA fallback).
	if deps.WebAssets != nil {
		mountSPAFallback(r, deps.WebAssets)
	}

	return r
}

// applyGlobalMiddleware monta el stack que envuelve toda la API: client
// IP, request id, logging, recoverer, security headers, métricas
// (cuando wired), CORS (con preferencia por el middleware dynamic) y
// CSRF. El orden importa — security headers después de Recoverer así
// que un panic que devuelva 500 sigue cargando headers; antes de CORS
// así que la misma capa aplica a preflights.
//
// Client-IP middleware: usa `ClientIPFromXFF(deps.TrustedProxies...)`
// si el operador ha declarado proxies de confianza, sino
// `ClientIPFromRemoteAddr`. Reemplaza `middleware.RealIP` que se
// deprecó en chi v5.3.0 por IP spoofing (3 CVE incl. Critical 9.3).
// La nueva API NO muta r.RemoteAddr — el IP va en el ctx; los
// handlers lo leen con `handlers.ClientIP(r)` (helper local que cae
// a r.RemoteAddr si el middleware no lo dejó set).
func applyGlobalMiddleware(r chi.Router, deps Dependencies) {
	if len(deps.TrustedProxies) > 0 {
		r.Use(middleware.ClientIPFromXFF(normalizeCIDRs(deps.TrustedProxies)...))
	} else {
		r.Use(middleware.ClientIPFromRemoteAddr)
	}
	r.Use(middleware.RequestID)
	r.Use(RequestLogger(deps.Logger))
	r.Use(middleware.Recoverer)
	r.Use(SecurityHeaders())
	if deps.Metrics != nil {
		r.Use(deps.Metrics.MetricsMiddleware)
	}
	// CORS middleware. Si deps.CorsRegistry está cableado (caso
	// default post-PR4-CORS-dynamic), usamos el middleware custom
	// que combina statics del YAML + dynamics del DB con un
	// atomic.Pointer; el panel admin puede añadir/quitar orígenes
	// sin restart. Si NO está (tests minimalistas o flag de
	// compatibilidad), caemos al handler estático de chi-cors con
	// la lista del YAML.
	corsMethods := []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	corsAllowedHeaders := []string{
		"Authorization", "Content-Type", "X-CSRF-Token",
		"Tus-Resumable", "Upload-Length", "Upload-Offset",
		"Upload-Metadata", "Upload-Concat", "Upload-Defer-Length",
		"Upload-Checksum",
	}
	corsExposedHeaders := []string{
		"Retry-After",
		"Location", "Tus-Resumable", "Tus-Version", "Tus-Extension",
		"Tus-Max-Size", "Upload-Offset", "Upload-Length",
	}
	if deps.CorsRegistry != nil {
		r.Use(CorsMiddleware(deps.CorsRegistry, corsMethods, corsAllowedHeaders, corsExposedHeaders, true, 300))
	} else {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   deps.AllowedOrigins,
			AllowedMethods:   corsMethods,
			AllowedHeaders:   corsAllowedHeaders,
			ExposedHeaders:   corsExposedHeaders,
			AllowCredentials: true,
			MaxAge:           300,
		}))
	}
	r.Use(CSRFProtect)
}

// mountMetricsEndpoint registra el /metrics handler de Prometheus en
// top-level (fuera de /api/v1) — los scrapers esperan path raíz, y la
// convención es dejarlo sin auth para que el operador lo proteja en su
// reverse proxy si desea.
func mountMetricsEndpoint(r chi.Router, deps Dependencies) {
	if deps.Metrics == nil || !deps.MetricsEnabled {
		return
	}
	path := deps.MetricsPath
	if path == "" {
		path = "/metrics"
	}
	r.Handle(path, deps.Metrics.Handler())
}

// mountSPAFallback sirve el frontend embebido. Intenta servir el path
// exacto primero (JS, CSS, images, etc.); si fs.Stat no lo encuentra,
// cae a index.html para que el routing client-side de la SPA tome el
// control. Se monta SIEMPRE al final del router para no eclipsar
// rutas /api/v1.
func mountSPAFallback(r chi.Router, assets fs.FS) {
	fileServer := http.FileServer(http.FS(assets))
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		path := strings.TrimPrefix(req.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(assets, path); err == nil {
			fileServer.ServeHTTP(w, req)
			return
		}
		// SPA fallback: serve index.html para todas las demás rutas.
		req.URL.Path = "/"
		fileServer.ServeHTTP(w, req)
	})
}

// fillFromConfig rellena los campos primitivos de Dependencies desde el
// `*config.Config` cuando vienen a zero — pensado para tests que sólo
// pasan `Config: cfg`. main.go los pasa siempre explícitos. Si `Config`
// es nil, deja todo como está (los tests que ni siquiera pasan Config
// ya no tocan ninguna ruta config-dependiente). Cierra olor V del
// audit 2026-05-14.
func (deps *Dependencies) fillFromConfig() {
	cfg := deps.Config
	if cfg == nil {
		return
	}
	if !deps.MetricsEnabled {
		deps.MetricsEnabled = cfg.Observability.MetricsEnabled
	}
	if deps.MetricsPath == "" {
		deps.MetricsPath = cfg.Observability.MetricsPath
	}
	if deps.AuthConfig == (config.AuthConfig{}) {
		deps.AuthConfig = cfg.Auth
	}
	if deps.DatabasePath == "" {
		deps.DatabasePath = cfg.Database.Path
	}
	if deps.DataDir == "" && cfg.Database.Path != "" {
		deps.DataDir = filepath.Dir(cfg.Database.Path)
	}
	if deps.DatabaseDriver == "" {
		deps.DatabaseDriver = cfg.Database.Driver
	}
	if deps.ServerAddr == "" {
		deps.ServerAddr = cfg.Server.Addr()
	}
	if deps.ServerBaseURL == "" {
		deps.ServerBaseURL = cfg.Server.BaseURL
	}
	if deps.ServerPort == 0 {
		deps.ServerPort = cfg.Server.Port
	}
	if !deps.MDNSEnabled {
		deps.MDNSEnabled = cfg.MDNS.Enabled
	}
	if deps.MDNSHostname == "" {
		deps.MDNSHostname = cfg.MDNS.Hostname
	}
	if deps.HWAccelDefault == (config.HWAccelConfig{}) {
		deps.HWAccelDefault = cfg.Streaming.HWAccel
	}
	if deps.AllowedOrigins == nil {
		deps.AllowedOrigins = allowedOrigins(cfg)
	}
}

// allowedOrigins builds the CORS origin list from config.
// In production: only the configured BaseURL.
// Always allows common local dev origins for the Vite dev server.
func allowedOrigins(cfg *config.Config) []string {
	origins := []string{
		"http://localhost:5173",
		"http://127.0.0.1:5173",
		"http://localhost:8096",
		"http://127.0.0.1:8096",
	}
	if cfg != nil && cfg.Server.BaseURL != "" {
		origins = append(origins, strings.TrimRight(cfg.Server.BaseURL, "/"))
	}
	return origins
}

// normalizeCIDRs ensures every entry is valid CIDR notation. Plain IP
// addresses (no '/') get a /32 (IPv4) or /128 (IPv6) suffix so that
// chi's ClientIPFromXFF — which calls netip.MustParsePrefix — doesn't panic.
func normalizeCIDRs(raw []string) []string {
	out := make([]string, len(raw))
	for i, s := range raw {
		if strings.Contains(s, "/") {
			out[i] = s
			continue
		}
		addr, err := netip.ParseAddr(s)
		if err != nil {
			out[i] = s
			continue
		}
		if addr.Is4() {
			out[i] = s + "/32"
		} else {
			out[i] = s + "/128"
		}
	}
	return out
}
