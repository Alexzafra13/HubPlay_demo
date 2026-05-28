package api

import (
	"fmt"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers/admin"
	libhandler "hubplay/internal/api/handlers/library"
	"hubplay/internal/api/handlers/system"
	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
)

// mountAdminSystem registra el bloque admin más grande del router:
// /admin/system/* — el dashboard del operador. Incluye stats, settings,
// backup/restore de DB, swap de driver, restart, CORS origins, logs,
// audit log, update checker, "now playing" sessions, storage breakdown,
// y la "recientemente añadido" para el dashboard.
//
// La mayoría va detrás de auth.RequireAdmin (read del dashboard). Los
// sub-bloques destructivos (backup/restore, DB driver swap, restart,
// CORS origins) suben a Permissions.RequireOwner — son operaciones que
// pueden exfiltrar/reemplazar la DB entera o expandir la superficie de
// CSRF.
func mountAdminSystem(r chi.Router, deps Dependencies) {
	var sysStreams system.SystemStatsProvider
	if deps.Streaming.StreamManager != nil {
		sysStreams = deps.Streaming.StreamManager
	}
	var sysLibs system.LibraryStatsProvider
	if deps.Catalog.Libraries != nil {
		sysLibs = deps.Catalog.Libraries
	}
	dbPath := deps.Server.DatabasePath
	imageDir := ""
	if deps.Server.DataDir != "" {
		imageDir = filepath.Join(deps.Server.DataDir, "images")
	}
	bindAddress := deps.Server.ServerAddr
	baseURL := deps.Server.ServerBaseURL
	mdnsURL := ""
	if deps.Server.MDNSEnabled {
		host := deps.Server.MDNSHostname
		if host == "" {
			host = "hubplay"
		}
		mdnsURL = fmt.Sprintf("http://%s.local:%d", host, deps.Server.ServerPort)
	}
	// Host info sampler — opcional. nil providers degradan a una
	// host section vacía así que el test rig + paths mínimos siguen
	// funcionando.
	var hostInfo system.HostInfoProvider
	if deps.Infra.HostMetrics != nil {
		hostInfo = deps.Infra.HostMetrics
	}
	sysHandler := system.NewSystemHandler(system.SystemHandlerConfig{
		Health:         deps.Admin.DB,
		Activity:       deps.Admin.Activity,
		Streams:        sysStreams,
		Libraries:      sysLibs,
		Settings:       deps.Admin.Settings,
		Host:           hostInfo,
		ImageDir:       imageDir,
		DBPath:         dbPath,
		BindAddress:    bindAddress,
		BaseURLDefault: baseURL,
		MDNSURL:        mdnsURL,
		Version:        deps.Infra.Version,
		Commit:         deps.Infra.Commit,
		BuildDate:      deps.Infra.BuildDate,
		Logger:         deps.Infra.Logger,
	})

	r.Route("/admin/system", func(r chi.Router) {
		// /admin/system es mixed bag (migración 055): la mayoría
		// son reads del dashboard (stats, stream-activity,
		// top-items, recently-added, storage) — fine para
		// cualquier admin. Las destructivas (DELETE /sessions/{id},
		// settings writes) cruzan múltiples capabilities y no
		// encajan bien con un flag único. Refinar cada endpoint
		// de aquí queda para una iteración futura; backup/db/
		// restart YA están en un sub-Group con RequireOwner más
		// abajo.
		r.Use(auth.RequireAdmin)
		r.Get("/stats", sysHandler.Stats)
		r.Get("/stream-activity", sysHandler.StreamActivity)
		r.Get("/top-items", sysHandler.TopItems)
		// Update checker (PR2 update-notifier). Si deps.Admin.Updates
		// es nil (dev build / repo no configurado) las rutas
		// devuelven cached zero-state vía el handler.
		if deps.Admin.Updates != nil {
			updHandler := system.NewUpdatesHandler(deps.Admin.Updates, deps.Infra.Logger)
			r.Get("/updates", updHandler.Status)
			r.Post("/updates/check", updHandler.Check)
		}
		// "Recientemente añadido" del dashboard. Mezcla movies +
		// series rolled-up por actividad (no episodios sueltos
		// como hacía /items/latest).
		if deps.Catalog.Libraries != nil {
			libAdminHandler := libhandler.NewLibraryHandler(
				deps.Catalog.Libraries, deps.Catalog.Images, deps.Catalog.Metadata,
				deps.Catalog.UserData, deps.Auth.Users, deps.Infra.Audit, deps.Infra.Logger,
			)
			r.Get("/recently-added", libAdminHandler.AdminRecentlyAdded)
		}
		// "Now Playing" admin panel — lista cada stream session
		// activa y deja al operador matar cualquiera. Routeado
		// aquí (no junto al player streaming) porque ambos
		// métodos son admin-only y quieren compartir el prefix
		// /admin/system/* que el dashboard ya usa.
		if deps.Streaming.StreamManager != nil {
			adminStreams := admin.NewAdminStreamsHandler(
				deps.Streaming.StreamManager, deps.Auth.Users, deps.Catalog.Items, deps.Infra.Logger,
			)
			r.Get("/sessions", adminStreams.ListSessions)
			r.Delete("/sessions/{id}", adminStreams.KillSession)
		}

		// Storage breakdown — disco físico + peso por biblioteca.
		// Endpoint dedicado (no parte de /stats) porque la cadencia
		// es distinta: stats cada 30s, storage cada minuto - cambia
		// solo con scans.
		if deps.Catalog.Libraries != nil && deps.Catalog.Items != nil {
			adminStorage := admin.NewAdminStorageHandler(
				deps.Catalog.Libraries, deps.Catalog.Items, deps.Infra.Logger,
			)
			r.Get("/storage/disks", adminStorage.Disks)
		}
		if deps.Admin.Settings != nil {
			// Surfacea los accelerators realmente detectados del
			// host al settings handler así que el panel sólo
			// ofrece choices que tengan alguna chance de funcionar.
			// Empty slice cuando el stream manager no está cableado
			// (test rig / startup mínimo) — handler trata eso como
			// "detector no vio nada" y cae a "auto".
			var detectedHWAccel []string
			var streamingDefaults admin.StreamingDefaults
			if deps.Streaming.StreamManager != nil {
				for _, a := range deps.Streaming.StreamManager.HWAccelInfo().Available {
					detectedHWAccel = append(detectedHWAccel, string(a))
				}
				// Snapshot de los knobs streaming auto-tuneados
				// del manager corriendo, así el panel "Default"
				// reflecta lo que el server eligió para el
				// hardware del host — no una constante YAML
				// estática que el admin tendría que deducir.
				streamingDefaults = admin.StreamingDefaults{
					MaxTranscodeSessions:        deps.Streaming.StreamManager.MaxTranscodeSessions(),
					MaxTranscodeSessionsPerUser: deps.Streaming.StreamManager.MaxTranscodeSessionsPerUser(),
					TranscodePreset:             deps.Streaming.StreamManager.TranscodePreset(),
				}
			}
			settingsHandler := admin.NewSettingsHandler(admin.SettingsHandlerConfig{
				Settings:          deps.Admin.Settings,
				BaseURLDefault:    baseURL,
				HWAccelDefault:    deps.Server.HWAccelDefault,
				HWAccelDetected:   detectedHWAccel,
				StreamingDefaults: streamingDefaults,
				Logger:            deps.Infra.Logger,
			})
			r.Get("/settings", settingsHandler.List)
			r.Put("/settings", settingsHandler.Update)
			r.Delete("/settings/{key}", settingsHandler.Reset)
		}

		// DB backup / restore + DB driver swap + restart.
		// OWNER-ONLY (migración 055) — son operaciones que pueden:
		//   - Exfiltrar TODA la DB en un fichero (backup download).
		//   - Reemplazar la DB con un sqlite arbitrario (restore upload).
		//   - Cambiar el driver de DB (swap a una DSN externa controlada
		//     por el atacante).
		//   - Reiniciar el server.
		// Las metemos en un sub-Group con permCheck.RequireOwner
		// encima del RequireAdmin del padre.
		r.Group(func(r chi.Router) {
			if deps.Auth.Permissions != nil {
				r.Use(deps.Auth.Permissions.RequireOwner)
			}
			if deps.Admin.DB != nil {
				backupHandler := admin.NewAdminBackupHandler(
					deps.Server.DatabaseDriver, deps.Admin.DB, deps.Server.DatabasePath, deps.Infra.Audit, deps.Infra.Logger,
				)
				r.Get("/backup", backupHandler.Download)
				r.Post("/backup/restore", backupHandler.Upload)
			}
			if deps.Setup.Service != nil && deps.Server.ConfigPath != "" {
				dbHandler := admin.NewAdminDBHandler(
					deps.Server.Config,
					deps.Server.ConfigPath,
					deps.Admin.DB,
					deps.Setup.Service.SaveDatabaseConfig,
					deps.Server.RestartRequester,
					deps.Infra.Audit,
					deps.Infra.Logger,
				)
				r.Get("/db", dbHandler.Status)
				r.Get("/db/profiles", dbHandler.Profiles)
				r.Post("/db/test", dbHandler.Test)
				r.Put("/db", dbHandler.Save)
				r.Post("/db/migrate", dbHandler.Migrate)
				r.Post("/restart", dbHandler.Restart)
			}

			// CORS origins panel (migración 056). Mismo gate
			// owner-only: añadir un origen es expandir la
			// superficie de CSRF cross-origin. Va dentro de
			// /admin/system para que el dashboard tenga un
			// único hogar de "configuración del servidor".
			if deps.Server.CorsOriginsRepo != nil && deps.Server.CorsRegistry != nil {
				corsHandler := system.NewCorsOriginsHandler(
					deps.Server.CorsOriginsRepo,
					deps.Server.CorsRegistry,
					ValidateCorsOrigin,
					deps.Infra.Audit,
					deps.Infra.Logger,
				)
				r.Get("/cors-origins", corsHandler.List)
				r.Post("/cors-origins", corsHandler.Add)
				r.Delete("/cors-origins", corsHandler.Delete)
			}
		})

		// Logs viewer. Snapshot endpoint para el fill inicial,
		// SSE stream para el live tail. El handler short-circuita
		// cuando LogBuffer es nil (test builds, etc.) así que los
		// callers no reciben 500.
		logsHandler := admin.NewAdminLogsHandler(deps.Infra.LogBuffer, deps.Infra.SSELimiter)
		r.Get("/logs", logsHandler.Snapshot)
		r.Get("/logs/stream", logsHandler.Stream)

		// Audit log unificado (PR5). Gateado por can_view_audit
		// — un admin con sólo este flag puede revisar el historial
		// sin tocar nada. El owner también pasa (User.Can()
		// devuelve true para todo). Sub-Group adicional sobre el
		// RequireAdmin del padre.
		if deps.Infra.AuditLog != nil {
			auditHandler := admin.NewAuditLogHandler(deps.Infra.AuditLog, deps.Infra.Logger)
			r.Group(func(r chi.Router) {
				if deps.Auth.Permissions != nil {
					r.Use(deps.Auth.Permissions.Require(authmodel.PermViewAudit))
				}
				r.Get("/audit-log", auditHandler.Query)
				r.Get("/audit-log/types", auditHandler.EventTypes)
			})
		}
	})
}
