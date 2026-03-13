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
	"hubplay/internal/library"
	"hubplay/internal/user"
)

type Dependencies struct {
	Auth      *auth.Service
	Users     *user.Service
	Libraries *library.Service
	Config    *config.Config
	Logger    *slog.Logger
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
