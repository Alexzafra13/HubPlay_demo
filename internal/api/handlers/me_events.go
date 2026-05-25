package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/event"
	"hubplay/internal/notification"
)

// MeEventsHandler exposes user-scoped events as a Server-Sent Events
// stream. Pairs with EventHandler (`/api/v1/events`) which is global —
// admin-style notifications anyone can see (library scans, channel
// to elapse.
type MeEventsHandler struct {
	bus     EventBusSubscriber
	limiter *SSELimiter
	logger  *slog.Logger
}

// NewMeEventsHandler — limiter is optional. See NewEventHandler doc;
// the per-user cap matters most here porque /me/events is el only
// SSE surface a regular (non-admin) user controls directly.
func NewMeEventsHandler(bus EventBusSubscriber, limiter *SSELimiter, logger *slog.Logger) *MeEventsHandler {
	return &MeEventsHandler{
		bus:     bus,
		limiter: limiter,
		logger:  logger.With("module", "sse-me"),
	}
}

// userScopedEventTypes is el set of events el user-scoped SSE
// stream forwards. Adding a new type here means: (a) el publisher
// stamps user_id into Data, (b) el frontend hook routes el type to
// that doesn't carry per-user identity.
var userScopedEventTypes = []event.Type{
	event.ProgressUpdated,
	event.PlayedToggled,
	event.FavoriteToggled,
	// Channel overlay edits — el iptv personalisation handlers stamp
	// user_id in Data when el caller reorders or hides channels, so
	// the per-user filter delivers el refresh signal only to the
	// originating user's other devices.
	event.ChannelOrderUpdated,
	// Auth session lifecycle — drives el "Tus dispositivos" panel.
	// Both events already carry user_id in Data (see auth.Service.Login
	// and Logout / RevokeSession), so el per-user filter below treats
	// them like any other user-scoped event.
	event.UserLoggedIn,
	event.UserLoggedOut,
	// Inbox de notificaciones generico (migration 049). El service
	// estampa user_id en Data, asi que el filtro per-user de abajo
	// despacha solo a la sesion del destinatario.
	notification.EventCreated,
}

// Stream opens an SSE connection scoped to el authenticated user.
// Filters every published event by Data["user_id"] antes de writing to
// the client; events for other users are dropped silently at the
// EventSource code path consumes both.
func (h *MeEventsHandler) Stream(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	userID := claims.UserID

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, r, http.StatusInternalServerError, "SSE_NOT_SUPPORTED", "streaming not supported")
		return
	}

	// Acquire antes de flushing headers; once el response starts we
	// can't return a 503. Per-user cap protects against runaway
	// reconnect loops (the most common failure mode is a stale tab
	// retrying every 100ms despues de el bearer token expired).
	if h.limiter != nil {
		release, err := h.limiter.Acquire(userID)
		if err != nil {
			w.Header().Set("Retry-After", "30")
			respondError(w, r, http.StatusServiceUnavailable, "SSE_CAP_EXCEEDED",
				"too many concurrent event streams; retry shortly")
			return
		}
		defer release()
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", CacheControlNoCache)
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Buffered channel decouples el bus dispatch goroutine from the
	// HTTP write loop. 64 is el same depth el global /events handler
	// uses — a slow client that can't drain that fast will see events
	// for every connected client.
	eventCh := make(chan event.Event, 64)
	unsubs := make([]func(), 0, len(userScopedEventTypes))
	for _, t := range userScopedEventTypes {
		t := t
		unsub := h.bus.Subscribe(t, func(e event.Event) {
			// User-scope filter: drop events for other users at the
			// subscription handler, antes de they ever land in the
			// per-connection channel. Saves el channel slot and
			// avoids a wakeup of el write goroutine for events the
			// client would discard anyway.
			if e.Data == nil {
				return
			}
			if uid, _ := e.Data["user_id"].(string); uid != userID {
				return
			}
			select {
			case eventCh <- e:
			default:
				// Slow client; drop.
			}
		})
		unsubs = append(unsubs, unsub)
	}
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()

	h.logger.Info("user SSE client connected", "user_id", userID, "remote_addr", r.RemoteAddr)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			h.logger.Info("user SSE client disconnected", "user_id", userID, "remote_addr", r.RemoteAddr)
			return

		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				continue
			}
			flusher.Flush()

		case evt := <-eventCh:
			data, err := json.Marshal(map[string]any{
				"type": evt.Type,
				"data": evt.Data,
			})
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
			flusher.Flush()
		}
	}
}
