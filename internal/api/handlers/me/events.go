package me

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	"hubplay/internal/event"
)

// sseKeepaliveInterval keeps an idle SSE stream below the typical
// reverse-proxy idle cutoff (nginx default = 60s). Comment lines are
// invisible to EventSource consumers but reset the proxy idle timer.
// 25s leaves comfortable margin against jittery 30s upstream caps too.
const sseKeepaliveInterval = 25 * time.Second

// EventHandler provides a Server-Sent Events (SSE) endpoint for real-time updates.
// Clients connect via GET /api/v1/events and receive JSON events as they happen.
type EventHandler struct {
	bus     handlers.EventBusSubscriber
	limiter *handlers.SSELimiter
	logger  *slog.Logger
}

// NewEventHandler — limiter is optional. nil means no cap enforcement
// (test builds wire it that way); production passes the shared
// SSELimiter from the router so global + per-user counts are unified
// across every SSE surface.
func NewEventHandler(bus handlers.EventBusSubscriber, limiter *handlers.SSELimiter, logger *slog.Logger) *EventHandler {
	return &EventHandler{
		bus:     bus,
		limiter: limiter,
		logger:  logger.With("module", "sse"),
	}
}

// Stream opens an SSE connection and forwards events to the client.
func (h *EventHandler) Stream(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
	flusher, ok := w.(http.Flusher)
	if !ok {
		handlers.RespondError(w, r, http.StatusInternalServerError, "SSE_NOT_SUPPORTED", "streaming not supported")
		return
	}

	// Cap connections BEFORE the headers flush. Once the SSE response
	// starts there's no way to return a 503; the client is committed
	// to read forever. Tying the slot to claims.UserID means a single
	// user opening 200 tabs can't starve the global cap for everyone
	// else.
	if h.limiter != nil {
		userID := ""
		if claims := auth.GetClaims(r.Context()); claims != nil {
			userID = claims.UserID
		}
		release, err := h.limiter.Acquire(userID)
		if err != nil {
			w.Header().Set("Retry-After", "30")
			handlers.RespondError(w, r, http.StatusServiceUnavailable, "SSE_CAP_EXCEEDED",
				"too many concurrent event streams; retry shortly")
			return
		}
		defer release()
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", handlers.CacheControlNoCache)
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Channel to receive events from the bus
	eventCh := make(chan event.Event, 64)

	// Subscribe to all relevant event types
	types := []event.Type{
		event.LibraryScanStarted,
		event.LibraryScanProgress,
		event.LibraryScanCompleted,
		event.ItemAdded,
		event.ItemUpdated,
		event.ItemRemoved,
		event.MetadataUpdated,
		event.TranscodeStarted,
		event.TranscodeCompleted,
		event.ChannelAdded,
		event.ChannelRemoved,
		event.EPGUpdated,
		event.PlaylistRefreshed,
		event.PlaylistRefreshFailed,
		event.ChannelHealthChanged,
		event.SegmentDetectStarted,
		event.SegmentDetectProgress,
		event.SegmentDetectCompleted,
	}

	// Collect unsubscribe funcs so we can detach every handler when the client
	// disconnects. Without this, each SSE connection leaks 12 handlers into
	// the bus for the lifetime of the process.
	var sseDrops atomic.Int64
	unsubs := make([]func(), 0, len(types))
	for _, t := range types {
		t := t
		unsub := h.bus.Subscribe(t, func(e event.Event) {
			select {
			case eventCh <- e:
			default:
				sseDrops.Add(1)
			}
		})
		unsubs = append(unsubs, unsub)
	}
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()

	// Debug: red móvil reconecta seguido (cada cambio de wifi/4G). Un Info
	// por connect/disconnect satura los logs sin aportar a operación.
	h.logger.Debug("SSE client connected", "remote_addr", handlers.ClientIP(r))

	// Initial keepalive comment doubles as a "connection ready"
	// signal for the browser EventSource: anything we write before
	// it forces the headers to flush.
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			if d := sseDrops.Load(); d > 0 {
				h.logger.Warn("SSE events dropped (slow client)", "remote_addr", handlers.ClientIP(r), "dropped", d)
			}
			h.logger.Debug("SSE client disconnected", "remote_addr", handlers.ClientIP(r))
			return

		case <-keepalive.C:
			// Comment frames keep proxies happy without surfacing
			// anything to the EventSource API. A failed write here
			// means the client is gone — let the next ctx.Done()
			// tick clean up rather than panicking on a half-closed
			// writer.
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
