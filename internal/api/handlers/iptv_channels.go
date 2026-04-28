package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/db"
)

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
