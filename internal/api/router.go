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
	Items          *db.ItemRepository
	MediaStreams    *db.MediaStreamRepository
	Images         *db.ImageRepository
	Metadata       *db.MetadataRepository
	UserData       *db.UserDataRepository
	Providers      *provider.Manager
	ExternalIDs    *db.ExternalIDRepository
	LibraryRepo    *db.LibraryRepository
	ProviderRepo   *db.ProviderRepository
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

			// Users (admin only)
			r.Route("/users", func(r chi.Router) {
				r.Use(auth.RequireAdmin)
				r.Get("/", userHandler.List)
				r.Post("/", authHandler.Register)
				r.Delete("/{id}", userHandler.Delete)
			})

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
					deps.Config.Server.BaseURL, deps.Logger,
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
				})
			}

			// Libraries & Items (only if service is wired)
			if deps.Libraries != nil {
				libHandler := handlers.NewLibraryHandler(deps.Libraries, deps.Images, deps.Metadata, deps.Logger)
				itemHandler := handlers.NewItemHandler(deps.Libraries, deps.Images, deps.Metadata, deps.Logger)

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
					iptvHandler := handlers.NewIPTVHandler(deps.IPTV, deps.IPTVProxy, deps.LibraryRepo, deps.Logger)

					r.Route("/libraries/{id}/channels", func(r chi.Router) {
						r.Get("/", iptvHandler.ListChannels)
						r.Get("/groups", iptvHandler.Groups)
					})

					r.Route("/channels/{channelId}", func(r chi.Router) {
						r.Get("/", iptvHandler.GetChannel)
						r.Get("/stream", iptvHandler.Stream)
						r.Get("/proxy", iptvHandler.ProxyURL)
						r.Get("/schedule", iptvHandler.Schedule)
					})

					r.Get("/channels/schedule", iptvHandler.BulkSchedule)

					// Public IPTV
					r.Get("/iptv/public/countries", iptvHandler.PublicCountries)

					// Admin IPTV operations
					r.Group(func(r chi.Router) {
						r.Use(auth.RequireAdmin)
						r.Post("/iptv/public/import", iptvHandler.ImportPublicIPTV)
						r.Route("/libraries/{id}/iptv", func(r chi.Router) {
							r.Post("/refresh-m3u", iptvHandler.RefreshM3U)
							r.Post("/refresh-epg", iptvHandler.RefreshEPG)
						})
					})
				}

				// Items
				r.Get("/items/latest", libHandler.LatestItems)
				r.Get("/items/search", itemHandler.Search)
				r.Route("/items/{id}", func(r chi.Router) {
					r.Get("/", itemHandler.Get)
					r.Get("/children", itemHandler.Children)
				})
			}

			// Image management
			if deps.Images != nil && deps.Providers != nil && deps.ExternalIDs != nil {
				imageDir := filepath.Join(filepath.Dir(deps.Config.Database.Path), "images")
				imgHandler := handlers.NewImageHandler(deps.Images, deps.ExternalIDs, deps.Items, deps.Providers, imageDir, deps.Logger)

				// Image management (nested under items)
				r.Route("/items/{id}/images", func(r chi.Router) {
					r.Get("/", imgHandler.List)
					r.Get("/available", imgHandler.Available)
					r.Put("/{type}/select", imgHandler.Select)
					r.Post("/{type}/upload", imgHandler.Upload)
					r.Put("/{imageId}/primary", imgHandler.SetPrimary)
					r.Delete("/{imageId}", imgHandler.Delete)
				})

				// Serve local image files
				r.Get("/images/file/{id}", imgHandler.ServeFile)

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
