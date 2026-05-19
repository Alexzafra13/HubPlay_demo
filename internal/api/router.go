package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
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
	Auth           *auth.Service
	DeviceCode     *auth.DeviceCodeService
	Users          *user.Service
	Libraries      *library.Service
	StreamManager  *stream.Manager
	IPTV           *iptv.Service
	IPTVProxy      *iptv.StreamProxy
	IPTVTransmux   *iptv.TransmuxManager
	IPTVLogoCache  *iptv.LogoCache
	IPTVScheduler  *iptv.Scheduler
	IPTVSchedules  *db.IPTVScheduleRepository
	Items          *db.ItemRepository
	MediaStreams    *db.MediaStreamRepository
	Images         *db.ImageRepository
	Metadata       *db.MetadataRepository
	UserData       *db.UserDataRepository
	Chapters        *db.ChapterRepository
	EpisodeSegments *db.EpisodeSegmentRepository
	People          *db.PeopleRepository
	Studios        *db.StudioRepository
	Collections    *db.CollectionRepository
	// CollectionImageOverrides es opcional. nil deshabilita los
	// endpoints de edición de carátula/fondo de colección con 503;
	// el listado y el detail siguen funcionando con la imagen TMDb
	// original.
	CollectionImageOverrides *db.CollectionImageOverrideRepository
	UserPreferences *db.UserPreferenceRepository
	Home            *db.HomeRepository
	Providers      *provider.Manager
	// Scanner expone SearchCandidates + IdentifyAndApply para el flujo
	// admin de "Identify" (rematch manual contra TMDb). Opcional — si
	// nil los endpoints /items/{id}/identify devuelven 503 y el resto
	// del item handler sigue funcionando. Comparte instancia con la
	// que dispara los scans periódicos: una sola fuente de verdad para
	// la aplicación de metadatos en disco.
	Scanner        *scanner.Scanner
	ExternalIDs    *db.ExternalIDRepository
	LibraryRepo    *db.LibraryRepository
	ProviderRepo   *db.ProviderRepository
	Settings       *db.SettingsRepository
	SetupService   *setup.Service
	EventBus       *event.Bus
	Federation     *federation.Manager
	// Notifications es el inbox por usuario (migration 049). Cualquier
	// feature emite con svc.Create / FanOutToAdmins; los handlers
	// /me/notifications + el SSE de /me/events consumen. Opcional:
	// si nil, los endpoints devuelven 503 — tests que no quieren
	// notif lo pasan asi.
	Notifications  *notification.Service
	// DB es el wrapper *db.Maintenance con las capacidades estrechas
	// que necesitan los handlers admin: PingContext (HealthChecker),
	// Stats (PoolStatsReporter), VacuumInto (BackupOperator) y
	// MigrationSource() (solo para el migrator sqlite→pg). Sustituye
	// al antiguo `Database *sql.DB`; cierra los olores K + T (handlers
	// no reciben raw `*sql.DB`).
	DB             *db.Maintenance
	// Activity expone DailyWatchActivity + TopItems para el admin
	// SystemHandler. Sustituye las queries raw inline en system.go.
	Activity       *db.ActivityRepository
	Version        string
	WebAssets      fs.FS
	Config         *config.Config
	Logger         *slog.Logger
	Metrics        *observability.Metrics
	// LogBuffer is the in-memory ring the admin "Logs" surface
	// tails. Optional — tests pass nil and the admin /logs
	// endpoint short-circuits to "logs not available" rather than
	// 500. Production builds wire it up via logging.NewWithBuffer.
	LogBuffer      *logging.Buffer
	// SSELimiter bounds concurrent Server-Sent Events connections
	// across all SSE surfaces (events, me_events, admin_logs). Optional
	// — tests pass nil and handlers skip enforcement; production wires
	// a single shared instance so global + per-user counts are unified.
	SSELimiter     *handlers.SSELimiter
	// HostMetrics samples host-level introspection (CPU%, RAM, GPU
	// model). Optional — tests pass nil and the admin /system/stats
	// response carries a zero-value host section, which the panel
	// renders as dashes. Production wires a single instance, started
	// at boot, lifetime bound to the process context.
	HostMetrics    *sysmetrics.Sampler
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
	Uploads        http.Handler
	UploadsAudit   handlers.UploadAuditLister
	// Permissions enforza los flags granulares de admin (migración 055).
	// nil = router cae al gate de RequireAdmin para todo (comportamiento
	// pre-migración); en producción se pasa siempre y los endpoints
	// owner-only + can_manage_admins lo aprovechan.
	Permissions    *auth.PermissionChecker
	// UserRepo expone GetByID + SetPermission + TransferOwnership al
	// PermissionsHandler. Interface estrecha definida en el paquete
	// handlers; el *db.UserRepository concreto la satisface.
	UserRepo       handlers.PermissionsStore
}

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	// Wire the observability hook into the handlers package so every rendered
	// AppError gets counted. Kept out of NewRouter's return path so tests
	// that never pass Metrics stay on the no-op recorder.
	if deps.Metrics != nil {
		handlers.SetErrorRecorder(func(code string) {
			deps.Metrics.HTTPErrors.WithLabelValues(code).Inc()
		})
	}

	// Middleware stack (order matters).
	//
	// Metrics goes after RequestID so traces and counters share the same id,
	// and after Recoverer so a panic still records a 500 request. It must
	// wrap the router so RoutePattern is populated by the time we read it.
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(RequestLogger(deps.Logger))
	r.Use(middleware.Recoverer)
	// Security response headers (CSP, X-Frame-Options, HSTS, …). Placed
	// after Recoverer so even a 500 from a panicking handler still ships
	// with the headers; placed before CORS so the same headers apply to
	// preflight responses without CORS overwriting them.
	r.Use(SecurityHeaders())
	if deps.Metrics != nil {
		r.Use(deps.Metrics.MetricsMiddleware)
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins(deps.Config),
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Retry-After"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(CSRFProtect)

	// Prometheus /metrics endpoint. Mounted outside /api/v1 because metrics
	// scrapers expect a top-level path; kept unauthenticated by convention,
	// operators are expected to protect it at the reverse proxy if desired.
	if deps.Metrics != nil && deps.Config != nil && deps.Config.Observability.MetricsEnabled {
		path := deps.Config.Observability.MetricsPath
		if path == "" {
			path = "/metrics"
		}
		r.Handle(path, deps.Metrics.Handler())
	}

	// Handlers
	authHandler := handlers.NewAuthHandler(deps.Auth, deps.Users, deps.Libraries, deps.Config.Auth, deps.Logger)
	userHandler := handlers.NewUserHandler(deps.Users, deps.Libraries, deps.Logger)

	// Avoid wrapping a nil concrete pointer in a non-nil interface.
	var streamSvc handlers.StreamManagerService
	if deps.StreamManager != nil {
		streamSvc = deps.StreamManager
	}
	healthHandler := handlers.NewHealthHandler(deps.DB, streamSvc, deps.Version, deps.Config.Database.Path)

	// Image handler is constructed early so the federation peer
	// surface (under /api/v1/peer/*, mounted BEFORE the user-auth
	// middleware group below) can reuse the same path-mapping store +
	// thumbnail cache as the local /images/file/{id} endpoint. The
	// local route is still registered down inside the auth-protected
	// group; this just lifts the constructor out so both share one
	// instance and stay perfectly cache-coherent.
	var (
		fedImgSrv   *handlers.ImageHandler
		fedImageDir string
	)
	if deps.DB != nil && deps.Config != nil && deps.Images != nil && deps.ExternalIDs != nil && deps.Items != nil && deps.Providers != nil {
		fedImageDir = filepath.Join(filepath.Dir(deps.Config.Database.Path), "images")
		fedImgSrv = handlers.NewImageHandler(
			deps.Images, deps.ExternalIDs, deps.Items, deps.Providers,
			library.NewImageRefresher(
				deps.Items, deps.ExternalIDs, deps.Images, deps.Providers,
				pathmap.New(fedImageDir), fedImageDir, deps.Logger,
			),
			fedImageDir, deps.Logger,
		)
	}

	// Public routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health check (no auth).
		// /health/live → process up, never touches deps. Kubernetes
		//   liveness probes go here so a flaky DB does not get healthy
		//   pods restarted in a loop.
		// /health/ready → DB ping, returns 503 when deps are down so
		//   load balancers drain traffic away from a broken backend.
		// /health → legacy combined endpoint, mirrors /ready status code
		//   plus rich body (ffmpeg, memory, streams) for the admin UI.
		r.Get("/health", healthHandler.Health)
		r.Get("/health/live", healthHandler.Live)
		r.Get("/health/ready", healthHandler.Ready)

		// OpenAPI 3.0.3 spec — public on purpose. Clients (Kotlin TV,
		// integration scripts, openapi-generator) fetch this before
		// they can authenticate, and the document itself contains no
		// secrets. ETag-aware so a polling client doesn't transfer the
		// body more than once per build.
		openapiHandler := handlers.NewOpenAPIHandler()
		r.Get("/openapi.yaml", openapiHandler.ServeYAML)
		r.Head("/openapi.yaml", openapiHandler.ServeYAML)

		// Auth (no auth required)
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/refresh", authHandler.Refresh)

		// Device authorization grant (RFC 8628). Two unauthenticated
		// endpoints (start + poll) for headless clients (TV apps, CLI
		// tools); the approve endpoint is gated by the auth middleware
		// down below.
		var deviceHandler *handlers.DeviceAuthHandler
		if deps.DeviceCode != nil {
			deviceHandler = handlers.NewDeviceAuthHandler(
				deps.DeviceCode, nil, deps.Config.Auth, deps.EventBus, deps.SSELimiter, deps.Logger)
			r.Post("/auth/device/start", deviceHandler.Start)
			r.Post("/auth/device/poll", deviceHandler.Poll)
			if deviceHandler.HasEventBus() {
				// SSE — the in-app pairing UI (QR + user_code) reacts
				// instantly to approval instead of polling /poll.
				r.Get("/auth/device/events", deviceHandler.Events)
			}
		}

		// Setup — create first admin (only works when no users exist)
		r.Post("/auth/setup", authHandler.Setup)

		// Setup wizard (no auth for status, auth handled per-step)
		if deps.SetupService != nil {
			setupHandler := handlers.NewSetupHandler(handlers.SetupHandlerConfig{
				Setup:     deps.SetupService,
				DBSaver:   deps.SetupService,
				Auth:      deps.Auth,
				Libraries: deps.Libraries,
				Users:     deps.Users,
				Providers: deps.ProviderRepo,
				Config:    deps.Config,
				Restart:   deps.RestartRequester,
				Logger:    deps.Logger,
			})

			r.Get("/setup/status", setupHandler.Status)
			r.Get("/setup/capabilities", setupHandler.Capabilities)
			r.Get("/setup/browse", setupHandler.Browse)
			r.Post("/setup/libraries", setupHandler.CreateLibraries)
			r.Post("/setup/settings", setupHandler.UpdateSettings)
			r.Post("/setup/complete", setupHandler.Complete)
			// Step 0 — database driver selector. Lets the operator
			// pick SQLite or Postgres before any data has landed so
			// the rest of the wizard creates rows in the chosen
			// backend, not the YAML default.
			r.Get("/setup/db/profiles", setupHandler.DatabaseProfiles)
			r.Post("/setup/db/test", setupHandler.TestDatabase)
			r.Post("/setup/db", setupHandler.SaveDatabase)
		}

		// Federation public surface. Two flavours:
		//
		//   1. Truly unauthenticated — /federation/info and /peer/handshake.
		//      The handshake authenticates by invite code in the body;
		//      info is intentionally public so a peer can fetch our
		//      identity before pairing.
		//
		//   2. Peer-authenticated — anything else under /peer/* is gated
		//      by the RequirePeerJWT middleware (Ed25519-signed JWT,
		//      issuer pinned to a paired peer, audience = our server_uuid).
		//      The same middleware applies the per-peer rate limit and
		//      records every request in the audit log.
		if deps.Federation != nil {
			pubFed := handlers.NewFederationPublicHandler(deps.Federation, deps.Logger)
			r.Get("/federation/info", pubFed.ServerInfo)
			// Foto del servidor — público a propósito: los peers la
			// consumen sin firmar JWT desde el avatar_image_url que
			// reciben en /federation/info.
			r.Get("/federation/identity/avatar", pubFed.ServeIdentityAvatar)
			r.Post("/peer/handshake", pubFed.Handshake)
			// Pairing requests "Steam-style" (migration 048). Tres
			// endpoints publicos en par a /peer/handshake:
			//   POST /federation/pairing-requests          (A -> B inicial)
			//   POST /federation/pairing-requests/{id}/callback (B -> A)
			//   POST /federation/pairing-requests/{id}/cancel   (A -> B)
			// La autorizacion va por contenido (request_token + firma
			// Ed25519 cuando aplica), no por JWT del peer - el JWT solo
			// existe DESPUES de pairing.
			//
			// Rate-limit per-IP: 5 req/min/IP, burst 3. Defense vs
			// flood; el admin toggle "accept_pairing_requests" + el
			// cap de incoming pending son las otras dos capas.
			pairingRL := handlers.NewPairingRequestRateLimiter()
			r.Group(func(r chi.Router) {
				r.Use(handlers.IPRateLimitMiddleware(pairingRL))
				r.Post("/federation/pairing-requests", pubFed.ReceivePairingRequest)
				r.Post("/federation/pairing-requests/{id}/callback", pubFed.ReceivePairingCallback)
				r.Post("/federation/pairing-requests/{id}/cancel", pubFed.ReceivePairingCancel)
			})

			r.Group(func(r chi.Router) {
				r.Use(federation.RequirePeerJWT(deps.Federation))
				r.Get("/peer/ping", pubFed.Ping)
				// Catalog browse (Phase 3) — JOIN-filtered against
				// federation_library_shares server-side. A peer never
				// sees libraries / items they don't have a share for.
				r.Get("/peer/libraries", pubFed.ListLibraries)
				r.Get("/peer/libraries/{libraryID}/items", pubFed.ListLibraryItems)
				r.Get("/peer/search", pubFed.SearchLibraries)
				r.Get("/peer/recent", pubFed.ListRecent)

				// Streaming (Phase 5). Peer A asks us to spawn a
				// stream session for one of our items; we serve HLS
				// manifests + segments against the resulting opaque
				// session UUID. Both ACL gated by share.CanPlay --
				// session UUID alone is NOT sufficient.
				if deps.StreamManager != nil && deps.Items != nil {
					// We're already inside the deps.StreamManager != nil branch
					// so the concrete-to-interface conversion is unconditional;
					// the helper below takes the StreamManagerService interface
					// directly without the var/assign dance the health handler
					// uses (where the value can stay nil).
					fedStream := handlers.NewFederationStreamHandler(deps.Federation, deps.StreamManager, deps.Items, deps.MediaStreams, deps.Logger)
					r.Post("/peer/stream/{itemId}/session", fedStream.StartSession)
					r.Get("/peer/stream/session/{sessionId}/master.m3u8", fedStream.MasterPlaylist)
					// Subtitles BEFORE the {quality}/* wildcard routes so
					// the literal `subtitles` segment wins the match (chi
					// prefers literal over param at the same depth, but
					// keeping the registration order explicit avoids
					// surprises if the routing logic changes).
					r.Get("/peer/stream/session/{sessionId}/subtitles", fedStream.Subtitles)
					r.Get("/peer/stream/session/{sessionId}/subtitles/{trackIndex}", fedStream.SubtitleTrack)
					r.Get("/peer/stream/session/{sessionId}/{quality}/index.m3u8", fedStream.QualityPlaylist)
					r.Get("/peer/stream/session/{sessionId}/{quality}/{segment}", fedStream.Segment)
				}

				// Poster proxy (Phase 5 Slice 2). The peer's catalog
				// UI fetches each item's poster bytes through here so
				// users on the peer never contact this server directly
				// (no IP / UA leak) and we can re-verify CanBrowse on
				// every fetch (a peer that lost a share since the
				// catalog cached locally cannot keep pulling artwork).
				if deps.Items != nil && deps.Images != nil && fedImgSrv != nil {
					fedImg := handlers.NewFederationImageHandler(deps.Federation, deps.Items, deps.Images, fedImgSrv, deps.Logger)
					r.Get("/peer/items/{itemId}/poster", fedImg.ItemPoster)
				}
			})
		}

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(deps.Auth.Middleware)

			// Auth
			r.Post("/auth/logout", authHandler.Logout)

			// Device authorization grant — approve route is auth-gated
			// (the operator must already be logged in to confirm a code).
			if deviceHandler != nil {
				r.Post("/auth/device/approve", deviceHandler.Approve)
			}

			// Server-Sent Events for real-time updates
			if deps.EventBus != nil {
				eventHandler := handlers.NewEventHandler(deps.EventBus, deps.SSELimiter, deps.Logger)
				r.Get("/events", eventHandler.Stream)

				// User-scoped SSE: cross-device sync of watch progress,
				// played, favourites. The handler filters by claims.UserID
				// so other users on the same server never see these events.
				meEventsHandler := handlers.NewMeEventsHandler(deps.EventBus, deps.SSELimiter, deps.Logger)
				r.Get("/me/events", meEventsHandler.Stream)
			}

			// Uploads (PR2 feature). Tres superficies:
			//   POST/PATCH/HEAD/DELETE /uploads/         → protocolo tus
			//   GET                    /uploads/mine     → audit del usuario
			//   GET                    /uploads/events   → SSE filtrado
			// El tus handler se monta con chi.Mount para que el path-routing
			// (basePath + uploadID) lo lleve tusd internamente sin que chi
			// le pise el id como param.
			if deps.Uploads != nil && deps.UploadsAudit != nil && deps.EventBus != nil {
				uploadsAPI := handlers.NewUploadsHandler(deps.UploadsAudit, deps.EventBus, deps.SSELimiter, deps.Logger)
				r.Get("/uploads/mine", uploadsAPI.ListMine)
				r.Get("/uploads/events", uploadsAPI.Stream)
				// tus handler. Importante: bajo /api/v1/uploads/ con el slash
				// final — tusd compone Location: /api/v1/uploads/<id> tras
				// el POST de creación, y el cliente PATCH-ea ahí mismo.
				r.Mount("/uploads/", deps.Uploads)
			}

			// Current user
			r.Get("/me", userHandler.Me)
			r.Post("/me/password", authHandler.ChangeMyPassword)
			// Avatar subido por el propio usuario. POST recibe el
			// multipart (campo "avatar"); el service resize + persiste
			// y devuelve la URL pública nueva (cambia en cada upload
			// para forzar refetch del navegador). DELETE es idempotente.
			r.Post("/me/avatar", userHandler.UploadMyAvatar)
			r.Delete("/me/avatar", userHandler.DeleteMyAvatar)
			r.Get("/me/profiles", authHandler.ListProfiles)
			r.Post("/auth/switch-profile", authHandler.SwitchProfile)
			r.Get("/me/sessions", authHandler.ListMySessions)
			r.Delete("/me/sessions/{id}", authHandler.RevokeMySession)

			// Inbox de notificaciones por usuario (migration 049).
			// Generico: cualquier feature puede emitir via el
			// notification.Service; el frontend pinta un dropdown
			// en el header con badge de unread_count.
			if deps.Notifications != nil {
				notifHandler := handlers.NewNotificationsHandler(deps.Notifications, deps.Logger)
				r.Get("/me/notifications", notifHandler.List)
				r.Post("/me/notifications/{id}/read", notifHandler.MarkRead)
				r.Post("/me/notifications/read-all", notifHandler.MarkAllRead)
			}

			// Per-user preferences (hero mode, theme overrides, etc.)
			// Authenticated; the handler derives userID from claims so
			// there's no path param to tamper with.
			if deps.UserPreferences != nil {
				prefsHandler := handlers.NewPreferencesHandler(deps.UserPreferences, deps.Logger)
				r.Get("/me/preferences", prefsHandler.ListMine)
				r.Put("/me/preferences/{key}", prefsHandler.SetMine)
				r.Delete("/me/preferences/{key}", prefsHandler.DeleteMine)
			}

			// Users — most surfaces are admin-only (List, Register,
			// Delete, role, content-rating, active, reset-password)
			// but PIN is special: the parent of a profile must be
			// able to set their own kid's PIN without admin help.
			// PIN therefore lives under the auth-only group below
			// while the rest stay admin-gated.
			r.Route("/users", func(r chi.Router) {
				r.Use(auth.RequireAdmin)
				r.Get("/", userHandler.List)
				r.Post("/", authHandler.Register)
				r.Delete("/{id}", userHandler.Delete)
				r.Post("/{id}/reset-password", authHandler.ResetPassword)
				r.Put("/{id}/content-rating", authHandler.SetContentRating)
				// SetRole es owner-only (migración 055): sólo el owner
				// promueve a admin o degrada uno. can_manage_admins gestiona
				// FLAGS de admins ya existentes, no el role.
				if deps.Permissions != nil {
					r.With(deps.Permissions.RequireOwner).Put("/{id}/role", userHandler.SetRole)
				} else {
					r.Put("/{id}/role", userHandler.SetRole)
				}
				r.Put("/{id}/active", userHandler.SetActive)
				r.Put("/{id}/access", userHandler.SetAccess)
				// Library access matrix. GET paints the admin UI
				// (current grants for the target's household); PUT
				// replaces the whole set transactionally. Grants
				// always target the top-level user — passing a
				// profile id to PUT returns 400 (ADR-014). The GET
				// counterpart normalises profile ids to the parent
				// so the frontend can render the inherited set
				// without an extra round-trip.
				r.Get("/{id}/library-access", userHandler.GetLibraryAccess)
				r.Put("/{id}/library-access", userHandler.SetLibraryAccess)
				// Personal IPTV library shortcut. Creates a livetv
				// library + grants access only to this user in one
				// tx, so the admin doesn't have to navigate to
				// /admin/libraries first and then come back to tick
				// the new lib in the access matrix.
				r.Post("/{id}/iptv-libraries", userHandler.CreatePersonalIPTV)

				// Permission flags (migración 055). El read es admin-only
				// genérico; los writes están gated finos:
				//   PUT  /users/{id}/permissions     → can_manage_admins
				//   POST /users/{id}/transfer-ownership → owner-only
				if deps.Permissions != nil && deps.UserRepo != nil {
					permHandler := handlers.NewPermissionsHandler(deps.UserRepo, deps.Logger)
					r.Get("/{id}/permissions", permHandler.GetPermissions)
					r.With(deps.Permissions.Require(authmodel.PermManageAdmins)).
						Put("/{id}/permissions", permHandler.PutPermissions)
					r.With(deps.Permissions.RequireOwner).
						Post("/{id}/transfer-ownership", permHandler.TransferOwnership)
				}
			})

			// PIN management — auth-only (the handler then enforces
			// the admin-OR-parent-of-target-OR-self matrix). Lives
			// outside the admin-gated /users block above so the
			// parent of a profile can hit it without holding the
			// admin role.
			r.Put("/users/{id}/pin", authHandler.SetPIN)
			// Display-name rename — same authorisation matrix as
			// SetPIN (admin OR parent-of-target OR self) so a parent
			// can relabel their own profile members from the picker
			// without needing the admin role.
			r.Put("/users/{id}/display-name", userHandler.SetDisplayName)
			// Avatar colour override — same authorisation matrix as
			// SetDisplayName. Lives outside the admin-gated /users
			// block so a parent can recolour their own profile
			// member without holding the admin role.
			r.Put("/users/{id}/avatar-color", userHandler.SetAvatarColor)
			// Servir el avatar subido. Auth-gated igual que el resto
			// del bloque — los clientes ya tienen sesión cuando lo
			// renderizan (lista admin, picker de perfil, TopBar). El
			// path es uniforme para todos los avatares aunque cambie
			// el fichero subyacente: la URL incluye ?v=<rel> como
			// cache-buster, no en el path.
			r.Get("/users/{id}/avatar", userHandler.ServeUserAvatar)

			// Signing key lifecycle (admin only). Every route here is
			// destructive — guarded at the group level so a single
			// middleware change toggles access for all of them at once.
			if ks := deps.Auth.KeyStoreOrNil(); ks != nil {
				var observe func(outcome string)
				if deps.Metrics != nil {
					observe = func(outcome string) {
						deps.Metrics.AuthKeyRotations.WithLabelValues(outcome).Inc()
					}
				}
				adminAuth := handlers.NewAdminAuthHandler(ks, nil, observe, deps.Logger)

				r.Route("/admin/auth/keys", func(r chi.Router) {
					// Owner-only (migración 055): JWT signing keys protegen
					// la autenticación de todo el server. Rotar/podar es
					// una operación que sólo el dueño de la instalación
					// debería tocar.
					if deps.Permissions != nil {
						r.Use(deps.Permissions.RequireOwner)
					} else {
						r.Use(auth.RequireAdmin)
					}
					r.Get("/", adminAuth.ListKeys)
					r.Post("/rotate", adminAuth.Rotate)
					r.Post("/prune", adminAuth.Prune)
				})

				// User-facing federation surface — any auth'd user
				// can browse what the admin has shared with paired
				// peers (Phase 4). Server uses peer JWTs internally;
				// the user only ever holds their normal session token.
				if deps.Federation != nil {
					mePeers := handlers.NewMePeersHandler(deps.Federation, deps.Logger)
					r.Route("/me/peers", func(r chi.Router) {
						r.Get("/", mePeers.ListMyPeers)
						// Unified view: all libraries from all paired
						// peers in one response, used by the /peers
						// landing page so the UI doesn't have to
						// fan-out N calls itself.
						r.Get("/libraries", mePeers.BrowseAllPeerLibraries)
						// Federated search: fan-out the user's query
						// to every paired peer in parallel and
						// aggregate the hits with origin metadata.
						// Per-peer timeouts inside the manager keep
						// one slow peer from blocking the response.
						r.Get("/search", mePeers.SearchPeers)
						// Cross-peer "what's new?" rail: fan-out to
						// every paired peer for their freshest items.
						// Same fan-out posture as /search (per-peer
						// timeout, errors-skip, per-peer fairness cap).
						r.Get("/recent", mePeers.RecentPeers)
						// Cross-peer Continue Watching: reads
						// federation_progress JOIN federation_item_cache
						// locally, no peer fan-out (state is ours).
						r.Get("/continue-watching", mePeers.PeerContinueWatching)
						r.Get("/{peerID}/libraries", mePeers.BrowsePeerLibraries)
						r.Get("/{peerID}/libraries/{libraryID}/items", mePeers.BrowsePeerItems)
						r.Post("/{peerID}/libraries/{libraryID}/refresh", mePeers.RefreshPeerLibrary)
						// Poster proxy. The PosterCard's <img src> hits
						// this endpoint and we relay the bytes from the
						// peer with our peer JWT. Same-origin so no CORS,
						// and the peer never sees the user's IP / UA.
						r.Get("/{peerID}/items/{itemId}/poster", mePeers.ProxyPeerItemPoster)
						// Streaming proxy (Phase 5). The user's HLS
						// player only ever talks to us; we proxy
						// the bytes from the peer with our peer JWT.
						r.Post("/{peerID}/stream/{itemId}/session", mePeers.StartPeerStreamSession)
						r.Get("/{peerID}/stream/session/{sessionId}/master.m3u8", mePeers.ProxyPeerStreamMaster)
						r.Get("/{peerID}/stream/session/{sessionId}/subtitles", mePeers.ProxyPeerStreamSubtitles)
						r.Get("/{peerID}/stream/session/{sessionId}/subtitles/{trackIndex}", mePeers.ProxyPeerStreamSubtitleTrack)
						r.Get("/{peerID}/stream/session/{sessionId}/{quality}/index.m3u8", mePeers.ProxyPeerStreamQuality)
						r.Get("/{peerID}/stream/session/{sessionId}/{quality}/{segment}", mePeers.ProxyPeerStreamSegment)
						// Cross-peer playback state for a single item.
						// Same shape as /me/items/{id}/progress but
						// scoped to (peer, remote_item_id) and backed
						// by federation_progress (migration 028).
						r.Get("/{peerID}/items/{itemId}/progress", mePeers.GetPeerItemProgress)
						r.Post("/{peerID}/items/{itemId}/progress", mePeers.UpdatePeerItemProgress)
					})
				}

				// Federation admin surface — invite generation, peer
				// pairing, peer listing, peer revocation.
				if deps.Federation != nil {
					adminFed := handlers.NewFederationAdminHandler(deps.Federation, deps.Logger)
					r.Route("/admin/peers", func(r chi.Router) {
						// Owner-only (migración 055): pairing con peers
						// remotos abre superficie de salida de datos
						// (catálogo, posters proxied). Operación de
						// instalación, no de admin de día a día.
						if deps.Permissions != nil {
							r.Use(deps.Permissions.RequireOwner)
						} else {
							r.Use(auth.RequireAdmin)
						}
						r.Get("/", adminFed.ListPeers)
						r.Get("/identity", adminFed.GetServerIdentity)
						r.Put("/identity", adminFed.UpdateServerIdentity)
						// Toggles admin de federation (anti-spam, etc.).
						r.Get("/settings", adminFed.GetFederationSettings)
						r.Put("/settings", adminFed.UpdateFederationSettings)
						// Foto del servidor: upload multipart + delete
						// idempotente. El serve público vive bajo
						// /federation/identity/avatar (sin auth).
						r.Post("/identity/avatar", adminFed.UploadServerAvatar)
						r.Delete("/identity/avatar", adminFed.DeleteServerAvatar)
						r.Post("/probe", adminFed.ProbePeer)
						r.Post("/accept", adminFed.AcceptInvite)
						// Pairing requests Steam-style: 5 admin endpoints
						// (migration 048). Reemplazan funcionalmente el
						// flow de Invite + AcceptInvite + handshake para
						// admins que prefieran "0 copy-paste".
						r.Route("/pairing-requests", func(r chi.Router) {
							r.Get("/", adminFed.ListPairingRequests)
							r.Post("/send", adminFed.SendPairingRequest)
							r.Post("/{id}/accept", adminFed.AcceptPairingRequest)
							r.Post("/{id}/decline", adminFed.DeclinePairingRequest)
							r.Delete("/{id}", adminFed.CancelPairingRequest)
						})
						r.Get("/{id}", adminFed.GetPeer)
						r.Post("/{id}/refresh", adminFed.RefreshPeer)
						r.Delete("/{id}", adminFed.RevokePeer)
						r.Route("/invites", func(r chi.Router) {
							r.Get("/", adminFed.ListActiveInvites)
							r.Post("/", adminFed.GenerateInvite)
						})
						r.Route("/{id}/shares", func(r chi.Router) {
							r.Get("/", adminFed.ListShares)
							r.Post("/", adminFed.CreateShare)
							r.Delete("/{shareID}", adminFed.DeleteShare)
						})
					})
				}
			}

			// Rich system stats (admin only). Public /health stays minimal
			// for liveness probes; this endpoint backs the React admin
			// "System" panel and can grow without breaking ops tooling.
			{
				var sysStreams handlers.SystemStatsProvider
				if deps.StreamManager != nil {
					sysStreams = deps.StreamManager
				}
				var sysLibs handlers.LibraryStatsProvider
				if deps.Libraries != nil {
					sysLibs = deps.Libraries
				}
				dbPath := ""
				imageDir := ""
				bindAddress := ""
				baseURL := ""
				if deps.Config != nil {
					dbPath = deps.Config.Database.Path
					imageDir = filepath.Join(filepath.Dir(deps.Config.Database.Path), "images")
					bindAddress = deps.Config.Server.Addr()
					baseURL = deps.Config.Server.BaseURL
				}
				// Host info sampler — optional. nil providers degrade to
				// an empty host section so the test rig + minimal startup
				// paths keep working.
				var hostInfo handlers.HostInfoProvider
				if deps.HostMetrics != nil {
					hostInfo = deps.HostMetrics
				}
				sysHandler := handlers.NewSystemHandler(handlers.SystemHandlerConfig{
					Health:         deps.DB,
					Activity:       deps.Activity,
					Streams:        sysStreams,
					Libraries:      sysLibs,
					Settings:       deps.Settings,
					Host:           hostInfo,
					ImageDir:       imageDir,
					DBPath:         dbPath,
					BindAddress:    bindAddress,
					BaseURLDefault: baseURL,
					Version:        deps.Version,
					Logger:         deps.Logger,
				})
				r.Route("/admin/system", func(r chi.Router) {
					r.Use(auth.RequireAdmin)
					r.Get("/stats", sysHandler.Stats)
					r.Get("/stream-activity", sysHandler.StreamActivity)
					r.Get("/top-items", sysHandler.TopItems)
					// "Recientemente añadido" del dashboard. Mezcla
					// movies + series rolled-up por actividad (no
					// episodios sueltos como hacia /items/latest).
					if deps.Libraries != nil {
						libAdminHandler := handlers.NewLibraryHandler(
							deps.Libraries, deps.Images, deps.Metadata,
							deps.UserData, deps.Users, deps.Logger,
						)
						r.Get("/recently-added", libAdminHandler.AdminRecentlyAdded)
					}
					// "Now Playing" admin panel — list every active stream
					// session and let the operator kill any of them. Routed
					// here (rather than next to the player streaming routes)
					// because both methods are admin-only and want to share
					// the /admin/system/* prefix the dashboard already uses.
					if deps.StreamManager != nil {
						adminStreams := handlers.NewAdminStreamsHandler(
							deps.StreamManager, deps.Users, deps.Items, deps.Logger,
						)
						r.Get("/sessions", adminStreams.ListSessions)
						r.Delete("/sessions/{id}", adminStreams.KillSession)
					}

					// Storage breakdown — disco fisico + peso por
					// biblioteca. Endpoint dedicado (no parte de
					// /stats) porque la cadencia es distinta: stats
					// cada 30s, storage cada minuto - cambia solo
					// con scans.
					if deps.Libraries != nil && deps.Items != nil {
						adminStorage := handlers.NewAdminStorageHandler(
							deps.Libraries, deps.Items, deps.Logger,
						)
						r.Get("/storage/disks", adminStorage.Disks)
					}
					if deps.Settings != nil {
						// Surface the host's actually-detected accelerators to the
						// settings handler so the panel only offers choices that have
						// a chance of working. Empty slice when the stream manager
						// isn't wired (test rig / minimal startup) — handler treats
						// that as "detector saw nothing" and falls back to "auto".
						var detectedHWAccel []string
						var streamingDefaults handlers.StreamingDefaults
						if deps.StreamManager != nil {
							for _, a := range deps.StreamManager.HWAccelInfo().Available {
								detectedHWAccel = append(detectedHWAccel, string(a))
							}
							// Snapshot the auto-tuned streaming knobs from the
							// running manager so the panel's "Default" column
							// reflects what the server actually picked for the
							// host's hardware — not a static YAML constant the
							// admin would have to deduce in their head.
							streamingDefaults = handlers.StreamingDefaults{
								MaxTranscodeSessions:        deps.StreamManager.MaxTranscodeSessions(),
								MaxTranscodeSessionsPerUser: deps.StreamManager.MaxTranscodeSessionsPerUser(),
								TranscodePreset:             deps.StreamManager.TranscodePreset(),
							}
						}
						settingsHandler := handlers.NewSettingsHandler(handlers.SettingsHandlerConfig{
							Settings:          deps.Settings,
							BaseURLDefault:    baseURL,
							HWAccelDefault:    deps.Config.Streaming.HWAccel,
							HWAccelDetected:   detectedHWAccel,
							StreamingDefaults: streamingDefaults,
							Logger:            deps.Logger,
						})
						r.Get("/settings", settingsHandler.List)
						r.Put("/settings", settingsHandler.Update)
						r.Delete("/settings/{key}", settingsHandler.Reset)
					}

					// DB backup / restore + DB driver swap + restart.
					// OWNER-ONLY (migración 055) — son operaciones que
					// pueden:
					//   - Exfiltrar TODA la DB en un fichero (backup
					//     download).
					//   - Reemplazar la DB con un sqlite arbitrario
					//     (restore upload).
					//   - Cambiar el driver de DB (swap a una DSN
					//     externa controlada por el atacante).
					//   - Reiniciar el server.
					// Las metemos en un sub-Group con permCheck.RequireOwner
					// encima del RequireAdmin del padre.
					r.Group(func(r chi.Router) {
						if deps.Permissions != nil {
							r.Use(deps.Permissions.RequireOwner)
						}
						if deps.DB != nil {
							backupHandler := handlers.NewAdminBackupHandler(
								deps.Config.Database.Driver, deps.DB, deps.Config.Database.Path, deps.Logger,
							)
							r.Get("/backup", backupHandler.Download)
							r.Post("/backup/restore", backupHandler.Upload)
						}
						if deps.SetupService != nil && deps.ConfigPath != "" {
							dbHandler := handlers.NewAdminDBHandler(
								deps.Config,
								deps.ConfigPath,
								deps.DB,
								deps.SetupService.SaveDatabaseConfig,
								deps.RestartRequester,
								deps.Logger,
							)
							r.Get("/db", dbHandler.Status)
							r.Get("/db/profiles", dbHandler.Profiles)
							r.Post("/db/test", dbHandler.Test)
							r.Put("/db", dbHandler.Save)
							r.Post("/db/migrate", dbHandler.Migrate)
							r.Post("/restart", dbHandler.Restart)
						}
					})

					// Logs viewer. Snapshot endpoint for the initial
					// fill, SSE stream for the live tail. The handler
					// short-circuits when LogBuffer is nil (test
					// builds, etc.) so callers don't 500.
					logsHandler := handlers.NewAdminLogsHandler(deps.LogBuffer, deps.SSELimiter)
					r.Get("/logs", logsHandler.Snapshot)
					r.Get("/logs/stream", logsHandler.Stream)
				})
			}

			// Watch Progress & User Engagement
			if deps.UserData != nil {
				progressHandler := handlers.NewProgressHandler(deps.UserData, deps.Images, deps.EventBus, deps.Logger)

				r.Get("/me/continue-watching", progressHandler.ContinueWatching)
				r.Delete("/me/continue-watching/{itemId}", progressHandler.RemoveFromContinueWatching)
				r.Get("/me/favorites", progressHandler.Favorites)
				r.Get("/me/next-up", progressHandler.NextUp)

				r.Route("/me/progress/{itemId}", func(r chi.Router) {
					r.Get("/", progressHandler.GetProgress)
					r.Put("/", progressHandler.UpdateProgress)
					r.Post("/played", progressHandler.MarkPlayed)
					r.Post("/unplayed", progressHandler.MarkUnplayed)
					r.Post("/favorite", progressHandler.ToggleFavorite)
				})
			}

			// Home page customisation + discovery rails. Sits next
			// to the other /me/* surfaces because every handler is
			// scoped to the caller — layout, trending, and live-now
			// are all per-user (trending filters by accessible
			// libraries, live-now joins favourites + access).
			if deps.Home != nil && deps.UserPreferences != nil && deps.LibraryRepo != nil && deps.Items != nil {
				homeHandler := handlers.NewHomeHandler(
					deps.Home,
					deps.UserPreferences,
					deps.LibraryRepo,
					deps.Items,
					deps.Images,
					deps.Metadata,
					deps.Users,
					deps.Logger,
				)
				r.Get("/me/home/layout", homeHandler.GetLayout)
				r.Put("/me/home/layout", homeHandler.PutLayout)
				r.Get("/me/home/trending", homeHandler.Trending)
				r.Get("/me/home/recommended", homeHandler.Recommended)
				r.Get("/me/home/because-you-watched", homeHandler.BecauseYouWatched)
				r.Get("/me/home/live-now", homeHandler.LiveNow)
			}

			// Streaming
			if deps.StreamManager != nil {
				streamHandler := handlers.NewStreamHandler(
					deps.StreamManager, deps.Items, deps.MediaStreams,
					deps.ExternalIDs, deps.Providers,
					deps.Settings, deps.Config.Server.BaseURL, deps.Logger,
				)

				r.Route("/stream/{itemId}", func(r chi.Router) {
					r.Get("/info", streamHandler.Info)
					r.Get("/master.m3u8", streamHandler.MasterPlaylist)
					r.Get("/{quality}/index.m3u8", streamHandler.QualityPlaylist)
					r.Get("/{quality}/{segment}", streamHandler.Segment)
					r.Get("/direct", streamHandler.DirectPlay)
					r.Delete("/session", streamHandler.StopSession)
					r.Get("/subtitles", streamHandler.Subtitles)
					r.Get("/subtitles/{trackIndex}", streamHandler.SubtitleTrack)
					// External subtitle providers (OpenSubtitles, ...).
					// Search returns candidates; the download endpoint
					// pipes the SRT/ASS through ffmpeg → WebVTT and
					// serves it for the player's <track> element.
					r.Get("/subtitles/external", streamHandler.SearchExternalSubtitles)
					r.Get("/subtitles/external/{fileId}", streamHandler.DownloadExternalSubtitle)
				})
			}

			// Libraries & Items (only if service is wired)
			if deps.Libraries != nil {
				libHandler := handlers.NewLibraryHandler(deps.Libraries, deps.Images, deps.Metadata, deps.UserData, deps.Users, deps.Logger)
				// Trickplay sprites land under <imageDir>/trickplay/ —
				// reusing the image-storage root keeps the on-disk
				// layout clustered (one tree the operator can backup,
				// rsync, or `du` to size the cache).
				trickplayDir := filepath.Join(filepath.Dir(deps.Config.Database.Path), "images", "trickplay")
				// scanner ↔ MetadataIdentifier: deps.Scanner es *scanner.Scanner;
				// el handler sólo necesita la pequeña interfaz MetadataIdentifier.
				// Pasarlo como nil cuando no esté wired hace que los endpoints
				// /identify devuelvan 503 sin tumbar el resto del handler.
				var identifier handlers.MetadataIdentifier
				if deps.Scanner != nil {
					identifier = deps.Scanner
				}
				itemHandler := handlers.NewItemHandler(deps.Libraries, deps.Images, deps.Metadata, deps.UserData, deps.Users, deps.Chapters, deps.EpisodeSegments, deps.ExternalIDs, deps.People, deps.Collections, deps.Providers, identifier, trickplayDir, deps.Logger)

				// Libraries
				r.Get("/libraries", libHandler.List)
				r.Route("/libraries/{id}", func(r chi.Router) {
					r.Get("/", libHandler.Get)
					r.Get("/items", libHandler.Items)

					// Admin-only library management
					r.Group(func(r chi.Router) {
						r.Use(auth.RequireAdmin)
						r.Put("/", libHandler.Update)
						r.Delete("/", libHandler.Delete)
						r.Post("/scan", libHandler.Scan)
					})

				})
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireAdmin)
					r.Post("/libraries", libHandler.Create)
					r.Get("/libraries/browse", libHandler.Browse)
				})

				// IPTV channels (within library routes)
				if deps.IPTV != nil {
					// Pass deps.IPTVTransmux as-is — when nil the handler
					// falls back to the raw passthrough proxy, which is
					// the correct degraded-but-functional behaviour for
					// HLS-only deployments without ffmpeg.
					iptvHandler := handlers.NewIPTVHandler(deps.IPTV, deps.IPTVProxy, deps.IPTVTransmux, deps.IPTVLogoCache, fedImageDir, deps.LibraryRepo, deps.Libraries, deps.Logger)

					r.Route("/libraries/{id}/channels", func(r chi.Router) {
						r.Get("/", iptvHandler.ListChannels)
						r.Get("/groups", iptvHandler.Groups)
					})

					r.Route("/channels/{channelId}", func(r chi.Router) {
						r.Get("/", iptvHandler.GetChannel)
						r.Get("/stream", iptvHandler.Stream)
						r.Get("/proxy", iptvHandler.ProxyURL)
						r.Get("/schedule", iptvHandler.Schedule)
						r.Post("/watch", iptvHandler.RecordChannelWatch)
						r.Post("/playback-failure", iptvHandler.RecordPlaybackFailure)
						// HLS transmux endpoints. The Stream handler 302s
						// here when the upstream is MPEG-TS (Xtream Codes,
						// raw TS-over-HTTP). The manifest spawns / re-uses
						// the per-channel ffmpeg session; segments are
						// served from the session's work dir. Both 404
						// gracefully when no session exists so hls.js
						// recovers via a manifest reload.
						r.Get("/hls/index.m3u8", iptvHandler.HLSManifest)
						r.Get("/hls/{segment}", iptvHandler.HLSSegment)
						// Same-origin proxy for the channel's tvg-logo.
						// Mirrors the upstream image to disk + serves
						// from the local cache, so CSP can stay
						// locked to `self` and external image hosts
						// don't get to track the user.
						r.Get("/logo", iptvHandler.ChannelLogo)
					})

					r.Get("/channels/schedule", iptvHandler.BulkSchedule)
					r.Post("/channels/schedule", iptvHandler.BulkSchedule)

					// Continue watching rail (per-user). GET only —
					// the beacon is POST /channels/{id}/watch above.
					r.Get("/me/channels/continue-watching", iptvHandler.ListContinueWatching)

					// Per-user channel personalisation: reorder + hide
					// channels for the caller's own view without
					// affecting other users or the admin defaults.
					r.Put("/me/iptv/channels/order", iptvHandler.ReplaceChannelOrder)
					r.Delete("/me/iptv/channels/order", iptvHandler.ResetChannelOrder)
					r.Put("/me/iptv/channels/{channelId}/visibility", iptvHandler.SetChannelVisibility)

					// Channel favorites (per-user, requires auth; no admin role).
					r.Route("/favorites/channels", func(r chi.Router) {
						r.Get("/", iptvHandler.ListFavorites)
						r.Get("/ids", iptvHandler.ListFavoriteIDs)
						r.Put("/{channelId}", iptvHandler.AddFavorite)
						r.Delete("/{channelId}", iptvHandler.RemoveFavorite)
					})

					// Public IPTV
					r.Get("/iptv/public/countries", iptvHandler.PublicCountries)
					r.Get("/iptv/epg-catalog", iptvHandler.EPGCatalog)

					// Per-library EPG source list (read: user with library ACL;
					// mutations: admin-only, below).
					r.Get("/libraries/{id}/epg-sources", iptvHandler.ListEPGSources)

					// Unhealthy-channels admin surface: read is gated by the
					// same library ACL as the channel list; the mutation
					// endpoints live under the admin group below.
					r.Get("/libraries/{id}/channels/unhealthy", iptvHandler.ListUnhealthyChannels)
					r.Get("/libraries/{id}/channels/without-epg", iptvHandler.ListChannelsWithoutEPG)
					// Lightweight summary: just the three counts the
					// admin panel needs on first paint. The heavy
					// unhealthy / without-epg lists then load lazily,
					// only when the operator opens their tab.
					r.Get("/libraries/{id}/channels/health-summary", iptvHandler.ChannelHealthSummary)

					// IPTV scheduled jobs (automated M3U + EPG refresh).
					// Read: any user with library ACL (so the livetv panel
					// can show schedule status). Mutations: admin-only, in
					// the group below.
					var iptvScheduleHandler *handlers.IPTVScheduleHandler
					if deps.IPTVSchedules != nil && deps.IPTVScheduler != nil {
						iptvScheduleHandler = handlers.NewIPTVScheduleHandler(
							deps.IPTVSchedules, deps.IPTVScheduler, deps.Libraries, deps.Logger)
						r.Get("/libraries/{id}/schedule", iptvScheduleHandler.List)
					}

					// Admin IPTV operations
					r.Group(func(r chi.Router) {
						r.Use(auth.RequireAdmin)
						r.Post("/iptv/preflight", iptvHandler.PreflightM3U)
						r.Post("/iptv/public/import", iptvHandler.ImportPublicIPTV)
						r.Post("/libraries/{id}/epg-sources", iptvHandler.AddEPGSource)
						r.Delete("/libraries/{id}/epg-sources/{sourceId}", iptvHandler.RemoveEPGSource)
						r.Patch("/libraries/{id}/epg-sources/reorder", iptvHandler.ReorderEPGSources)
						r.Post("/channels/{channelId}/reset-health", iptvHandler.ResetChannelHealth)
						r.Post("/channels/{channelId}/disable", iptvHandler.DisableChannel)
						r.Post("/channels/{channelId}/enable", iptvHandler.EnableChannel)
						r.Patch("/channels/{channelId}", iptvHandler.PatchChannel)
						// Override del logo del canal (URL externa o
						// archivo subido). El GET del logo (proxy) NO
						// está aquí — vive arriba con los demás endpoints
						// de canal porque cualquier usuario autenticado lo
						// pide; sólo escritura es admin-only.
						r.Put("/channels/{channelId}/logo", iptvHandler.SetChannelLogo)
						r.Post("/channels/{channelId}/logo/upload", iptvHandler.UploadChannelLogo)
						r.Delete("/channels/{channelId}/logo", iptvHandler.ClearChannelLogo)
						// Admin channel curation. Reorder, hide, restore
						// M3U order. Hidden HERE is a hard constraint:
						// downstream the per-user overlay can only hide
						// more, not surface what the admin removed.
						r.Get("/libraries/{id}/channels/admin-view", iptvHandler.ListLibraryChannelsAdmin)
						r.Put("/libraries/{id}/channels/order", iptvHandler.ReplaceLibraryChannelOrder)
						r.Delete("/libraries/{id}/channels/order", iptvHandler.ResetLibraryChannelOrder)
						r.Put("/libraries/{id}/channels/{channelId}/admin-visibility", iptvHandler.SetLibraryChannelVisibility)
						r.Route("/libraries/{id}/iptv", func(r chi.Router) {
							r.Post("/refresh-m3u", iptvHandler.RefreshM3U)
							r.Post("/refresh-epg", iptvHandler.RefreshEPG)
							// Auto-discovery de logos contra iptv-org
							// (database pública con miles de canales
							// mapeados por tvg-id → logo URL).
							r.Post("/refresh-logos-from-iptv-org", iptvHandler.RefreshLogosFromIPTVOrg)
						})
						if iptvScheduleHandler != nil {
							r.Put("/libraries/{id}/schedule/{kind}", iptvScheduleHandler.Upsert)
							r.Delete("/libraries/{id}/schedule/{kind}", iptvScheduleHandler.Delete)
							r.Post("/libraries/{id}/schedule/{kind}/run", iptvScheduleHandler.RunNow)
						}
					})
				}

				// Items
				r.Get("/items/latest", libHandler.LatestItems)
				// Global paginated items list. Same payload shape as
				// /libraries/{id}/items but spanning every library —
				// the Movies / Series browse pages don't pre-pick a
				// library so they can't go through the scoped route.
				// Without this the pages used to fall back to
				// /items/latest which is capped at 50 and doesn't
				// paginate, which surfaced as "only a few movies show
				// up" in the browse grid.
				r.Get("/items", libHandler.AllItems)
				r.Get("/items/search", itemHandler.Search)
				// Catalogue-wide genre vocabulary for the filter panel.
				// Returns name + count, sorted by frequency desc, scoped
				// by ?type=movie|series so a TV-only library doesn't
				// surface "Action & Adventure" to /movies and vice versa.
				r.Get("/items/genres", libHandler.Genres)
				r.Route("/items/{id}", func(r chi.Router) {
					r.Get("/", itemHandler.Get)
					r.Get("/children", itemHandler.Children)
					// "More like this" rail. Pulls from TMDb's
					// recommendations endpoint and cross-references
					// each candidate against the local library so the
					// UI can deep-link to in-library matches.
					r.Get("/recommendations", itemHandler.Recommendations)
					// Trickplay (seek-bar thumbnail previews). The
					// first hit triggers ffmpeg generation; both
					// endpoints serve from disk on subsequent hits.
					r.Get("/trickplay.json", itemHandler.TrickplayManifest)
					r.Get("/trickplay.png", itemHandler.TrickplaySprite)

					// Identify / rematch contra TMDb (admin-only).
					// Mismo patrón Plex/Jellyfin: el operador busca,
					// elige el match correcto y se aplica sobrescribiendo
					// metadatos + imágenes del item.
					r.Group(func(r chi.Router) {
						r.Use(auth.RequireAdmin)
						r.Get("/identify/candidates", itemHandler.IdentifyCandidates)
						r.Post("/identify", itemHandler.Identify)
						// Editor manual de metadatos. Distinto de
						// identify: no consulta TMDb, sólo escribe los
						// campos que el operador suministra. Bloquea
						// el item al guardar para que el siguiente
						// "Refresh metadata" no pise la edición.
						r.Patch("/metadata", itemHandler.UpdateItemMetadata)
						r.Put("/metadata/lock", itemHandler.SetMetadataLock)
						// Re-corre el enrich del scanner sobre este
						// item (mismo flujo que el library refresh,
						// pero para un solo item). Lo dispara el
						// kebab "Actualizar metadatos" del poster /
						// del detalle. Respeta el lock.
						r.Post("/refresh-metadata", itemHandler.RefreshItemMetadata)
					})
				})
			}

			// Image management — reuse the handler lifted above so the
			// peer-facing federation poster endpoint and the local
			// /images/file/{id} endpoint share one path-mapping store
			// and one thumbnail cache.
			if deps.Images != nil && deps.Providers != nil && deps.ExternalIDs != nil && fedImgSrv != nil {
				imageDir := fedImageDir
				imgHandler := fedImgSrv

				// Image management (nested under items)
				r.Route("/items/{id}/images", func(r chi.Router) {
					r.Get("/", imgHandler.List)
					r.Get("/available", imgHandler.Available)
					r.Put("/{type}/select", imgHandler.Select)
					r.Post("/{type}/upload", imgHandler.Upload)
					r.Put("/{imageId}/primary", imgHandler.SetPrimary)
					r.Put("/{imageId}/lock", imgHandler.SetLocked)
					r.Delete("/{imageId}", imgHandler.Delete)
				})

				// Serve local image files
				r.Get("/images/file/{id}", imgHandler.ServeFile)

				// Serve cast/crew profile photos. Sits next to the
				// regular image endpoint so the cache + auth context
				// match exactly. People IDs are uuids; the handler
				// validates the resolved on-disk path stays inside
				// imageDir before serving.
				if deps.People != nil {
					peopleHandler := handlers.NewPeopleHandler(deps.People, imageDir, deps.Logger)
					r.Get("/people/{id}", peopleHandler.Get)
					r.Get("/people/{id}/thumb", peopleHandler.Thumb)
				}

				// Studios browse + detail. Powers the click-on-the-
				// studio-mark flow on movie/series detail pages —
				// /studios/{slug} returns the studio header (logo,
				// name) plus every item from this catalogue linked to
				// it, sorted year-desc.
				if deps.Studios != nil {
					studioHandler := handlers.NewStudioHandler(deps.Studios, deps.Logger)
					r.Get("/studios", studioHandler.List)
					r.Get("/studios/{slug}", studioHandler.Get)
				}

				// Movie collections (Jellyfin-style sagas). Backed by
				// TMDb's belongs_to_collection record on each movie;
				// /collections/{id} renders the saga's members in
				// release order under a hero pulled from the
				// collection's own poster + backdrop.
				if deps.Collections != nil {
					var collectionOverrides handlers.CollectionImageOverrideRepo
					if deps.CollectionImageOverrides != nil {
						collectionOverrides = deps.CollectionImageOverrides
					}
					var collectionImages handlers.CollectionImageProvider
					if deps.Providers != nil {
						collectionImages = deps.Providers
					}
					collectionHandler := handlers.NewCollectionHandler(deps.Collections, collectionOverrides, collectionImages, fedImageDir, deps.Logger)
					r.Get("/collections", collectionHandler.List)
					r.Get("/collections/{id}", collectionHandler.Get)
					// Cualquier usuario autenticado puede GET el archivo
					// (img-src 'self' del CSP del proyecto lo requiere).
					r.Get("/collections/{id}/images/{type}/file", collectionHandler.ServeCollectionImage)
					// Admin: gestión del override.
					r.Group(func(r chi.Router) {
						r.Use(auth.RequireAdmin)
						r.Get("/collections/{id}/images/{type}/available", collectionHandler.AvailableCollectionImages)
						r.Put("/collections/{id}/images/{type}", collectionHandler.SetCollectionImage)
						r.Post("/collections/{id}/images/{type}/upload", collectionHandler.UploadCollectionImage)
						r.Delete("/collections/{id}/images/{type}", collectionHandler.ClearCollectionImage)
					})
				}

				// Admin: batch refresh images for a library
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireAdmin)
					r.Post("/libraries/{id}/images/refresh", imgHandler.RefreshLibraryImages)
				})
			}

			// Providers (metadata, images, subtitles)
			if deps.Providers != nil {
				providerHandler := handlers.NewProviderHandler(deps.Providers, deps.ProviderRepo, deps.Logger)

				r.Get("/providers/search/metadata", providerHandler.SearchMetadata)
				r.Get("/providers/metadata/{externalId}", providerHandler.GetMetadata)
				r.Get("/providers/images", providerHandler.GetImages)
				r.Get("/providers/search/subtitles", providerHandler.SearchSubtitles)

				// Admin provider management
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireAdmin)
					r.Get("/providers", providerHandler.List)
					r.Put("/providers/{name}", providerHandler.Update)
				})
			}
		})
	})

	// Serve embedded web frontend (SPA fallback)
	if deps.WebAssets != nil {
		fileServer := http.FileServer(http.FS(deps.WebAssets))
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			// Try to serve the exact file first (JS, CSS, images, etc.)
			path := strings.TrimPrefix(r.URL.Path, "/")
			if path == "" {
				path = "index.html"
			}
			if _, err := fs.Stat(deps.WebAssets, path); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
			// SPA fallback: serve index.html for all other routes
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
		})
	}

	return r
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
