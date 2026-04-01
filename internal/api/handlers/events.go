package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"hubplay/internal/event"
)

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
		respondError(w, http.StatusInternalServerError, "SSE_NOT_SUPPORTED", "streaming not supported")
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
	}

	for _, t := range types {
		t := t
		h.bus.Subscribe(t, func(e event.Event) {
			select {
			case eventCh <- e:
			default:
				// Drop event if channel is full (slow client)
			}
		})
	}

	h.logger.Info("SSE client connected", "remote_addr", r.RemoteAddr)

	// Send keepalive comment
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			h.logger.Info("SSE client disconnected", "remote_addr", r.RemoteAddr)
			return

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
