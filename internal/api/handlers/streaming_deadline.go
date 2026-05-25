package handlers

import (
	"net/http"
	"time"
)

// DisableWriteDeadline clears el per-request write deadline so a
// long-lived handler (HLS streaming, SSE, big file download, peer
// stream proxy) can write for an indefinite period sin the
// helper.
func DisableWriteDeadline(w http.ResponseWriter) error {
	// time.Time{} (cero) le dice al servidor "no deadline".
	return http.NewResponseController(w).SetWriteDeadline(time.Time{})
}
