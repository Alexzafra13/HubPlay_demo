package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/iptv"
	"hubplay/internal/library"
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
	UserData       *db.UserDataRepository
	Config         *config.Config
	Logger         *slog.Logger
}

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	// Middleware stack (order matters)
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(RequestLogger(deps.Logger))
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Retry-After"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Handlers
	authHandler := handlers.NewAuthHandler(deps.Auth, deps.Users, deps.Logger)
	userHandler := handlers.NewUserHandler(deps.Users, deps.Logger)
	healthHandler := handlers.NewHealthHandler()

	// Public routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health check (no auth)
		r.Get("/health", healthHandler.Health)

		// Auth (no auth required)
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/refresh", authHandler.Refresh)

		// Setup — create first admin (only works when no users exist)
		r.Post("/auth/setup", authHandler.Setup)

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(deps.Auth.Middleware)

			// Auth
			r.Post("/auth/logout", authHandler.Logout)

			// Current user
			r.Get("/me", userHandler.Me)

			// Users (admin only)
			r.Route("/users", func(r chi.Router) {
				r.Use(auth.RequireAdmin)
				r.Get("/", userHandler.List)
				r.Post("/", authHandler.Register)
			})

			// Watch Progress & User Engagement
			if deps.UserData != nil {
				progressHandler := handlers.NewProgressHandler(deps.UserData, deps.Logger)

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
				libHandler := handlers.NewLibraryHandler(deps.Libraries, deps.Logger)
				itemHandler := handlers.NewItemHandler(deps.Libraries, deps.Logger)

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
				})

				// IPTV channels (within library routes)
				if deps.IPTV != nil {
					iptvHandler := handlers.NewIPTVHandler(deps.IPTV, deps.IPTVProxy, deps.Logger)

					r.Route("/libraries/{id}/channels", func(r chi.Router) {
						r.Get("/", iptvHandler.ListChannels)
						r.Get("/groups", iptvHandler.Groups)
					})

					r.Route("/channels/{channelId}", func(r chi.Router) {
						r.Get("/", iptvHandler.GetChannel)
						r.Get("/stream", iptvHandler.Stream)
						r.Get("/schedule", iptvHandler.Schedule)
					})

					r.Get("/channels/schedule", iptvHandler.BulkSchedule)

					// Admin IPTV operations
					r.Group(func(r chi.Router) {
						r.Use(auth.RequireAdmin)
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
		})
	})

	return r
}
