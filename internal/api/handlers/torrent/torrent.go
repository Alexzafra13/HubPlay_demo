// Package torrenthandler exposes the torrent-streaming feature over HTTP:
// a free-text search and a streaming endpoint that plays a result (or an
// operator-supplied magnet / .torrent URL) while it downloads. The
// built-in search provider is Internet Archive, but the provider registry
// (internal/torrentstream) is extensible. `/stream` accepts any magnet or
// http(s) .torrent URL — http(s) fetches are SSRF-guarded by the engine
// (imaging.SafeGet), so an arbitrary URL can't reach internal services.
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
	"hubplay/internal/auth"
	"hubplay/internal/provider"
	"hubplay/internal/torrentstream"
)

// Manager is the slice of *torrentstream.Manager the handler needs. An
// interface keeps the handler testable (a stub can stand in for the live
// engine).
type Manager interface {
	Search(ctx context.Context, query string, limit int) ([]torrentstream.SearchResult, error)
	// GetActive returns an already-running session without starting a
	// download — the "play what's live" path open to any user.
	GetActive(src string) (*torrentstream.Session, bool)
	// GetOrStart starts a download if needed — the bandwidth-spending
	// path, gated to admins.
	GetOrStart(ctx context.Context, uri string) (*torrentstream.Session, error)
}

// MetadataSearcher is the slice of *provider.Manager used to ENRICH the
// catalogue browse with TMDb metadata (poster / overview / year / id).
// It's metadata only — it does not resolve playable sources; sources come
// from the legal provider registry via Manager.Search. nil when no
// metadata provider is configured.
type MetadataSearcher interface {
	SearchMetadata(ctx context.Context, query provider.SearchQuery) ([]provider.SearchResult, error)
}

type Handler struct {
	mgr        Manager
	meta       MetadataSearcher
	adminCheck func(*http.Request) bool
	logger     *slog.Logger
}

// NewHandler builds the handler. meta may be nil (no TMDb configured →
// /discover returns 503). adminCheck reports whether the caller may START
// a torrent (spend bandwidth); pass nil to use the default claims role
// check.
func NewHandler(mgr Manager, meta MetadataSearcher, adminCheck func(*http.Request) bool, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	if adminCheck == nil {
		adminCheck = claimsAdmin
	}
	return &Handler{mgr: mgr, meta: meta, adminCheck: adminCheck, logger: logger}
}

// claimsAdmin is the default "may start a download" gate: admin role.
func claimsAdmin(r *http.Request) bool {
	c := auth.GetClaims(r.Context())
	return c != nil && c.Role == "admin"
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
	if h.mgr == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "TORRENT_DISABLED",
			"torrent streaming is not enabled on this server")
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

	results, err := h.mgr.Search(ctx, query, limit)
	if err != nil {
		h.logger.Warn("torrent search failed", "query", query, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "SEARCH_FAILED", "could not search the catalogue")
		return
	}
	handlers.RespondData(w, http.StatusOK, results)
}

// discoverResult is one enriched movie candidate for the browse grid.
type discoverResult struct {
	TMDbID    string `json:"tmdb_id"`
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	Overview  string `json:"overview,omitempty"`
	PosterURL string `json:"poster_url,omitempty"`
}

// Discover runs a metadata (TMDb) search so the UI can present a poster
// grid the user picks from ("by id" UX). Picking a result then resolves
// playable sources via /torrent/search — sourcing stays on the legal
// provider registry, this is metadata only.
//
// GET /torrent/discover?q=<text>
func (h *Handler) Discover(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_QUERY", "q parameter required")
		return
	}
	if h.meta == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "NO_METADATA_PROVIDER",
			"no metadata provider configured (set up TMDb to enable enriched discovery)")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	results, err := h.meta.SearchMetadata(ctx, provider.SearchQuery{
		Title:    query,
		ItemType: provider.ItemMovie,
	})
	if err != nil {
		h.logger.Warn("torrent discover failed", "query", query, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "DISCOVER_FAILED", "could not search metadata")
		return
	}

	out := make([]discoverResult, 0, len(results))
	for _, m := range results {
		out = append(out, discoverResult{
			TMDbID:    m.ExternalID,
			Title:     m.Title,
			Year:      m.Year,
			Overview:  m.Overview,
			PosterURL: m.PosterURL,
		})
	}
	handlers.RespondData(w, http.StatusOK, out)
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

	// Bandwidth model: a torrent's download is shared across all viewers,
	// so only admins may START a new one (spend the bandwidth). Any user
	// can play one that's already active — that just serves the local
	// HTTP stream, no extra download.
	var (
		sess *torrentstream.Session
		err  error
	)
	if h.adminCheck(r) {
		sess, err = h.mgr.GetOrStart(r.Context(), src)
	} else {
		var ok bool
		sess, ok = h.mgr.GetActive(src)
		if !ok {
			handlers.RespondError(w, r, http.StatusForbidden, "TORRENT_NOT_STARTED",
				"this content is not active; ask an administrator to add it")
			return
		}
	}
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

// isAllowedSource gates the source *scheme*: a magnet, or an http(s)
// .torrent URL. It is not tied to any single catalogue — the operator
// chooses what to add. SSRF safety for http(s) URLs (blocking
// loopback/LAN/metadata) is enforced downstream by imaging.SafeGet in the
// engine, so this only rejects non-fetchable schemes (ftp, file, …).
func isAllowedSource(src string) bool {
	return strings.HasPrefix(src, "magnet:") ||
		strings.HasPrefix(src, "http://") ||
		strings.HasPrefix(src, "https://")
}
