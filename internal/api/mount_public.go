package api

import (
	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers"
)

// mountHealthAndOpenAPI registra los endpoints de salud (sin auth) y el
// documento OpenAPI público. `/health/live` no toca deps; `/health/ready`
// + `/health` ping a la DB y devuelven 503 cuando algo está down (load
// balancers drenan tráfico). El OpenAPI es ETag-aware para clientes que
// polling.
func mountHealthAndOpenAPI(r chi.Router, healthHandler *handlers.HealthHandler) {
	r.Get("/health", healthHandler.Health)
	r.Get("/health/live", healthHandler.Live)
	r.Get("/health/ready", healthHandler.Ready)

	openapiHandler := handlers.NewOpenAPIHandler()
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
	authHandler *handlers.AuthHandler,
	deviceHandler *handlers.DeviceAuthHandler,
) {
	r.Post("/auth/login", authHandler.Login)
	r.Post("/auth/refresh", authHandler.Refresh)

	if deviceHandler != nil {
		r.Post("/auth/device/start", deviceHandler.Start)
		r.Post("/auth/device/poll", deviceHandler.Poll)
		if deviceHandler.HasEventBus() {
			// SSE — la UI in-app del pairing (QR + user_code) reacciona
			// instantánea al approve sin polling.
			r.Get("/auth/device/events", deviceHandler.Events)
		}
	}

	// Setup — crea el primer admin (sólo cuando no hay usuarios todavía).
	r.Post("/auth/setup", authHandler.Setup)
}

// mountSetupWizard registra el wizard del primer arranque. Cada endpoint
// excepto /status valida internamente que el wizard sigue activo; cuando
// SetupService no está cableado (caso típico en tests minimalistas) el
// bloque entero se omite y todas las rutas devuelven 404.
func mountSetupWizard(r chi.Router, deps Dependencies) {
	if deps.SetupService == nil {
		return
	}
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
	// Step 0 — selector de driver de DB. El operador escoge SQLite
	// o Postgres antes de que aterricen datos, así el resto del
	// wizard crea filas en el backend elegido y no en el default
	// del YAML.
	r.Get("/setup/db/profiles", setupHandler.DatabaseProfiles)
	r.Post("/setup/db/test", setupHandler.TestDatabase)
	r.Post("/setup/db", setupHandler.SaveDatabase)
}
