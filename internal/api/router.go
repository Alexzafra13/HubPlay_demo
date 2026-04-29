package api

import (
	"database/sql"
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
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/imaging/pathmap"
	"hubplay/internal/iptv"
	"hubplay/internal/library"
	"hubplay/internal/observability"
	"hubplay/internal/provider"
	"hubplay/internal/setup"
	"hubplay/internal/stream"
	"hubplay/internal/user"
)

type Dependencies struct {
	Auth           *auth.Service
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
	Chapters       *db.ChapterRepository
	People         *db.PeopleRepository
	UserPreferences *db.UserPreferenceRepository
	Providers      *provider.Manager
	ExternalIDs    *db.ExternalIDRepository
	LibraryRepo    *db.LibraryRepository
	ProviderRepo   *db.ProviderRepository
	Settings       *db.SettingsRepository
	SetupService   *setup.Service
	EventBus       *event.Bus
	Database       *sql.DB
	Version        string
	WebAssets      fs.FS
	Config         *config.Config
	Logger         *slog.Logger
	Metrics        *observability.Metrics
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
	authHandler := handlers.NewAuthHandler(deps.Auth, deps.Users, deps.Config.Auth, deps.Logger)
	userHandler := handlers.NewUserHandler(deps.Users, deps.Logger)

	// Avoid wrapping a nil concrete pointer in a non-nil interface.
	var streamSvc handlers.StreamManagerService
	if deps.StreamManager != nil {
		streamSvc = deps.StreamManager
	}
	healthHandler := handlers.NewHealthHandler(deps.Database, streamSvc, deps.Version)

	// Public routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health check (no auth)
		r.Get("/health", healthHandler.Health)

		// Auth (no auth required)
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/refresh", authHandler.Refresh)

		// Setup — create first admin (only works when no users exist)
		r.Post("/auth/setup", authHandler.Setup)

		// Setup wizard (no auth for status, auth handled per-step)
		if deps.SetupService != nil {
			setupHandler := handlers.NewSetupHandler(deps.SetupService, deps.Auth, deps.Libraries, deps.Users, deps.ProviderRepo, deps.Config, deps.Logger)

			r.Get("/setup/status", setupHandler.Status)
			r.Get("/setup/capabilities", setupHandler.Capabilities)
			r.Post("/setup/browse", setupHandler.Browse)
			r.Post("/setup/libraries", setupHandler.CreateLibraries)
			r.Post("/setup/settings", setupHandler.UpdateSettings)
			r.Post("/setup/complete", setupHandler.Complete)
		}

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(deps.Auth.Middleware)

			// Auth
			r.Post("/auth/logout", authHandler.Logout)

			// Server-Sent Events for real-time updates
			if deps.EventBus != nil {
				eventHandler := handlers.NewEventHandler(deps.EventBus, deps.Logger)
				r.Get("/events", eventHandler.Stream)
			}

			// Current user
			r.Get("/me", userHandler.Me)

			// Per-user preferences (hero mode, theme overrides, etc.)
			// Authenticated; the handler derives userID from claims so
			// there's no path param to tamper with.
			if deps.UserPreferences != nil {
				prefsHandler := handlers.NewPreferencesHandler(deps.UserPreferences, deps.Logger)
				r.Get("/me/preferences", prefsHandler.ListMine)
				r.Put("/me/preferences/{key}", prefsHandler.SetMine)
				r.Delete("/me/preferences/{key}", prefsHandler.DeleteMine)
			}

			// Users (admin only)
			r.Route("/users", func(r chi.Router) {
				r.Use(auth.RequireAdmin)
				r.Get("/", userHandler.List)
				r.Post("/", authHandler.Register)
				r.Delete("/{id}", userHandler.Delete)
			})

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
					r.Use(auth.RequireAdmin)
					r.Get("/", adminAuth.ListKeys)
					r.Post("/rotate", adminAuth.Rotate)
					r.Post("/prune", adminAuth.Prune)
				})
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
				sysHandler := handlers.NewSystemHandler(handlers.SystemHandlerConfig{
					DB:             deps.Database,
					Streams:        sysStreams,
					Libraries:      sysLibs,
					Settings:       deps.Settings,
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
					if deps.Settings != nil {
						// Surface the host's actually-detected accelerators to the
						// settings handler so the panel only offers choices that have
						// a chance of working. Empty slice when the stream manager
						// isn't wired (test rig / minimal startup) — handler treats
						// that as "detector saw nothing" and falls back to "auto".
						var detectedHWAccel []string
						if deps.StreamManager != nil {
							for _, a := range deps.StreamManager.HWAccelInfo().Available {
								detectedHWAccel = append(detectedHWAccel, string(a))
							}
						}
						settingsHandler := handlers.NewSettingsHandler(handlers.SettingsHandlerConfig{
							Settings:        deps.Settings,
							BaseURLDefault:  baseURL,
							HWAccelDefault:  deps.Config.Streaming.HWAccel,
							HWAccelDetected: detectedHWAccel,
							Logger:          deps.Logger,
						})
						r.Get("/settings", settingsHandler.List)
						r.Put("/settings", settingsHandler.Update)
						r.Delete("/settings/{key}", settingsHandler.Reset)
					}
				})
			}

			// Watch Progress & User Engagement
			if deps.UserData != nil {
				progressHandler := handlers.NewProgressHandler(deps.UserData, deps.Images, deps.Logger)

				r.Get("/me/continue-watching", progressHandler.ContinueWatching)
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
				libHandler := handlers.NewLibraryHandler(deps.Libraries, deps.Images, deps.Metadata, deps.UserData, deps.Logger)
				// Trickplay sprites land under <imageDir>/trickplay/ —
				// reusing the image-storage root keeps the on-disk
				// layout clustered (one tree the operator can backup,
				// rsync, or `du` to size the cache).
				trickplayDir := filepath.Join(filepath.Dir(deps.Config.Database.Path), "images", "trickplay")
				itemHandler := handlers.NewItemHandler(deps.Libraries, deps.Images, deps.Metadata, deps.UserData, deps.Chapters, deps.ExternalIDs, deps.People, trickplayDir, deps.Logger)

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
					r.Post("/libraries/browse", libHandler.Browse)
				})

				// IPTV channels (within library routes)
				if deps.IPTV != nil {
					// Pass deps.IPTVTransmux as-is — when nil the handler
					// falls back to the raw passthrough proxy, which is
					// the correct degraded-but-functional behaviour for
					// HLS-only deployments without ffmpeg.
					iptvHandler := handlers.NewIPTVHandler(deps.IPTV, deps.IPTVProxy, deps.IPTVTransmux, deps.IPTVLogoCache, deps.LibraryRepo, deps.Libraries, deps.Logger)

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
						r.Route("/libraries/{id}/iptv", func(r chi.Router) {
							r.Post("/refresh-m3u", iptvHandler.RefreshM3U)
							r.Post("/refresh-epg", iptvHandler.RefreshEPG)
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
				r.Get("/items/search", itemHandler.Search)
				r.Route("/items/{id}", func(r chi.Router) {
					r.Get("/", itemHandler.Get)
					r.Get("/children", itemHandler.Children)
					// Trickplay (seek-bar thumbnail previews). The
					// first hit triggers ffmpeg generation; both
					// endpoints serve from disk on subsequent hits.
					r.Get("/trickplay.json", itemHandler.TrickplayManifest)
					r.Get("/trickplay.png", itemHandler.TrickplaySprite)
				})
			}

			// Image management
			if deps.Images != nil && deps.Providers != nil && deps.ExternalIDs != nil {
				imageDir := filepath.Join(filepath.Dir(deps.Config.Database.Path), "images")
				imageRefresher := library.NewImageRefresher(
					deps.Items, deps.ExternalIDs, deps.Images, deps.Providers,
					pathmap.New(imageDir), imageDir, deps.Logger,
				)
				imgHandler := handlers.NewImageHandler(
					deps.Images, deps.ExternalIDs, deps.Items, deps.Providers,
					imageRefresher, imageDir, deps.Logger,
				)

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
