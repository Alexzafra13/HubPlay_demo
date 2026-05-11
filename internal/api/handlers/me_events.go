package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/event"
)

// MeEventsHandler exposes user-scoped events as a Server-Sent Events
// stream. Pairs with EventHandler (`/api/v1/events`) which is global —
// admin-style notifications anyone can see (library scans, channel
// health, EPG refreshes). The events served HERE carry per-user state
// (watch progress, favourites, played) and MUST be filtered to the
// authenticated user before fan-out so device A's progress on Stranger
// Things never leaks to user B who happens to share the server.
//
// The cross-device sync use case: I start an episode on the laptop,
// progress gets persisted server-side, the server publishes a
// `user.progress.updated` event with my user_id; my phone, which has
// /me/events open in the background, receives the event and
// invalidates its TanStack queries — Continue Watching jumps to that
// episode within ~50ms instead of waiting for the next 60s staleTime
// to elapse.
type MeEventsHandler struct {
	bus     EventBusSubscriber
	limiter *SSELimiter
	logger  *slog.Logger
}

// NewMeEventsHandler — limiter is optional. See NewEventHandler doc;
// the per-user cap matters most here because /me/events is the only
// SSE surface a regular (non-admin) user controls directly.
func NewMeEventsHandler(bus EventBusSubscriber, limiter *SSELimiter, logger *slog.Logger) *MeEventsHandler {
	return &MeEventsHandler{
		bus:     bus,
		limiter: limiter,
		logger:  logger.With("module", "sse-me"),
	}
}

// userScopedEventTypes is the set of events the user-scoped SSE
// stream forwards. Adding a new type here means: (a) the publisher
// stamps user_id into Data, (b) the frontend hook routes the type to
// the right query invalidations. Keeping this list narrow is on
// purpose — the global /events stream is the right home for anything
// that doesn't carry per-user identity.
var userScopedEventTypes = []event.Type{
	event.ProgressUpdated,
	event.PlayedToggled,
	event.FavoriteToggled,
	// Auth session lifecycle — drives the "Tus dispositivos" panel.
	// Both events already carry user_id in Data (see auth.Service.Login
	// and Logout / RevokeSession), so the per-user filter below treats
	// them like any other user-scoped event.
	event.UserLoggedIn,
	event.UserLoggedOut,
}

// Stream opens an SSE connection scoped to the authenticated user.
// Filters every published event by Data["user_id"] before writing to
// the client; events for other users are dropped silently at the
// subscription handler. The shape of the JSON written to the client
// matches what the global /events handler emits, so the same frontend
// EventSource code path consumes both.
func (h *MeEventsHandler) Stream(w http.ResponseWriter, r *http.Request) {
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

	// Acquire before flushing headers; once the response starts we
	// can't return a 503. Per-user cap protects against runaway
	// reconnect loops (the most common failure mode is a stale tab
	// retrying every 100ms after the bearer token expired).
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
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Buffered channel decouples the bus dispatch goroutine from the
	// HTTP write loop. 64 is the same depth the global /events handler
	// uses — a slow client that can't drain that fast will see events
	// dropped at the `default` branch below, which is the right
	// behaviour: better to lose a tick than to block the bus dispatch
	// for every connected client.
	eventCh := make(chan event.Event, 64)
	unsubs := make([]func(), 0, len(userScopedEventTypes))
	for _, t := range userScopedEventTypes {
		t := t
		unsub := h.bus.Subscribe(t, func(e event.Event) {
			// User-scope filter: drop events for other users at the
			// subscription handler, before they ever land in the
			// per-connection channel. Saves the channel slot and
			// avoids a wakeup of the write goroutine for events the
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
