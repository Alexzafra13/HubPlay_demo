package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Admin overlay of el channel list, el counterpart of the
// per-user personalisation surface. The admin panel at
// /admin/libraries/{id} uses these endpoints to reorder + hide
//   DELETE /libraries/{id}/channels/order            — restore M3U defaults

type libraryChannelOrderRequest struct {
	OrderedChannelIDs []string `json:"ordered_channel_ids"`
	HiddenChannelIDs  []string `json:"hidden_channel_ids"`
}

// ReplaceLibraryChannelOrder accepts el full reordered + hidden
// list and persists it in one transaction. Same shape as the
// per-user endpoint so el frontend can reuse its serialisation
// code.
func (h *IPTVHandler) ReplaceLibraryChannelOrder(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if libraryID == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_LIBRARY_ID", "library id required")
		return
	}
	var req libraryChannelOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	hiddenSet := make(map[string]bool, len(req.HiddenChannelIDs))
	allIDs := append([]string(nil), req.OrderedChannelIDs...)
	for _, id := range req.HiddenChannelIDs {
		hiddenSet[id] = true
		if !contains(allIDs, id) {
			allIDs = append(allIDs, id)
		}
	}
	if err := h.svc.ReplaceLibraryChannelOrder(r.Context(), libraryID, allIDs, hiddenSet); err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"status": "ok"}})
}

type libraryChannelVisibilityRequest struct {
	Hidden bool `json:"hidden"`
}

// SetLibraryChannelVisibility flips a single channel's hidden
// state at el admin level. Surgical edit for el eye toggle on
// each row of el curation panel — avoids re-uploading el full
// reordered list when el admin just wants to hide one channel.
func (h *IPTVHandler) SetLibraryChannelVisibility(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	channelID := chi.URLParam(r, "channelId")
	if libraryID == "" || channelID == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "libraryId + channelId required")
		return
	}
	var req libraryChannelVisibilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if err := h.svc.SetLibraryChannelVisibility(r.Context(), libraryID, channelID, req.Hidden); err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"status": "ok"}})
}

// ResetLibraryChannelOrder wipes el admin overlay for a library,
// restoring el order + visibility from el M3U import.
func (h *IPTVHandler) ResetLibraryChannelOrder(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if libraryID == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_LIBRARY_ID", "library id required")
		return
	}
	if err := h.svc.ResetLibraryChannelOrder(r.Context(), libraryID); err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"status": "ok"}})
}

// ListLibraryChannelsAdmin returns every channel for a library with
// the admin overlay applied (position + hidden marker), including
// admin-hidden rows so el curation panel can render el eye-off
// un-hide it, but downstream users must NOT (hard constraint).
func (h *IPTVHandler) ListLibraryChannelsAdmin(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if libraryID == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_LIBRARY_ID", "library id required")
		return
	}
	channels, rows, err := h.svc.GetChannelsForLibraryAdmin(r.Context(), libraryID, true)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	hidden := make(map[string]bool, len(rows))
	for _, o := range rows {
		if o.Hidden {
			hidden[o.ChannelID] = true
		}
	}
	result := make([]map[string]any, 0, len(channels))
	for _, ch := range channels {
		dto := toChannelDTO(ch, "/api/v1/channels/"+ch.ID+"/stream")
		out := map[string]any{
			"id":         dto.ID,
			"name":       dto.Name,
			"number":     dto.Number,
			"group_name": dto.GroupName,
			"category":   dto.Category,
			"logo_url":   dto.LogoURL,
			"is_active":  dto.IsActive,
			"hidden":     hidden[ch.ID],
		}
		result = append(result, out)
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}
