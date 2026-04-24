package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/iptv"

	"github.com/go-chi/chi/v5"
)

// IPTVHandler handles IPTV channel and EPG endpoints.
type IPTVHandler struct {
	svc       IPTVService
	proxy     IPTVStreamProxyService
	libraries LibraryRepository
	access    LibraryAccessService
	logger    *slog.Logger
}

// NewIPTVHandler creates a new IPTV handler.
func NewIPTVHandler(svc IPTVService, proxy IPTVStreamProxyService, libraries LibraryRepository, access LibraryAccessService, logger *slog.Logger) *IPTVHandler {
	return &IPTVHandler{
		svc:       svc,
		proxy:     proxy,
		libraries: libraries,
		access:    access,
		logger:    logger.With("module", "iptv-handler"),
	}
}

// canAccessLibrary gates per-library access for the authenticated caller.
// Admins pass unconditionally. Unauthenticated requests fail closed.
// Errors in the ACL lookup fail closed too — the caller sees a generic 404.
func (h *IPTVHandler) canAccessLibrary(r *http.Request, libraryID string) bool {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		return false
	}
	if claims.Role == "admin" {
		return true
	}
	ok, err := h.access.UserHasAccess(r.Context(), claims.UserID, libraryID)
	if err != nil {
		h.logger.Error("library access check failed",
			"user", claims.UserID, "library", libraryID, "error", err)
		return false
	}
	return ok
}

// denyForbidden writes a NOT_FOUND response (not 403) so an unauthorised
// user can't distinguish "channel exists but you can't see it" from
// "channel doesn't exist" — same treatment libraries already give.
func (h *IPTVHandler) denyForbidden(w http.ResponseWriter, r *http.Request) {
	respondAppError(w, r.Context(), domain.NewNotFound("channel"))
}

// ListChannels returns all channels for a library.
func (h *IPTVHandler) ListChannels(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}
	activeOnly := r.URL.Query().Get("active") != "false"

	channels, err := h.svc.GetChannels(r.Context(), libraryID, activeOnly)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	result := make([]channelDTO, 0, len(channels))
	for _, ch := range channels {
		result = append(result, toChannelDTO(ch, "/api/v1/channels/"+ch.ID+"/stream"))
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

// GetChannel returns a single channel.
func (h *IPTVHandler) GetChannel(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}

	// Get now playing
	nowPlaying, _ := h.svc.NowPlaying(r.Context(), channelID)

	// Detail endpoint omits stream_url; clients hit /channels/{id}/stream directly.
	dto := toChannelDTO(ch, "")

	// Use a wrapping map so the now_playing extension lives outside the
	// typed DTO — it's optional per-channel and specific to the detail view.
	resp := map[string]any{
		"id":            dto.ID,
		"name":          dto.Name,
		"number":        dto.Number,
		"group_name":    dto.GroupName,
		"category":      dto.Category,
		"logo_url":      dto.LogoURL,
		"logo_initials": dto.LogoInitials,
		"logo_bg":       dto.LogoBg,
		"logo_fg":       dto.LogoFg,
		"tvg_id":        dto.TvgID,
		"language":      dto.Language,
		"country":       dto.Country,
		"is_active":     dto.IsActive,
	}

	if nowPlaying != nil {
		resp["now_playing"] = map[string]any{
			"title":       nowPlaying.Title,
			"description": nowPlaying.Description,
			"category":    nowPlaying.Category,
			"start_time":  nowPlaying.StartTime,
			"end_time":    nowPlaying.EndTime,
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
}

// Groups returns channel group names for a library.
func (h *IPTVHandler) Groups(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}

	groups, err := h.svc.GetGroups(r.Context(), libraryID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": groups})
}

// Stream proxies a live IPTV stream to the client.
func (h *IPTVHandler) Stream(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}

	if !ch.IsActive {
		respondError(w, r, http.StatusNotFound, "CHANNEL_INACTIVE", "channel is not active")
		return
	}

	if err := h.proxy.ProxyStream(r.Context(), w, channelID, ch.StreamURL); err != nil {
		h.logger.Error("stream proxy error", "channel", channelID, "error", err)
		// Don't write error — response may already be partially written
	}
}

// ProxyURL proxies an HLS segment or sub-playlist for a channel.
func (h *IPTVHandler) ProxyURL(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_URL", "url parameter required")
		return
	}

	// Authorisation: resolve the channel's library and check access. The
	// proxy-itself validates the upstream URL against SSRF, but we must
	// still confirm the caller owns the channel they're proxying through.
	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}

	if err := h.proxy.ProxyURL(r.Context(), w, channelID, rawURL); err != nil {
		h.logger.Error("proxy URL error", "channel", channelID, "error", err)
	}
}

// Schedule returns EPG schedule for a channel.
func (h *IPTVHandler) Schedule(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}

	from, to := parseTimeRange(r)

	programs, err := h.svc.GetSchedule(r.Context(), channelID, from, to)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	result := make([]map[string]any, 0, len(programs))
	for _, p := range programs {
		result = append(result, programToJSON(p))
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

// bulkScheduleMaxChannels caps how many channels a single request may
// ask about. Keeps a single bad/mistaken POST from pinning a SQLite
// connection while we iterate thousands of IN() chunks. The cap is
// generous enough that a whole country's EPG fits in one round-trip.
const bulkScheduleMaxChannels = 5000

// bulkScheduleRequest is the POST body for the bulk schedule endpoint.
// Times use the same "hours relative to now" convention as the GET
// variant (handled by parseBulkTimeRange) so both shapes are
// interchangeable.
type bulkScheduleRequest struct {
	Channels []string `json:"channels"`
	From     string   `json:"from,omitempty"`
	To       string   `json:"to,omitempty"`
}

// BulkSchedule returns EPG for multiple channels at once.
//
// Accepts both GET (?channels=a,b,c) and POST (JSON body). POST is the
// preferred transport: a library with ~250 channels already produces a
// query string big enough to trip a 414 at common nginx defaults, so
// the React client always POSTs. GET stays supported for curl/ad-hoc
// debugging on small libraries.
//
// Each channel is filtered individually through the ACL — inaccessible
// channels are dropped silently (no error) so a single restricted channel
// doesn't poison a bulk call for an otherwise-authorised user.
func (h *IPTVHandler) BulkSchedule(w http.ResponseWriter, r *http.Request) {
	channelIDs, from, to, ok := h.parseBulkScheduleRequest(w, r)
	if !ok {
		return
	}

	allowed := make([]string, 0, len(channelIDs))
	for _, id := range channelIDs {
		if id == "" {
			continue
		}
		ch, err := h.svc.GetChannel(r.Context(), id)
		if err != nil {
			continue // unknown channel — skip rather than bubble a 500
		}
		if h.canAccessLibrary(r, ch.LibraryID) {
			allowed = append(allowed, id)
		}
	}
	if len(allowed) == 0 {
		respondJSON(w, http.StatusOK, map[string]any{"data": map[string]any{}})
		return
	}

	schedules, err := h.svc.GetBulkSchedule(r.Context(), allowed, from, to)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	result := make(map[string]any)
	for chID, programs := range schedules {
		progs := make([]map[string]any, 0, len(programs))
		for _, p := range programs {
			progs = append(progs, programToJSON(p))
		}
		result[chID] = progs
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

// parseBulkScheduleRequest normalises the two transports (GET query,
// POST JSON body) into a single (channelIDs, from, to) tuple. On error
// it writes the response and returns ok=false; the caller must bail.
func (h *IPTVHandler) parseBulkScheduleRequest(w http.ResponseWriter, r *http.Request) (ids []string, from, to time.Time, ok bool) {
	if r.Method == http.MethodPost {
		// Cap the body at 1 MiB — more than enough for 5k channel UUIDs
		// but small enough to stop a malicious client from streaming a
		// gigabyte into the JSON decoder.
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		defer r.Body.Close() //nolint:errcheck

		var body bulkScheduleRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
			return nil, time.Time{}, time.Time{}, false
		}
		if len(body.Channels) == 0 {
			respondError(w, r, http.StatusBadRequest, "MISSING_CHANNELS", "channels field required")
			return nil, time.Time{}, time.Time{}, false
		}
		if len(body.Channels) > bulkScheduleMaxChannels {
			respondError(w, r, http.StatusBadRequest, "TOO_MANY_CHANNELS",
				fmt.Sprintf("at most %d channels per request", bulkScheduleMaxChannels))
			return nil, time.Time{}, time.Time{}, false
		}
		from, to = parseBulkTimeRange(body.From, body.To)
		return body.Channels, from, to, true
	}

	// GET fallback — kept for curl / small-list back-compat.
	raw := r.URL.Query().Get("channels")
	if raw == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_CHANNELS", "channels parameter required")
		return nil, time.Time{}, time.Time{}, false
	}
	ids = strings.Split(raw, ",")
	if len(ids) > bulkScheduleMaxChannels {
		respondError(w, r, http.StatusBadRequest, "TOO_MANY_CHANNELS",
			fmt.Sprintf("at most %d channels per request", bulkScheduleMaxChannels))
		return nil, time.Time{}, time.Time{}, false
	}
	from, to = parseTimeRange(r)
	return ids, from, to, true
}

// RefreshM3U triggers an M3U playlist refresh for a library.
//
// Already admin-only at the route level, but we also verify library access
// defence-in-depth: admins can see every library regardless of the ACL, so
// this check is effectively a documentation anchor today. It becomes
// load-bearing the day a non-admin role gains access to refresh endpoints.
func (h *IPTVHandler) RefreshM3U(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}

	count, err := h.svc.RefreshM3U(r.Context(), libraryID)
	if err != nil {
		// Log the raw error for operators; handleServiceError renders a safe
		// typed AppError (or a generic 500) without leaking upstream messages.
		h.logger.Error("M3U refresh failed", "library", libraryID, "error", err)
		handleServiceError(w, r, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"channels_imported": count,
		},
	})
}

// RefreshEPG triggers an EPG refresh for a library.
func (h *IPTVHandler) RefreshEPG(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}

	count, err := h.svc.RefreshEPG(r.Context(), libraryID)
	if err != nil {
		h.logger.Error("EPG refresh failed", "library", libraryID, "error", err)
		handleServiceError(w, r, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"programs_imported": count,
		},
	})
}

// ───────────────────────────────────────────────────────────────────
// Channel favorites
// ───────────────────────────────────────────────────────────────────
//
// Routes (registered in router.go):
//   GET    /api/v1/favorites/channels         — list (full channel rows)
//   GET    /api/v1/favorites/channels/ids     — list (just channel IDs)
//   PUT    /api/v1/favorites/channels/{channelId}  — add
//   DELETE /api/v1/favorites/channels/{channelId}  — remove
//
// Authorization: user is derived from JWT claims. Add/Remove additionally
// verify the caller can access the channel's library (same ACL gate as
// `canAccessLibrary` — consistent with the rest of the IPTV surface).

// ListFavorites returns the caller's favorite channels as full channel DTOs.
func (h *IPTVHandler) ListFavorites(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondAppError(w, r.Context(), domain.NewUnauthorized("auth required"))
		return
	}
	channels, err := h.svc.ListFavoriteChannels(r.Context(), claims.UserID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	result := make([]channelDTO, 0, len(channels))
	for _, ch := range channels {
		result = append(result, toChannelDTO(ch, "/api/v1/channels/"+ch.ID+"/stream"))
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

// ListFavoriteIDs returns just the IDs — lighter payload used on page load
// to hydrate the frontend's favorite set without re-shipping channel data
// the client already has from ListChannels.
func (h *IPTVHandler) ListFavoriteIDs(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondAppError(w, r.Context(), domain.NewUnauthorized("auth required"))
		return
	}
	ids, err := h.svc.ListFavoriteIDs(r.Context(), claims.UserID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": ids})
}

// AddFavorite marks a channel favorited by the caller. Idempotent.
func (h *IPTVHandler) AddFavorite(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondAppError(w, r.Context(), domain.NewUnauthorized("auth required"))
		return
	}
	channelID := chi.URLParam(r, "channelId")

	// Look up the channel so we can verify the caller can access its library.
	// Favoriting a channel from a library the user can't see would leak the
	// existence of that library.
	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}

	if err := h.svc.AddFavorite(r.Context(), claims.UserID, channelID); err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"channel_id": channelID, "is_favorite": true},
	})
}

// RemoveFavorite unmarks a channel. Idempotent — returns 200 even if the
// channel wasn't favorited.
func (h *IPTVHandler) RemoveFavorite(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondAppError(w, r.Context(), domain.NewUnauthorized("auth required"))
		return
	}
	channelID := chi.URLParam(r, "channelId")

	// ACL gate by channel's library. If the channel no longer exists (e.g.
	// removed during an M3U refresh after it was favorited), skip the ACL
	// check and still allow removal — the row is about to be cascaded out
	// anyway, and failing here would leave stale rows in the table.
	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err == nil {
		if !h.canAccessLibrary(r, ch.LibraryID) {
			h.denyForbidden(w, r)
			return
		}
	}

	if err := h.svc.RemoveFavorite(r.Context(), claims.UserID, channelID); err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"channel_id": channelID, "is_favorite": false},
	})
}

func parseTimeRange(r *http.Request) (time.Time, time.Time) {
	return parseBulkTimeRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
}

// parseBulkTimeRange resolves the optional `from`/`to` params shared by
// the GET and POST variants of the schedule endpoints. Accepts either
// RFC3339 timestamps ("2026-04-24T12:00:00Z") or bare integers
// interpreted as hours (`from=6` → 6h ago, `to=12` → 12h from now).
// Empty values fall back to the default ±window.
func parseBulkTimeRange(fromRaw, toRaw string) (time.Time, time.Time) {
	now := time.Now()
	from := now.Add(-2 * time.Hour) // default: 2h ago
	to := now.Add(24 * time.Hour)   // default: 24h from now

	if fromRaw != "" {
		if t, err := time.Parse(time.RFC3339, fromRaw); err == nil {
			from = t
		} else if hours, err := strconv.Atoi(fromRaw); err == nil {
			from = now.Add(-time.Duration(hours) * time.Hour)
		}
	}
	if toRaw != "" {
		if t, err := time.Parse(time.RFC3339, toRaw); err == nil {
			to = t
		} else if hours, err := strconv.Atoi(toRaw); err == nil {
			to = now.Add(time.Duration(hours) * time.Hour)
		}
	}

	return from, to
}

func programToJSON(p *db.EPGProgram) map[string]any {
	return map[string]any{
		"id":          p.ID,
		"title":       p.Title,
		"description": p.Description,
		"category":    p.Category,
		"icon_url":    p.IconURL,
		"start_time":  p.StartTime,
		"end_time":    p.EndTime,
	}
}

// PublicCountries returns the list of countries with available public IPTV channels.
func (h *IPTVHandler) PublicCountries(w http.ResponseWriter, r *http.Request) {
	countries := iptv.PublicCountries()

	result := make([]map[string]any, 0, len(countries))
	for _, c := range countries {
		result = append(result, map[string]any{
			"code": c.Code,
			"name": c.Name,
			"flag": c.Flag,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

// ImportPublicIPTV creates a livetv library for a country and triggers M3U import.
func (h *IPTVHandler) ImportPublicIPTV(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Country string `json:"country"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	country, ok := iptv.FindCountry(req.Country)
	if !ok {
		respondError(w, r, http.StatusBadRequest, "INVALID_COUNTRY", "unknown country code")
		return
	}

	libraryName := req.Name
	if libraryName == "" {
		libraryName = fmt.Sprintf("Live TV - %s", country.Name)
	}

	now := time.Now()
	lib := &db.Library{
		ID:          generateLibraryID(),
		Name:        libraryName,
		ContentType: "livetv",
		M3UURL:      country.M3UURL(),
		ScanMode:    "auto",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := h.libraries.Create(r.Context(), lib); err != nil {
		h.logger.Error("create public IPTV library", "error", err)
		respondError(w, r, http.StatusInternalServerError, "CREATE_ERROR", "failed to create library")
		return
	}

	// Trigger M3U refresh in background (use detached context)
	libID := lib.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		count, err := h.svc.RefreshM3U(ctx, libID)
		if err != nil {
			h.logger.Error("public IPTV M3U refresh failed", "library", libID, "error", err)
			return
		}
		h.logger.Info("public IPTV imported", "library", libID, "country", req.Country, "channels", count)
	}()

	respondJSON(w, http.StatusCreated, map[string]any{
		"data": map[string]any{
			"library_id": lib.ID,
			"name":       lib.Name,
			"country":    req.Country,
			"m3u_url":    lib.M3UURL,
		},
	})
}

func generateLibraryID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ── EPG source management endpoints ──────────────────────────────
//
// GET  /api/v1/iptv/epg-catalog                          (auth user)
// GET  /api/v1/libraries/{id}/epg-sources                (auth + ACL)
// POST /api/v1/libraries/{id}/epg-sources                (admin)
// DELETE /api/v1/libraries/{id}/epg-sources/{sourceId}   (admin)
// PATCH  /api/v1/libraries/{id}/epg-sources/reorder      (admin)
//
// The catalog endpoint is intentionally viewer-accessible: the shape
// is public data (provider names + URLs) and exposing it to the
// frontend keeps the admin dropdown code identical across roles.

// EPGCatalog returns the curated EPG provider list.
func (h *IPTVHandler) EPGCatalog(w http.ResponseWriter, r *http.Request) {
	catalog := h.svc.PublicEPGCatalog()
	out := make([]map[string]any, 0, len(catalog))
	for _, src := range catalog {
		out = append(out, map[string]any{
			"id":          src.ID,
			"name":        src.Name,
			"description": src.Description,
			"language":    src.Language,
			"countries":   src.Countries,
			"url":         src.URL,
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// ListEPGSources returns the EPG providers attached to a library.
// Gated by the library ACL — the EPG source list leaks URL info we'd
// rather keep library-private.
func (h *IPTVHandler) ListEPGSources(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}
	sources, err := h.svc.ListEPGSources(r.Context(), libraryID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": epgSourcesToJSON(sources)})
}

type addEPGSourceRequest struct {
	CatalogID string `json:"catalog_id"`
	URL       string `json:"url"`
}

// AddEPGSource attaches a new provider. Admin-only at the route level.
func (h *IPTVHandler) AddEPGSource(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}
	var body addEPGSourceRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}
	src, err := h.svc.AddEPGSource(r.Context(), libraryID, body.CatalogID, body.URL)
	if err != nil {
		// Duplicate URL is the expected failure mode when the admin
		// re-adds a source (or the catalog entry for a URL they'd
		// already pasted custom). Map to 409 + clean message so the
		// UI can render "ya añadida" instead of a raw SQL error.
		if errors.Is(err, db.ErrEPGSourceAlreadyAttached) {
			respondError(w, r, http.StatusConflict, "ALREADY_ATTACHED",
				"esa fuente EPG ya está añadida a esta biblioteca")
			return
		}
		// Other errors from AddEPGSource are shape problems (unknown
		// catalog id, missing fields) — surface them as 400.
		if _, ok := err.(interface{ Kind() string }); !ok {
			respondError(w, r, http.StatusBadRequest, "INVALID_SOURCE", err.Error())
			return
		}
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"data": epgSourceToJSON(src)})
}

// RemoveEPGSource deletes one provider from the library.
func (h *IPTVHandler) RemoveEPGSource(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	sourceID := chi.URLParam(r, "sourceId")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}
	if err := h.svc.RemoveEPGSource(r.Context(), libraryID, sourceID); err != nil {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type reorderEPGSourcesRequest struct {
	SourceIDs []string `json:"source_ids"`
}

// ReorderEPGSources rewrites every source's priority. Body is the
// full ordered id list.
func (h *IPTVHandler) ReorderEPGSources(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}
	var body reorderEPGSourcesRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}
	if err := h.svc.ReorderEPGSources(r.Context(), libraryID, body.SourceIDs); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_ORDER", err.Error())
		return
	}
	sources, err := h.svc.ListEPGSources(r.Context(), libraryID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": epgSourcesToJSON(sources)})
}

// ── Channels without EPG + manual edit ───────────────────────────
//
// GET  /api/v1/libraries/{id}/channels/without-epg   (auth + ACL)
// PATCH /api/v1/channels/{channelId}                  (admin)
//
// The list endpoint flags channels that no XMLTV source matched for
// the next ~24h. The PATCH lets the admin fix the mismatch by
// correcting tvg_id by hand; the override persists across M3U
// refreshes via the channel_overrides table.

// ListChannelsWithoutEPG returns active channels with no programmes
// in the default guide window.
func (h *IPTVHandler) ListChannelsWithoutEPG(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}
	channels, err := h.svc.ListChannelsWithoutEPG(r.Context(), libraryID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	out := make([]map[string]any, 0, len(channels))
	for _, ch := range channels {
		out = append(out, channelWithoutEPGDTO(ch))
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

type patchChannelRequest struct {
	TvgID *string `json:"tvg_id,omitempty"` // pointer so missing != empty
}

// PatchChannel accepts admin edits to a single channel. Currently
// only `tvg_id` is mutable — other fields are derived from the M3U
// and would be wiped on the next refresh anyway.
//
// A nil TvgID means "field not present in request" (leave alone);
// an explicit "" means "clear tvg_id AND the persistent override".
func (h *IPTVHandler) PatchChannel(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")
	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}

	var body patchChannelRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}

	if body.TvgID != nil {
		if err := h.svc.SetChannelTvgID(r.Context(), channelID, strings.TrimSpace(*body.TvgID)); err != nil {
			handleServiceError(w, r, err)
			return
		}
	}

	// Return the post-edit channel so the UI can skip a follow-up GET.
	updated, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": channelWithoutEPGDTO(updated)})
}

// channelWithoutEPGDTO shapes the row so the UI gets the minimum
// needed to render "canal sin guía": identity + current tvg_id +
// the display-name variants that might help the admin pick the
// right override value.
func channelWithoutEPGDTO(ch *db.Channel) map[string]any {
	return map[string]any{
		"id":         ch.ID,
		"library_id": ch.LibraryID,
		"name":       ch.Name,
		"number":     ch.Number,
		"group_name": ch.GroupName,
		"logo_url":   ch.LogoURL,
		"tvg_id":     ch.TvgID,
		"is_active":  ch.IsActive,
	}
}

// ── Channel health endpoints ─────────────────────────────────────
//
// GET /api/v1/libraries/{id}/channels/unhealthy   (auth + ACL)
// POST /api/v1/channels/{channelId}/reset-health  (admin)
// POST /api/v1/channels/{channelId}/disable       (admin)
//
// Read path is gated by the same per-library ACL as the channel list.
// Write paths are admin-only at the route level (router.go).

// ListUnhealthyChannels returns channels whose probe-failure count is
// above the threshold. Optional `?threshold=N` query param; default
// is the repo constant.
func (h *IPTVHandler) ListUnhealthyChannels(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}
	threshold := 0 // 0 = let repo pick its default
	if v := r.URL.Query().Get("threshold"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			threshold = n
		}
	}
	channels, err := h.svc.ListUnhealthyChannels(r.Context(), libraryID, threshold)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	out := make([]map[string]any, 0, len(channels))
	for _, ch := range channels {
		out = append(out, channelHealthDTO(ch))
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// ResetChannelHealth clears the failure counter so the channel is
// visible again in the user list. Doesn't probe — the operator is
// asserting the channel works.
func (h *IPTVHandler) ResetChannelHealth(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")
	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}
	if err := h.svc.ResetChannelHealth(r.Context(), channelID); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DisableChannel permanently hides a channel from the user list by
// flipping is_active. Idempotent.
func (h *IPTVHandler) DisableChannel(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")
	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}
	if err := h.svc.SetChannelActive(r.Context(), channelID, false); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// EnableChannel is the mirror image — lets the admin re-enable a
// channel that was manually disabled.
func (h *IPTVHandler) EnableChannel(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")
	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}
	if err := h.svc.SetChannelActive(r.Context(), channelID, true); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// channelHealthDTO shapes a channel with its health fields for the
// admin UI. The regular channel DTO (iptv_dto.go) stays lean and
// omits these columns to avoid payload bloat on the hot list path.
func channelHealthDTO(ch *db.Channel) map[string]any {
	var lastProbe any
	if !ch.LastProbeAt.IsZero() {
		lastProbe = ch.LastProbeAt
	}
	return map[string]any{
		"id":                   ch.ID,
		"library_id":           ch.LibraryID,
		"name":                 ch.Name,
		"number":               ch.Number,
		"group_name":           ch.GroupName,
		"logo_url":             ch.LogoURL,
		"tvg_id":               ch.TvgID,
		"is_active":            ch.IsActive,
		"last_probe_at":        lastProbe,
		"last_probe_status":    ch.LastProbeStatus,
		"last_probe_error":     ch.LastProbeError,
		"consecutive_failures": ch.ConsecutiveFailures,
	}
}

func epgSourcesToJSON(sources []*db.LibraryEPGSource) []map[string]any {
	out := make([]map[string]any, 0, len(sources))
	for _, s := range sources {
		out = append(out, epgSourceToJSON(s))
	}
	return out
}

func epgSourceToJSON(s *db.LibraryEPGSource) map[string]any {
	var lastRefreshed any
	if !s.LastRefreshedAt.IsZero() {
		lastRefreshed = s.LastRefreshedAt
	}
	return map[string]any{
		"id":                 s.ID,
		"library_id":         s.LibraryID,
		"catalog_id":         s.CatalogID,
		"url":                s.URL,
		"priority":           s.Priority,
		"last_refreshed_at":  lastRefreshed,
		"last_status":        s.LastStatus,
		"last_error":         s.LastError,
		"last_program_count": s.LastProgramCount,
		"last_channel_count": s.LastChannelCount,
		"created_at":         s.CreatedAt,
	}
}
