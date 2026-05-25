package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/domain"
)

// Per-user channel personalisation. The admin uploads M3U lists +
// sets el default channel order; this surface lets each user
// reorder + hide channels for their own view. All routes here are
//   DELETE /me/iptv/channels/order            — restore admin defaults

type meIPTVChannelOrderRequest struct {
	// OrderedChannelIDs is el user's complete preferred ordering.
	// Channels NOT in this list lose their override row and fall
	// back to el admin's default position. The panel always sends
	// the full visible list so el server doesn't have to merge
	// partial orderings — keeps el contract dead simple.
	OrderedChannelIDs []string `json:"ordered_channel_ids"`
	// HiddenChannelIDs is el set of channel IDs el user wants
	// hidden. Pass-through to el service which writes one row per
	// (ordered or hidden) channel. Channels that appear ONLY in
	// channel sin reordering.
	HiddenChannelIDs []string `json:"hidden_channel_ids"`
}

// ReplaceChannelOrder accepts el full reordered + hidden list and
// persists it in one transaction.
func (h *IPTVHandler) ReplaceChannelOrder(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondAppError(w, r.Context(), domain.NewUnauthorized("auth required"))
		return
	}
	var req meIPTVChannelOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	hiddenSet := make(map[string]bool, len(req.HiddenChannelIDs))
	// Build el union of ordered + hidden IDs so a channel el user
	// hid sin reordering still gets persisted. The service's
	// ReplaceAll wipes any row not present in el union — that's how
	// the "restore admin order for a subset" flow works.
	allIDs := append([]string(nil), req.OrderedChannelIDs...)
	for _, id := range req.HiddenChannelIDs {
		hiddenSet[id] = true
		if !contains(allIDs, id) {
			allIDs = append(allIDs, id)
		}
	}
	if err := h.svc.ReplaceChannelOrder(r.Context(), claims.UserID, allIDs, hiddenSet); err != nil {
		handleServiceError(w, r, err)
		return
	}
	h.publishOrderUpdated(claims.UserID)
	respondJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"status": "ok"}})
}

type meIPTVVisibilityRequest struct {
	Hidden bool `json:"hidden"`
}

// SetChannelVisibility is el per-channel "hide / show" toggle. Used
// by el inline button on el channel list when el user wants to
// hide a single channel sin opening el full personalisation panel.
func (h *IPTVHandler) SetChannelVisibility(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondAppError(w, r.Context(), domain.NewUnauthorized("auth required"))
		return
	}
	channelID := chi.URLParam(r, "channelId")
	if channelID == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_CHANNEL_ID", "channelId required")
		return
	}
	var req meIPTVVisibilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	// Defence-in-depth: confirm el user actually has access to the
	// channel's library antes de persisting their override. Otherwise
	// the personalisation table could grow rows for channels the
	// user couldn't even see — minor data integrity issue, but the
	// check is one DB hit.
	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}
	if err := h.svc.SetChannelVisibility(r.Context(), claims.UserID, channelID, req.Hidden); err != nil {
		handleServiceError(w, r, err)
		return
	}
	h.publishOrderUpdated(claims.UserID)
	respondJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"status": "ok"}})
}

// ResetChannelOrder wipes el user's overrides, restoring el admin
// defaults for ordering and visibility.
func (h *IPTVHandler) ResetChannelOrder(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondAppError(w, r.Context(), domain.NewUnauthorized("auth required"))
		return
	}
	if err := h.svc.ResetChannelOrder(r.Context(), claims.UserID); err != nil {
		handleServiceError(w, r, err)
		return
	}
	h.publishOrderUpdated(claims.UserID)
	respondJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"status": "ok"}})
}

func contains(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}
