package api

import (
	authhandler "hubplay/internal/api/handlers/auth"
	"hubplay/internal/api/handlers/me"
	"hubplay/internal/api/handlers/users"

	"github.com/go-chi/chi/v5"
)

// mountAuthProtected registra los endpoints de auth que requieren sesión
// previa: logout y device-approve. Vive en el grupo protegido del router
// (deps.Auth.Middleware) — el operador tiene que estar logueado para
// confirmar un device code.
func mountAuthProtected(
	r chi.Router,
	authHandler *authhandler.AuthHandler,
	deviceHandler *authhandler.DeviceAuthHandler,
) {
	r.Post("/auth/logout", authHandler.Logout)
	if deviceHandler != nil {
		r.Post("/auth/device/approve", deviceHandler.Approve)
	}
}

// mountSSEEvents registra el server-sent events stream global y el
// user-scoped (/me/events). Ambos requieren EventBus; sin él los tests
// minimalistas no montan ninguna ruta SSE.
func mountSSEEvents(r chi.Router, deps Dependencies) {
	if deps.EventBus == nil {
		return
	}
	eventHandler := me.NewEventHandler(deps.EventBus, deps.SSELimiter, deps.Logger)
	r.Get("/events", eventHandler.Stream)

	// User-scoped SSE: cross-device sync de progreso de visualizado,
	// played, favourites. El handler filtra por claims.UserID así
	// que otros usuarios del mismo server nunca ven estos events.
	meEventsHandler := me.NewMeEventsHandler(deps.EventBus, deps.SSELimiter, deps.Logger)
	r.Get("/me/events", meEventsHandler.Stream)
}

// mountMeIdentity registra los endpoints "yo": información del usuario
// actual, password, avatar, perfiles, sesiones. Todos derivan el userID
// del claim del JWT — no hay path param que manipular.
func mountMeIdentity(
	r chi.Router,
	authHandler *authhandler.AuthHandler,
	userHandler *users.UserHandler,
) {
	r.Get("/me", userHandler.Me)
	r.Post("/me/password", authHandler.ChangeMyPassword)
	// Avatar subido por el propio usuario. POST recibe el multipart
	// (campo "avatar"); el service resize + persiste y devuelve la
	// URL pública nueva (cambia en cada upload para forzar refetch
	// del navegador). DELETE es idempotente.
	r.Post("/me/avatar", userHandler.UploadMyAvatar)
	r.Delete("/me/avatar", userHandler.DeleteMyAvatar)
	r.Get("/me/profiles", authHandler.ListProfiles)
	r.Post("/auth/switch-profile", authHandler.SwitchProfile)
	r.Get("/me/sessions", authHandler.ListMySessions)
	r.Delete("/me/sessions/{id}", authHandler.RevokeMySession)
}

// mountMeNotificationsAndPreferences registra el inbox de notificaciones
// (migration 049, genérico — cualquier feature emite) y el almacén de
// preferencias por usuario (hero mode, theme overrides, etc.). Ambos
// son opcionales: tests minimalistas pasan nil y los endpoints no se
// montan.
func mountMeNotificationsAndPreferences(r chi.Router, deps Dependencies) {
	if deps.Notifications != nil {
		notifHandler := me.NewNotificationsHandler(deps.Notifications, deps.Logger)
		r.Get("/me/notifications", notifHandler.List)
		r.Post("/me/notifications/{id}/read", notifHandler.MarkRead)
		r.Post("/me/notifications/read-all", notifHandler.MarkAllRead)
	}

	if deps.UserPreferences != nil {
		prefsHandler := me.NewPreferencesHandler(deps.UserPreferences, deps.Logger)
		r.Get("/me/preferences", prefsHandler.ListMine)
		r.Put("/me/preferences/{key}", prefsHandler.SetMine)
		r.Delete("/me/preferences/{key}", prefsHandler.DeleteMine)
	}
}

// mountWatchProgress registra los endpoints de progreso de visualizado:
// continue-watching, favoritos, next-up, y el sub-route /me/progress/{itemId}
// para get/update/marcar played-unplayed-favorite.
func mountWatchProgress(r chi.Router, deps Dependencies) {
	if deps.UserData == nil {
		return
	}
	progressHandler := me.NewProgressHandler(deps.UserData, deps.Images, deps.EventBus, deps.Logger)

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

// mountHome registra la customización de la home + los rails de
// discovery. Vive junto al resto de /me/* porque cada handler está
// scoped al caller — layout, trending, y live-now son todos per-user
// (trending filtra por libraries accesibles, live-now hace JOIN
// favourites + access).
func mountHome(r chi.Router, deps Dependencies) {
	if deps.Home == nil || deps.UserPreferences == nil || deps.LibraryRepo == nil || deps.Items == nil {
		return
	}
	homeHandler := me.NewHomeHandler(
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
