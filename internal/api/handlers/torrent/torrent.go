// Package torrenthandler exposes the legal torrent-streaming feature over
// HTTP: a free-text search against Internet Archive and a streaming
// endpoint that plays a result (or an operator-supplied magnet) while it
// downloads. There is NO third-party indexer integration — `/search`
// hits Internet Archive only, and `/stream` accepts magnets or
// archive.org .torrent URLs, nothing else.
package torrenthandler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hubplay/internal/api/handlers"
	"hubplay/internal/torrentstream"
)

// Manager is the slice of *torrentstream.Manager the handler needs. An
// interface keeps the handler testable (validation paths can run against
// a nil/stub manager).
type Manager interface {
	GetOrStart(ctx context.Context, uri string) (*torrentstream.Session, error)
}

type Handler struct {
	mgr    Manager
	logger *slog.Logger
}

func NewHandler(mgr Manager, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{mgr: mgr, logger: logger}
}

// Search runs a free-text query against the legal catalogue (Internet
// Archive) and returns streamable results.
//
// GET /torrent/search?q=<text>&limit=<n>
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_QUERY", "q parameter required")
		return
	}
	limit := 30
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	results, err := torrentstream.SearchArchive(ctx, query, limit)
	if err != nil {
		h.logger.Warn("torrent search failed", "query", query, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "SEARCH_FAILED", "could not search the catalogue")
		return
	}
	handlers.RespondData(w, http.StatusOK, results)
}

// Stream resolves the source (magnet or archive.org .torrent URL) and
// serves the main file with Range support so the browser can play it
// while it downloads.
//
// GET /torrent/stream?src=<magnet|archive.org .torrent URL>
func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt out of the global 30s write deadline — a
	// cold torrent can take longer than that to surface the first bytes.
	_ = handlers.DisableWriteDeadline(w)

	src := strings.TrimSpace(r.URL.Query().Get("src"))
	if src == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_SOURCE", "src parameter required")
		return
	}
	if !isAllowedSource(src) {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_SOURCE",
			"src must be a magnet link or an archive.org .torrent URL")
		return
	}
	if h.mgr == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "TORRENT_DISABLED",
			"torrent streaming is not enabled on this server")
		return
	}

	sess, err := h.mgr.GetOrStart(r.Context(), src)
	if err != nil {
		switch {
		case errors.Is(err, torrentstream.ErrTooManySessions):
			w.Header().Set("Retry-After", "10")
			handlers.RespondError(w, r, http.StatusServiceUnavailable, "TORRENT_BUSY",
				"server is at maximum simultaneous torrent sessions; retry shortly")
		case errors.Is(err, torrentstream.ErrMetadataTimeout):
			handlers.RespondError(w, r, http.StatusGatewayTimeout, "TORRENT_METADATA_TIMEOUT",
				"could not resolve torrent metadata (no reachable peers)")
		case errors.Is(err, torrentstream.ErrNoFiles):
			handlers.RespondError(w, r, http.StatusUnprocessableEntity, "TORRENT_EMPTY",
				"torrent has no playable files")
		case errors.Is(err, r.Context().Err()):
			// client gave up — nothing to write
		default:
			h.logger.Error("torrent GetOrStart", "error", err)
			handlers.RespondError(w, r, http.StatusBadGateway, "TORRENT_FAILED",
				"could not start torrent session")
		}
		return
	}

	sess.Touch()
	reader := sess.Reader()
	defer reader.Close() //nolint:errcheck

	w.Header().Set("Content-Type", sess.ContentType())
	// http.ServeContent gives us Range support (seeking) over the
	// sequential torrent reader. ModTime is "now" — the content is a
	// live download, not a cacheable static asset.
	http.ServeContent(w, r, sess.FileName(), time.Now(), reader)
}

// isAllowedSource restricts streaming to magnets and archive.org torrent
// URLs. This keeps the .torrent fetch from becoming an open SSRF (it can
// only reach archive.org) and ties the HTTP path to the legal catalogue.
func isAllowedSource(src string) bool {
	if strings.HasPrefix(src, "magnet:") {
		return true
	}
	return strings.HasPrefix(src, "https://archive.org/") ||
		strings.HasPrefix(src, "https://ia") && strings.Contains(src, ".archive.org/")
}
