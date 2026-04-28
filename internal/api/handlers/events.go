package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

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
	bus    EventBusSubscriber
	logger *slog.Logger
}

func NewEventHandler(bus EventBusSubscriber, logger *slog.Logger) *EventHandler {
	return &EventHandler{
		bus:    bus,
		logger: logger.With("module", "sse"),
	}
}

// Stream opens an SSE connection and forwards events to the client.
func (h *EventHandler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, r, http.StatusInternalServerError, "SSE_NOT_SUPPORTED", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Channel to receive events from the bus
	eventCh := make(chan event.Event, 64)

	// Subscribe to all relevant event types
	types := []event.Type{
		event.LibraryScanStarted,
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
	}

	// Collect unsubscribe funcs so we can detach every handler when the client
	// disconnects. Without this, each SSE connection leaks 12 handlers into
	// the bus for the lifetime of the process.
	unsubs := make([]func(), 0, len(types))
	for _, t := range types {
		t := t
		unsub := h.bus.Subscribe(t, func(e event.Event) {
			select {
			case eventCh <- e:
			default:
				// Drop event if channel is full (slow client)
			}
		})
		unsubs = append(unsubs, unsub)
	}
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()

	h.logger.Info("SSE client connected", "remote_addr", r.RemoteAddr)

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
			h.logger.Info("SSE client disconnected", "remote_addr", r.RemoteAddr)
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
