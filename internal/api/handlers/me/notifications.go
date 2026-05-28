package me

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	"hubplay/internal/domain"
	"hubplay/internal/notification"
)

// NotificationsHandler expone el inbox por usuario:
//
//	GET    /me/notifications              listing + unread_count
//	POST   /me/notifications/{id}/read    marca una como leida
//	POST   /me/notifications/read-all     marca todas como leidas
//
// Todas las rutas son auth-gated (claims.UserID es el dueño del
// inbox). El service hace un WHERE user_id extra como defense-in-
// depth para que un id de notificacion robado no permita acceso
// cruzado.
type NotificationsHandler struct {
	svc    *notification.Service
	logger *slog.Logger
}

func NewNotificationsHandler(svc *notification.Service, logger *slog.Logger) *NotificationsHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &NotificationsHandler{svc: svc, logger: logger.With("handler", "notifications")}
}

// notificationWire es el shape JSON al frontend. payload se entrega
// como objeto JSON cuando el backend persistio un JSON valido; si
// no, va como string para no romper consumidores con datos legacy.
type notificationWire struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Link      string `json:"link,omitempty"`
	Payload   any    `json:"payload,omitempty"`
	CreatedAt string `json:"created_at"`
	ReadAt    string `json:"read_at,omitempty"`
}

func notificationToWire(n *notification.Notification) notificationWire {
	w := notificationWire{
		ID:        n.ID,
		Kind:      string(n.Kind),
		Title:     n.Title,
		Body:      n.Body,
		Link:      n.Link,
		CreatedAt: n.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if n.ReadAt != nil {
		w.ReadAt = n.ReadAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if n.Payload != "" {
		var parsed any
		if err := json.Unmarshal([]byte(n.Payload), &parsed); err == nil {
			w.Payload = parsed
		} else {
			w.Payload = n.Payload
		}
	}
	return w
}

// List devuelve {data:[...], unread_count:N}. unread_count va en la
// misma respuesta para que el frontend evite un segundo round-trip
// — el componente Bell + el panel se hidratan con un solo fetch.
func (h *NotificationsHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	notifs, err := h.svc.ListForUser(r.Context(), claims.UserID, 50)
	if err != nil {
		h.logger.Error("list notifications", "user_id", claims.UserID, "error", err)
		handlers.RespondError(w, r, http.StatusInternalServerError, "INTERNAL", "failed to list notifications")
		return
	}
	unread, err := h.svc.UnreadCountForUser(r.Context(), claims.UserID)
	if err != nil {
		h.logger.Error("count unread notifications", "user_id", claims.UserID, "error", err)
		handlers.RespondError(w, r, http.StatusInternalServerError, "INTERNAL", "failed to count notifications")
		return
	}
	out := make([]notificationWire, 0, len(notifs))
	for _, n := range notifs {
		out = append(out, notificationToWire(n))
	}
	handlers.RespondJSON(w, http.StatusOK, map[string]any{
		"data":         out,
		"unread_count": unread,
	})
}

// MarkRead — POST /me/notifications/{id}/read. Idempotente: si la
// notif ya estaba leida o no existe, 204 igual (el handler de
// errores convierte domain.ErrNotFound a 204 silencioso porque
// el dropdown puede llamar varias veces durante la misma sesion).
func (h *NotificationsHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	id := handlers.RequireParam(w, r, "id")
	if id == "" {
		return
	}
	if err := h.svc.MarkRead(r.Context(), id, claims.UserID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// No revelamos si era de otro user vs no existia vs ya leida
			// — todos los casos resultan en "no hay nada que cambiar".
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.logger.Error("mark notification read", "user_id", claims.UserID, "id", id, "error", err)
		handlers.RespondError(w, r, http.StatusInternalServerError, "INTERNAL", "failed to mark notification read")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MarkAllRead — POST /me/notifications/read-all. Devuelve cuantas
// se actualizaron por si el frontend quiere mostrar "5 marcadas".
func (h *NotificationsHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	n, err := h.svc.MarkAllReadForUser(r.Context(), claims.UserID)
	if err != nil {
		h.logger.Error("mark all notifications read", "user_id", claims.UserID, "error", err)
		handlers.RespondError(w, r, http.StatusInternalServerError, "INTERNAL", "failed to mark all read")
		return
	}
	handlers.RespondJSON(w, http.StatusOK, map[string]any{
		"marked_count": n,
	})
}
