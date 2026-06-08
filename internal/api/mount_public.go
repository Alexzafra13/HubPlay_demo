package api

import (
	"hubplay/internal/api/handlers"
	authhandler "hubplay/internal/api/handlers/auth"
	"hubplay/internal/api/handlers/system"

	"github.com/go-chi/chi/v5"
)

// mountHealthAndOpenAPI registra los endpoints de salud (sin auth) y el
// documento OpenAPI público. `/health/live` no toca deps; `/health/ready`
// + `/health` ping a la DB y devuelven 503 cuando algo está down (load
// balancers drenan tráfico). El OpenAPI es ETag-aware para clientes que
// polling.
func mountHealthAndOpenAPI(r chi.Router, healthHandler *system.HealthHandler) {
	r.Get("/health", healthHandler.Health)
	r.Get("/health/live", healthHandler.Live)
	r.Get("/health/ready", healthHandler.Ready)

	openapiHandler := system.NewOpenAPIHandler()
	r.Get("/openapi.yaml", openapiHandler.ServeYAML)
	r.Head("/openapi.yaml", openapiHandler.ServeYAML)
}

// mountAuthPublic registra los endpoints de auth sin sesión previa:
// login, refresh, setup-del-primer-admin y los dos endpoints del Device
// Authorization Grant (RFC 8628) para clientes headless (TV apps, CLI).
// El endpoint /auth/device/approve se monta en mountAuthProtected porque
// requiere sesión.
func mountAuthPublic(
	r chi.Router,
	authHandler *authhandler.AuthHandler,
	deviceHandler *authhandler.DeviceAuthHandler,
) {
	// Rate-limit por IP a nivel app sobre los endpoints sin sesión:
	// frena fuerza-bruta de credenciales sin depender de que el operador
	// configure nginx. El SSE de device/events queda FUERA del grupo —
	// es una conexión long-lived legítima que el limiter (1 token por
	// open) penalizaría en reconexiones de un proxy que mata idle conns.
	authRL := handlers.NewAuthRateLimiter()
	r.Group(func(r chi.Router) {
		r.Use(handlers.IPRateLimitMiddleware(authRL))

		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/refresh", authHandler.Refresh)
		// Setup — crea el primer admin (sólo cuando no hay usuarios todavía).
		// Solo desde la red local (M3): cierra el race-to-setup desde internet.
		r.With(RequirePrivateClient).Post("/auth/setup", authHandler.Setup)

		if deviceHandler != nil {
			r.Post("/auth/device/start", deviceHandler.Start)
			r.Post("/auth/device/poll", deviceHandler.Poll)
		}
	})

	if deviceHandler != nil && deviceHandler.HasEventBus() {
		// SSE — la UI in-app del pairing (QR + user_code) reacciona
		// instantánea al approve sin polling. Sin rate-limit (long-lived).
		r.Get("/auth/device/events", deviceHandler.Events)
	}
}

// mountSetupWizard registra el wizard del primer arranque. Cada endpoint
// excepto /status valida internamente que el wizard sigue activo; cuando
// SetupService no está cableado (caso típico en tests minimalistas) el
// bloque entero se omite y todas las rutas devuelven 404.
func mountSetupWizard(r chi.Router, deps Dependencies) {
	if deps.Setup.Service == nil {
		return
	}
	setupHandler := system.NewSetupHandler(system.SetupHandlerConfig{
		Setup:     deps.Setup.Service,
		DBSaver:   deps.Setup.Service,
		Auth:      deps.Auth.Auth,
		Libraries: deps.Catalog.Libraries,
		Users:     deps.Auth.Users,
		Providers: deps.Providers.Repo,
		Config:    deps.Server.Config,
		Restart:   deps.Server.RestartRequester,
		Logger:    deps.Infra.Logger,
	})

	// /setup/status queda accesible siempre: la SPA lo consulta para saber
	// si toca el wizard o el login (también desde fuera, post-setup).
	r.Get("/setup/status", setupHandler.Status)

	// El resto del wizard solo desde la red local (M3): incluye navegar el
	// filesystem del host (/setup/browse) y crear libs/ajustes/DB. Post-setup
	// estos endpoints ya devuelven 404 (validan wizard activo), así que la
	// restricción solo muerde durante la ventana inicial.
	r.Group(func(r chi.Router) {
		r.Use(RequirePrivateClient)
		r.Get("/setup/capabilities", setupHandler.Capabilities)
		r.Get("/setup/browse", setupHandler.Browse)
		r.Post("/setup/libraries", setupHandler.CreateLibraries)
		r.Post("/setup/settings", setupHandler.UpdateSettings)
		r.Post("/setup/complete", setupHandler.Complete)
		// Step 0 — selector de driver de DB. El operador escoge SQLite
		// o Postgres antes de que aterricen datos, así el resto del
		// wizard crea filas en el backend elegido y no en el default
		// del YAML.
		r.Get("/setup/db/profiles", setupHandler.DatabaseProfiles)
		r.Post("/setup/db/test", setupHandler.TestDatabase)
		r.Post("/setup/db", setupHandler.SaveDatabase)
	})
}
