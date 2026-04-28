// Channel health + manual EPG override endpoints.
//
//   GET   /api/v1/libraries/{id}/channels/without-epg   (auth + ACL)
//   PATCH /api/v1/channels/{channelId}                  (admin)
//   GET   /api/v1/libraries/{id}/channels/unhealthy     (auth + ACL)
//   POST  /api/v1/channels/{channelId}/reset-health     (admin)
//   POST  /api/v1/channels/{channelId}/disable          (admin)
//   POST  /api/v1/channels/{channelId}/enable           (admin)
//
// Read paths are gated by the same per-library ACL as the channel
// list. Write paths are admin-only at the route level (router.go).

package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/db"
)

// ── Channels without EPG ─────────────────────────────────────────
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

// channelHealthDTO shapes a channel with its health fields. Built on
// top of the regular `toChannelDTO` so the frontend can feed these
// rows into the same `ChannelCard` component as normal channels with
// just a `dimmed` flag flipped on — no parallel shape, no copy-paste
// of derivation logic.
//
// The stream_url is still populated: the "Apagados" rail in Discover
// lets viewers try the channel anyway (a click doesn't commit to a
// belief the channel is dead; the proxy records another failure if
// it still is, and resets the counter on first success).
func channelHealthDTO(ch *db.Channel) map[string]any {
	base := toChannelDTO(ch, "/api/v1/channels/"+ch.ID+"/stream")
	var lastProbe any
	if !ch.LastProbeAt.IsZero() {
		lastProbe = ch.LastProbeAt
	}
	return map[string]any{
		"id":                   base.ID,
		"name":                 base.Name,
		"number":               base.Number,
		"group":                base.Group,
		"group_name":           base.GroupName,
		"category":             base.Category,
		"logo_url":             base.LogoURL,
		"logo_initials":        base.LogoInitials,
		"logo_bg":              base.LogoBg,
		"logo_fg":              base.LogoFg,
		"stream_url":           base.StreamURL,
		"library_id":           base.LibraryID,
		"tvg_id":               base.TvgID,
		"language":             base.Language,
		"country":              base.Country,
		"is_active":            base.IsActive,
		"added_at":             base.AddedAt,
		"last_probe_at":        lastProbe,
		"last_probe_status":    ch.LastProbeStatus,
		"last_probe_error":     ch.LastProbeError,
		"consecutive_failures": ch.ConsecutiveFailures,
	}
}
