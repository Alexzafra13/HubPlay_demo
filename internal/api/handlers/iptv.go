package handlers

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/iptv"

	"github.com/go-chi/chi/v5"
)

// IPTVHandler handles IPTV channel and EPG endpoints.
type IPTVHandler struct {
	svc    *iptv.Service
	proxy  *iptv.StreamProxy
	logger *slog.Logger
}

// NewIPTVHandler creates a new IPTV handler.
func NewIPTVHandler(svc *iptv.Service, proxy *iptv.StreamProxy, logger *slog.Logger) *IPTVHandler {
	return &IPTVHandler{
		svc:    svc,
		proxy:  proxy,
		logger: logger.With("module", "iptv-handler"),
	}
}

// ListChannels returns all channels for a library.
func (h *IPTVHandler) ListChannels(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	activeOnly := r.URL.Query().Get("active") != "false"

	channels, err := h.svc.GetChannels(r.Context(), libraryID, activeOnly)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	result := make([]map[string]any, 0, len(channels))
	for _, ch := range channels {
		result = append(result, map[string]any{
			"id":         ch.ID,
			"name":       ch.Name,
			"number":     ch.Number,
			"group_name": ch.GroupName,
			"logo_url":   ch.LogoURL,
			"tvg_id":     ch.TvgID,
			"language":   ch.Language,
			"country":    ch.Country,
			"is_active":  ch.IsActive,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

// GetChannel returns a single channel.
func (h *IPTVHandler) GetChannel(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	// Get now playing
	nowPlaying, _ := h.svc.NowPlaying(r.Context(), channelID)

	resp := map[string]any{
		"id":         ch.ID,
		"name":       ch.Name,
		"number":     ch.Number,
		"group_name": ch.GroupName,
		"logo_url":   ch.LogoURL,
		"tvg_id":     ch.TvgID,
		"language":   ch.Language,
		"country":    ch.Country,
		"is_active":  ch.IsActive,
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

	groups, err := h.svc.GetGroups(r.Context(), libraryID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": groups})
}

// Stream proxies a live IPTV stream to the client.
func (h *IPTVHandler) Stream(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	if !ch.IsActive {
		respondError(w, http.StatusNotFound, "CHANNEL_INACTIVE", "channel is not active")
		return
	}

	if err := h.proxy.ProxyStream(r.Context(), w, channelID, ch.StreamURL); err != nil {
		h.logger.Error("stream proxy error", "channel", channelID, "error", err)
		// Don't write error — response may already be partially written
	}
}

// Schedule returns EPG schedule for a channel.
func (h *IPTVHandler) Schedule(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")

	from, to := parseTimeRange(r)

	programs, err := h.svc.GetSchedule(r.Context(), channelID, from, to)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	result := make([]map[string]any, 0, len(programs))
	for _, p := range programs {
		result = append(result, programToJSON(p))
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

// BulkSchedule returns EPG for multiple channels at once.
func (h *IPTVHandler) BulkSchedule(w http.ResponseWriter, r *http.Request) {
	channelIDs := strings.Split(r.URL.Query().Get("channels"), ",")
	if len(channelIDs) == 0 || (len(channelIDs) == 1 && channelIDs[0] == "") {
		respondError(w, http.StatusBadRequest, "MISSING_CHANNELS", "channels parameter required")
		return
	}

	from, to := parseTimeRange(r)

	schedules, err := h.svc.GetBulkSchedule(r.Context(), channelIDs, from, to)
	if err != nil {
		handleServiceError(w, err)
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

// RefreshM3U triggers an M3U playlist refresh for a library.
func (h *IPTVHandler) RefreshM3U(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")

	count, err := h.svc.RefreshM3U(r.Context(), libraryID)
	if err != nil {
		h.logger.Error("M3U refresh failed", "library", libraryID, "error", err)
		respondError(w, http.StatusInternalServerError, "REFRESH_ERROR", err.Error())
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

	count, err := h.svc.RefreshEPG(r.Context(), libraryID)
	if err != nil {
		h.logger.Error("EPG refresh failed", "library", libraryID, "error", err)
		respondError(w, http.StatusInternalServerError, "REFRESH_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"programs_imported": count,
		},
	})
}

func parseTimeRange(r *http.Request) (time.Time, time.Time) {
	now := time.Now()
	from := now.Add(-2 * time.Hour) // default: 2h ago
	to := now.Add(24 * time.Hour)   // default: 24h from now

	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t
		} else if hours, err := strconv.Atoi(v); err == nil {
			from = now.Add(-time.Duration(hours) * time.Hour)
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t
		} else if hours, err := strconv.Atoi(v); err == nil {
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
