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
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

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

// SourceSearcher is the slice of *torrentstream.SourceService the handler
// needs for source aggregation (/torrent/sources/*). nil when no Torznab
// indexer is configured. Resolve runs the hybrid search (imdbid + title)
// described by the Query.
type SourceSearcher interface {
	Resolve(ctx context.Context, q torrentstream.Query) ([]torrentstream.SearchResult, error)
}

// MetadataSearcher is the slice of *provider.Manager used to ENRICH the
// catalogue browse with TMDb metadata (poster / overview / year / id).
// It's metadata only — it does not resolve playable sources; sources come
// from the legal provider registry via Manager.Search. nil when no
// metadata provider is configured.
type MetadataSearcher interface {
	SearchMetadata(ctx context.Context, query provider.SearchQuery) ([]provider.SearchResult, error)
	FetchMetadata(ctx context.Context, externalID string, itemType provider.ItemType) (*provider.MetadataResult, error)
}

// VODPreparer is the slice of *torrentstream.VODTransmux the handler needs to
// decide how to deliver a torrent (direct play vs HLS remux) and to serve the
// remux output. nil when transcoding is off → /torrent/play falls back to a
// plain direct stream URL.
type VODPreparer interface {
	Prepare(ctx context.Context, sess torrentstream.Playable) (torrentstream.PlayResult, error)
	PlaylistPath(infohash string) (string, bool)
	SegmentPath(infohash, segment string) (string, bool)
}

type Handler struct {
	mgr        Manager
	sources    SourceSearcher
	meta       MetadataSearcher
	vod        VODPreparer
	adminCheck func(*http.Request) bool
	logger     *slog.Logger
}

// NewHandler builds the handler. mgr may be nil (streaming off → /search,
// /discover, /stream not used). sources may be nil (no Torznab indexer →
// /sources/* returns 503). meta may be nil (no TMDb configured →
// /discover returns 503). adminCheck reports whether the caller may START
// a torrent (spend bandwidth); pass nil to use the default claims role
// check.
func NewHandler(mgr Manager, sources SourceSearcher, meta MetadataSearcher, vod VODPreparer, adminCheck func(*http.Request) bool, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	if adminCheck == nil {
		adminCheck = claimsAdmin
	}
	return &Handler{mgr: mgr, sources: sources, meta: meta, vod: vod, adminCheck: adminCheck, logger: logger}
}

// imdbIDPattern is the canonical IMDb id form: "tt" followed by digits.
// We don't pin the digit count — IMDb has grown past 7–8 digits and will
// keep growing, so a fixed width would reject valid (newer) ids.
var imdbIDPattern = regexp.MustCompile(`^tt\d+$`)

// SourcesMovie resolves streamable sources for a movie by IMDb id.
//
// GET /torrent/sources/movie/{imdbId}
func (h *Handler) SourcesMovie(w http.ResponseWriter, r *http.Request) {
	h.sourcesByIMDb(w, r, torrentstream.MediaTypeMovie)
}

// SourcesSeries resolves streamable sources for a series by IMDb id.
//
// GET /torrent/sources/series/{imdbId}
func (h *Handler) SourcesSeries(w http.ResponseWriter, r *http.Request) {
	h.sourcesByIMDb(w, r, torrentstream.MediaTypeSeries)
}

// SourcesSearch runs a free-text search against the configured indexers
// (the general torrent search box).
//
// GET /torrent/sources/search?q=<text>&type=<movie|series>
func (h *Handler) SourcesSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_QUERY", "q parameter required")
		return
	}
	if h.sources == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "INDEXER_DISABLED",
			"no source indexer is configured on this server")
		return
	}
	mt := torrentstream.MediaTypeMovie
	if r.URL.Query().Get("type") == "series" {
		mt = torrentstream.MediaTypeSeries
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	results, err := h.sources.Resolve(ctx, torrentstream.Query{Type: mt, Title: query})
	if err != nil {
		h.logger.Warn("torrent text search failed", "query", query, "error", err)
		h.respondSourceError(w, r, err)
		return
	}
	handlers.RespondData(w, http.StatusOK, results)
}

// respondSourceError maps an aggregator error to an HTTP response: a clear
// "rate-limited, retry" for HTTP 429 from an indexer, otherwise a generic
// bad-gateway.
func (h *Handler) respondSourceError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, torrentstream.ErrRateLimited) {
		w.Header().Set("Retry-After", "10")
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "RATE_LIMITED",
			"the indexer is rate-limiting requests; try again in a few seconds")
		return
	}
	handlers.RespondError(w, r, http.StatusBadGateway, "INDEXER_UNAVAILABLE",
		"source indexer unavailable")
}

// sourcesByIMDb validates the IMDb id, queries the aggregator (cache →
// Torznab) and returns the normalised, sorted sources under the canonical
// {data} envelope.
func (h *Handler) sourcesByIMDb(w http.ResponseWriter, r *http.Request, mt torrentstream.MediaType) {
	imdbID := strings.TrimSpace(chi.URLParam(r, "imdbId"))
	if !imdbIDPattern.MatchString(imdbID) {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_IMDB_ID",
			"imdbId must be a valid IMDb id (e.g. tt0133093)")
		return
	}
	if h.sources == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "INDEXER_DISABLED",
			"no source indexer is configured on this server")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	// Optional title/year/season/episode let the client enrich the query so
	// the text-search pass (which most indexers actually answer) can run
	// alongside the imdbid pass. All optional — imdbid alone still works.
	q := torrentstream.Query{
		Type:    mt,
		IMDbID:  imdbID,
		Title:   strings.TrimSpace(r.URL.Query().Get("title")),
		Year:    atoiOr0(r.URL.Query().Get("year")),
		Season:  atoiOr0(r.URL.Query().Get("season")),
		Episode: atoiOr0(r.URL.Query().Get("episode")),
	}
	results, err := h.sources.Resolve(ctx, q)
	if err != nil {
		h.logger.Warn("torrent sources failed", "type", mt, "imdb", imdbID, "error", err)
		h.respondSourceError(w, r, err)
		return
	}
	handlers.RespondData(w, http.StatusOK, results)
}

// atoiOr0 parses a positive integer query param, returning 0 for empty or
// invalid input (the "not provided" sentinel for Query's numeric fields).
func atoiOr0(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
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

	itemType := provider.ItemMovie
	if r.URL.Query().Get("type") == "series" {
		itemType = provider.ItemSeries
	}
	results, err := h.meta.SearchMetadata(ctx, provider.SearchQuery{
		Title:    query,
		ItemType: itemType,
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

// DiscoverSources resolves the streamable sources for a TMDb pick: it looks
// up the title's IMDb id (the reliable, Torrentio-style key) and queries the
// indexers by imdbid; if there's no IMDb id it falls back to a title search.
//
// GET /torrent/discover/sources?type=<movie|series>&tmdb_id=<id>
func (h *Handler) DiscoverSources(w http.ResponseWriter, r *http.Request) {
	tmdbID := strings.TrimSpace(r.URL.Query().Get("tmdb_id"))
	if tmdbID == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_TMDB_ID", "tmdb_id required")
		return
	}
	if h.meta == nil || h.sources == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "INDEXER_DISABLED",
			"discovery sources are not available on this server")
		return
	}
	mt := torrentstream.MediaTypeMovie
	itemType := provider.ItemMovie
	if r.URL.Query().Get("type") == "series" {
		mt = torrentstream.MediaTypeSeries
		itemType = provider.ItemSeries
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	meta, err := h.meta.FetchMetadata(ctx, tmdbID, itemType)
	if err != nil || meta == nil {
		h.logger.Warn("discover sources: metadata fetch failed", "tmdb", tmdbID, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "DISCOVER_FAILED", "could not resolve title metadata")
		return
	}

	// Build a hybrid query: imdbid (precise) when TMDb gave us one, AND the
	// title+year so the text-search pass runs too — most indexers don't
	// answer imdbid lookups, so the title pass is what usually returns hits.
	q := torrentstream.Query{
		Type:    mt,
		Title:   meta.Title,
		Year:    meta.Year,
		Season:  atoiOr0(r.URL.Query().Get("season")),
		Episode: atoiOr0(r.URL.Query().Get("episode")),
	}
	if imdb := strings.TrimSpace(meta.ExternalIDs["imdb"]); imdbIDPattern.MatchString(imdb) {
		q.IMDbID = imdb
	}
	results, err := h.sources.Resolve(ctx, q)
	if err != nil {
		h.logger.Warn("discover sources failed", "tmdb", tmdbID, "error", err)
		h.respondSourceError(w, r, err)
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

	sess, ok := h.acquireSession(w, r, src)
	if !ok {
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

// acquireSession applies the bandwidth gate and maps engine errors to HTTP
// responses, returning ok=false when it has already written a response.
//
// Bandwidth model: a torrent's download is shared across all viewers, so only
// admins may START a new one (spend the bandwidth). Any user can play one
// that's already active — that just serves the local stream, no extra
// download.
func (h *Handler) acquireSession(w http.ResponseWriter, r *http.Request, src string) (*torrentstream.Session, bool) {
	if h.adminCheck(r) {
		sess, err := h.mgr.GetOrStart(r.Context(), src)
		if err != nil {
			h.writeStartError(w, r, err)
			return nil, false
		}
		return sess, true
	}
	sess, ok := h.mgr.GetActive(src)
	if !ok {
		handlers.RespondError(w, r, http.StatusForbidden, "TORRENT_NOT_STARTED",
			"this content is not active; ask an administrator to add it")
		return nil, false
	}
	return sess, true
}

// writeStartError maps a GetOrStart error to the right HTTP response.
func (h *Handler) writeStartError(w http.ResponseWriter, r *http.Request, err error) {
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
}

// playResponse tells the frontend how to play a source: "direct" → use url in
// a <video>; "hls" → load url with hls.js; "reencode" → not yet supported.
type playResponse struct {
	Mode string `json:"mode"`
	URL  string `json:"url,omitempty"`
}

// Play decides how a source should be delivered (direct play vs HLS remux)
// and returns the URL to load. It starts the torrent (admin) or joins an
// active one (any user), probes the main file and, for an H.264-in-MKV style
// release, spins up a cheap `-c copy` remux so the browser can play it.
//
// GET /torrent/play?src=<magnet|.torrent URL>
func (h *Handler) Play(w http.ResponseWriter, r *http.Request) {
	src := strings.TrimSpace(r.URL.Query().Get("src"))
	if src == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_SOURCE", "src parameter required")
		return
	}
	if !isAllowedSource(src) {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_SOURCE",
			"src must be a magnet link or an http(s) .torrent URL")
		return
	}
	if h.mgr == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "TORRENT_DISABLED",
			"torrent streaming is not enabled on this server")
		return
	}
	// No transcoder wired → everything is "direct" (browser plays what it can;
	// the existing /torrent/stream serves it).
	if h.vod == nil {
		handlers.RespondData(w, http.StatusOK, playResponse{Mode: "direct", URL: directStreamURL(src)})
		return
	}

	sess, ok := h.acquireSession(w, r, src)
	if !ok {
		return
	}

	res, err := h.vod.Prepare(r.Context(), sess)
	if err != nil {
		h.logger.Warn("torrent play prepare failed", "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "PLAY_PREPARE_FAILED",
			"could not prepare playback for this source")
		return
	}
	switch res.Mode {
	case torrentstream.PlayRemux:
		handlers.RespondData(w, http.StatusOK, playResponse{
			Mode: "hls",
			URL:  "/api/v1/torrent/hls/" + res.InfoHash + "/index.m3u8",
		})
	case torrentstream.PlayReencode:
		// Detected but not yet wired (P1b-2). Tell the UI so it can warn
		// instead of showing a black screen.
		handlers.RespondData(w, http.StatusOK, playResponse{Mode: "reencode"})
	default:
		handlers.RespondData(w, http.StatusOK, playResponse{Mode: "direct", URL: directStreamURL(src)})
	}
}

// directStreamURL builds the existing direct-play stream URL for a source.
func directStreamURL(src string) string {
	return "/api/v1/torrent/stream?src=" + url.QueryEscape(src)
}

// HLSPlaylist serves the index.m3u8 of an active remux session.
//
// GET /torrent/hls/{infohash}/index.m3u8
func (h *Handler) HLSPlaylist(w http.ResponseWriter, r *http.Request) {
	if h.vod == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "TRANSCODE_DISABLED", "transcoding is not enabled")
		return
	}
	path, ok := h.vod.PlaylistPath(chi.URLParam(r, "infohash"))
	if !ok {
		handlers.RespondError(w, r, http.StatusNotFound, "NOT_ACTIVE", "no active transcode for this content")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	http.ServeFile(w, r, path)
}

// HLSSegment serves one .ts segment of an active remux session.
//
// GET /torrent/hls/{infohash}/{segment}
func (h *Handler) HLSSegment(w http.ResponseWriter, r *http.Request) {
	if h.vod == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "TRANSCODE_DISABLED", "transcoding is not enabled")
		return
	}
	segment := chi.URLParam(r, "segment")
	if !torrentstream.IsValidSegmentName(segment) {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_SEGMENT", "invalid segment name")
		return
	}
	path, ok := h.vod.SegmentPath(chi.URLParam(r, "infohash"), segment)
	if !ok {
		handlers.RespondError(w, r, http.StatusNotFound, "NOT_ACTIVE", "no active transcode for this content")
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	http.ServeFile(w, r, path)
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
