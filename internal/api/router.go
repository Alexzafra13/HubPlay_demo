package api

import (
	"crypto/subtle"
	"io/fs"
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
	"hubplay/internal/config"
	"hubplay/internal/imaging/pathmap"
	"hubplay/internal/library"
)

// Dependencies es el grafo completo de dependencias que NewRouter
// necesita para montar el router. Los campos están agrupados en
// sub-structs por dominio (definidos en deps.go) — `gopls find-references`
// sobre un sub-struct devuelve exactamente quién depende de él.
//
// Cierra el olor MM del audit 2026-05-14 ("Dependencies tiene 70+
// campos planos y es ilegible"). El composition root construye cada
// sub-struct explícitamente; el router y los mountXxx leen siempre via
// path anidado: `deps.Infra.Logger`, `deps.IPTV.Service`, etc.
type Dependencies struct {
	Infra      InfraDeps
	Server     ServerDeps
	Auth       AuthDeps
	Catalog    CatalogDeps
	Streaming  StreamingDeps
	IPTV       IPTVDeps
	Federation FederationDeps
	Providers  ProvidersDeps
	Admin      AdminDeps
	Setup      SetupDeps
	Uploads    UploadsDeps
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
	// Retro-compat con tests minimalistas que sólo pasan `Server.Config: cfg`
	// — si los campos primitivos no llegan rellenados, los derivamos
	// aquí desde Config. main.go los pasa siempre explícitos (path
	// idiomático). Una vez derivados, ningún sitio downstream lee
	// `deps.Server.Config.X.Y` excepto los dos handlers que mutan el
	// fichero (setup wizard + admin DB). Cierra olor V del audit
	// 2026-05-14.
	deps.fillFromConfig()

	r := chi.NewRouter()

	// Wire the observability hook into the handlers package so every
	// rendered AppError gets counted. Kept out of NewRouter's return
	// path so tests that never pass Metrics stay on the no-op recorder.
	if deps.Infra.Metrics != nil {
		handlers.SetErrorRecorder(func(code string) {
			deps.Infra.Metrics.HTTPErrors.WithLabelValues(code).Inc()
		})
	}

	applyGlobalMiddleware(r, deps)
	mountMetricsEndpoint(r, deps)

	// Handlers compartidos por varios mountXxx — construidos una vez.
	authHandler := authhandler.NewAuthHandler(
		deps.Auth.Auth, deps.Auth.Users, deps.Catalog.Libraries,
		deps.Server.AuthConfig, deps.Infra.Audit, deps.Infra.Logger,
	)
	userHandler := users.NewUserHandler(
		deps.Auth.Users, deps.Catalog.Libraries, deps.Infra.Audit, deps.Infra.Logger,
	)

	// Avoid wrapping a nil concrete pointer in a non-nil interface.
	var streamSvc handlers.StreamManagerService
	if deps.Streaming.StreamManager != nil {
		streamSvc = deps.Streaming.StreamManager
	}
	healthHandler := system.NewHealthHandler(
		deps.Admin.DB, streamSvc, deps.Infra.Version, deps.Server.DatabasePath,
	)

	// Device auth handler: construido aquí porque vive en dos mounts
	// distintos — start/poll/events públicos (mountAuthPublic) y
	// approve auth-gated (mountAuthProtected). Stateless internamente
	// — la sesión y los códigos viven en deps.Auth.DeviceCode.
	var deviceHandler *authhandler.DeviceAuthHandler
	if deps.Auth.DeviceCode != nil {
		deviceHandler = authhandler.NewDeviceAuthHandler(
			deps.Auth.DeviceCode, nil, deps.Server.AuthConfig,
			deps.Infra.EventBus, deps.Infra.SSELimiter, deps.Infra.Logger,
		)
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
	if deps.Admin.DB != nil && deps.Server.DataDir != "" &&
		deps.Catalog.Images != nil && deps.Catalog.ExternalIDs != nil &&
		deps.Catalog.Items != nil && deps.Providers.Manager != nil {
		fedImageDir = filepath.Join(deps.Server.DataDir, "images")
		fedImgSrv = media.NewImageHandler(
			deps.Catalog.Images, deps.Catalog.ExternalIDs, deps.Catalog.Items, deps.Providers.Manager,
			library.NewImageRefresher(
				deps.Catalog.Items, deps.Catalog.ExternalIDs, deps.Catalog.Images, deps.Providers.Manager,
				pathmap.New(fedImageDir), fedImageDir, deps.Infra.Logger,
			),
			fedImageDir, deps.Infra.Audit, deps.Infra.Logger,
		)
	}

	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no auth required).
		mountHealthAndOpenAPI(r, healthHandler)
		mountAuthPublic(r, authHandler, deviceHandler)
		mountSetupWizard(r, deps)
		mountFederationPublic(r, deps, fedImgSrv)

		// Protected routes (deps.Auth.Auth.Middleware enforza sesión).
		r.Group(func(r chi.Router) {
			r.Use(deps.Auth.Auth.Middleware)

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
	if deps.Infra.WebAssets != nil {
		mountSPAFallback(r, deps.Infra.WebAssets)
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
// Client-IP middleware: usa `ClientIPFromXFF(deps.Server.TrustedProxies...)`
// si el operador ha declarado proxies de confianza, sino
// `ClientIPFromRemoteAddr`. Reemplaza `middleware.RealIP` que se
// deprecó en chi v5.3.0 por IP spoofing (3 CVE incl. Critical 9.3).
// La nueva API NO muta r.RemoteAddr — el IP va en el ctx; los
// handlers lo leen con `handlers.ClientIP(r)` (helper local que cae
// a r.RemoteAddr si el middleware no lo dejó set).
func applyGlobalMiddleware(r chi.Router, deps Dependencies) {
	if len(deps.Server.TrustedProxies) > 0 {
		r.Use(middleware.ClientIPFromXFF(normalizeCIDRs(deps.Server.TrustedProxies)...))
	} else {
		r.Use(middleware.ClientIPFromRemoteAddr)
	}
	r.Use(middleware.RequestID)
	r.Use(RequestLogger(deps.Infra.Logger))
	r.Use(middleware.Recoverer)
	r.Use(SecurityHeaders())
	if deps.Infra.Metrics != nil {
		r.Use(deps.Infra.Metrics.MetricsMiddleware)
	}
	// CORS middleware. Si deps.Server.CorsRegistry está cableado (caso
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
	if deps.Server.CorsRegistry != nil {
		r.Use(CorsMiddleware(deps.Server.CorsRegistry, corsMethods, corsAllowedHeaders, corsExposedHeaders, true, 300))
	} else {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   deps.Server.AllowedOrigins,
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
	if deps.Infra.Metrics == nil || !deps.Server.MetricsEnabled {
		return
	}
	path := deps.Server.MetricsPath
	if path == "" {
		path = "/metrics"
	}
	h := deps.Infra.Metrics.Handler()
	if token := deps.Server.MetricsToken; token != "" {
		// Gate opt-in: exige Bearer (o ?token= para scrapers que no pueden
		// poner cabecera, p.ej. algunos blackbox exporters). Comparación
		// en tiempo constante para no filtrar el token por timing.
		h = requireMetricsToken(token, h)
	} else if deps.Infra.Logger != nil {
		// Sin token: /metrics queda público. Aviso ruidoso porque el bind
		// por defecto es 0.0.0.0 y las métricas son reconocimiento útil.
		deps.Infra.Logger.Warn("/metrics expuesto SIN autenticación — bloquéalo en el reverse proxy o define observability.metrics_token",
			"path", path)
	}
	r.Handle(path, h)
}

// requireMetricsToken envuelve el handler de métricas con un check de
// token (Bearer o query). Usa subtle.ConstantTimeCompare.
func requireMetricsToken(token string, next http.Handler) http.Handler {
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			got = strings.TrimPrefix(h, "Bearer ")
		} else if q := r.URL.Query().Get("token"); q != "" {
			got = q
		}
		if got == "" || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
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

// fillFromConfig rellena los campos primitivos de Server desde el
// `*config.Config` cuando vienen a zero — pensado para tests que sólo
// pasan `Server.Config: cfg`. main.go los pasa siempre explícitos. Si
// `Config` es nil, deja todo como está (los tests que ni siquiera pasan
// Config ya no tocan ninguna ruta config-dependiente). Cierra olor V
// del audit 2026-05-14.
func (deps *Dependencies) fillFromConfig() {
	cfg := deps.Server.Config
	if cfg == nil {
		return
	}
	if !deps.Server.MetricsEnabled {
		deps.Server.MetricsEnabled = cfg.Observability.MetricsEnabled
	}
	if deps.Server.MetricsPath == "" {
		deps.Server.MetricsPath = cfg.Observability.MetricsPath
	}
	if deps.Server.MetricsToken == "" {
		deps.Server.MetricsToken = cfg.Observability.MetricsToken
	}
	if deps.Server.AuthConfig == (config.AuthConfig{}) {
		deps.Server.AuthConfig = cfg.Auth
	}
	if deps.Server.DatabasePath == "" {
		deps.Server.DatabasePath = cfg.Database.Path
	}
	if deps.Server.DataDir == "" && cfg.Database.Path != "" {
		deps.Server.DataDir = filepath.Dir(cfg.Database.Path)
	}
	if deps.Server.DatabaseDriver == "" {
		deps.Server.DatabaseDriver = cfg.Database.Driver
	}
	if deps.Server.ServerAddr == "" {
		deps.Server.ServerAddr = cfg.Server.Addr()
	}
	if deps.Server.ServerBaseURL == "" {
		deps.Server.ServerBaseURL = cfg.Server.BaseURL
	}
	if deps.Server.ServerPort == 0 {
		deps.Server.ServerPort = cfg.Server.Port
	}
	if !deps.Server.MDNSEnabled {
		deps.Server.MDNSEnabled = cfg.MDNS.Enabled
	}
	if deps.Server.MDNSHostname == "" {
		deps.Server.MDNSHostname = cfg.MDNS.Hostname
	}
	if deps.Server.HWAccelDefault == (config.HWAccelConfig{}) {
		deps.Server.HWAccelDefault = cfg.Streaming.HWAccel
	}
	if deps.Server.AllowedOrigins == nil {
		deps.Server.AllowedOrigins = allowedOrigins(cfg)
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
