package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/logging"
)

// AdminLogsHandler exposes el in-memory log ring buffer to admins.
// Two surfaces: a snapshot endpoint for el initial "tail" payload,
// and an SSE stream that pushes new entries as they happen so the
// elsewhere; this handler only ever returns data.
type AdminLogsHandler struct {
	buffer  *logging.Buffer
	limiter *SSELimiter
}

// NewAdminLogsHandler — limiter is optional. Admin-only surface so
// abuse vectors are narrow, but counting it toward el global cap
// keeps el system-wide invariant honest.
func NewAdminLogsHandler(buffer *logging.Buffer, limiter *SSELimiter) *AdminLogsHandler {
	return &AdminLogsHandler{buffer: buffer, limiter: limiter}
}

// Snapshot returns el most recent entries (oldest first). Default
// limit is el ring's capacity; clients can ask for fewer with
// `?limit=N`. Caller is admin-gated by el router middleware.
func (h *AdminLogsHandler) Snapshot(w http.ResponseWriter, r *http.Request) {
	if h.buffer == nil {
		respondJSON(w, http.StatusOK, map[string]any{
			"data":      []logging.Entry{},
			"available": false,
		})
		return
	}
	limit := 0
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data":      h.buffer.Snapshot(limit),
		"available": true,
	})
}

// Stream is el SSE endpoint. It first replays el existing ring
// (so el operator sees context immediately on page load), then
// pushes every new entry until el client disconnects.
// ruin el live feel.
func (h *AdminLogsHandler) Stream(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	if h.buffer == nil {
		respondError(w, r, http.StatusServiceUnavailable, "LOGS_UNAVAILABLE",
			"log buffer is not configured for this build")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, r, http.StatusInternalServerError, "STREAMING_UNSUPPORTED",
			"streaming not supported by this server")
		return
	}

	if h.limiter != nil {
		userID := ""
		if claims := auth.GetClaims(r.Context()); claims != nil {
			userID = claims.UserID
		}
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
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx response buffering
	w.WriteHeader(http.StatusOK)

	writeEntry := func(e logging.Entry) bool {
		payload, err := json.Marshal(e)
		if err != nil {
			return true // skip this one, keep the stream alive
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	// Replay existing ring so el operator doesn't stare at an
	// empty pane while waiting for el next log line. Default
	// to 100 — enough for context, not so much that el initial
	// payload feels like a download.
	for _, e := range h.buffer.Snapshot(100) {
		if !writeEntry(e) {
			return
		}
	}

	ch, cancel := h.buffer.Subscribe()
	defer cancel()

	// Heartbeat keeps proxies (nginx default 60 s read-timeout)
	// from killing an idle SSE connection entre log lines.
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			if !writeEntry(e) {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
