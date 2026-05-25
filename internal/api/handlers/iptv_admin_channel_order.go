package handlers

import (
	"encoding/json"
	"net/http"
)

// Admin overlay of the channel list, the counterpart of the
// per-user personalisation surface. The admin panel at
// /admin/libraries/{id} uses these endpoints to reorder + hide
// channels at the library level — hidden HERE is a hard
// constraint, users cannot un-hide via their own overlay.
//
// All routes here are admin-gated by the router (admin role on
// the parent route group). Library access is implicit (admin sees
// everything).
//
// Routes:
//   PUT    /libraries/{id}/channels/order            — replace full ordering
//   PUT    /libraries/{id}/channels/{channelId}/admin-visibility — hide/show
//   DELETE /libraries/{id}/channels/order            — restore M3U defaults

type libraryChannelOrderRequest struct {
	OrderedChannelIDs []string `json:"ordered_channel_ids"`
	HiddenChannelIDs  []string `json:"hidden_channel_ids"`
}

// ReplaceLibraryChannelOrder accepts the full reordered + hidden
// list and persists it in one transaction. Same shape as the
// per-user endpoint so the frontend can reuse its serialisation
// code.
func (h *IPTVHandler) ReplaceLibraryChannelOrder(w http.ResponseWriter, r *http.Request) {
	libraryID := requireParam(w, r, "id")
	if libraryID == "" {
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
	respondData(w, http.StatusOK, map[string]any{"status": "ok"})
}

type libraryChannelVisibilityRequest struct {
	Hidden bool `json:"hidden"`
}

// SetLibraryChannelVisibility flips a single channel's hidden
// state at the admin level. Surgical edit for the eye toggle on
// each row of the curation panel — avoids re-uploading the full
// reordered list when the admin just wants to hide one channel.
func (h *IPTVHandler) SetLibraryChannelVisibility(w http.ResponseWriter, r *http.Request) {
	libraryID := requireParam(w, r, "id")
	if libraryID == "" {
		return
	}
	channelID := requireParam(w, r, "channelId")
	if channelID == "" {
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
	respondData(w, http.StatusOK, map[string]any{"status": "ok"})
}

// ResetLibraryChannelOrder wipes the admin overlay for a library,
// restoring the order + visibility from the M3U import.
func (h *IPTVHandler) ResetLibraryChannelOrder(w http.ResponseWriter, r *http.Request) {
	libraryID := requireParam(w, r, "id")
	if libraryID == "" {
		return
	}
	if err := h.svc.ResetLibraryChannelOrder(r.Context(), libraryID); err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondData(w, http.StatusOK, map[string]any{"status": "ok"})
}

// ListLibraryChannelsAdmin returns every channel for a library with
// the admin overlay applied (position + hidden marker), including
// admin-hidden rows so the curation panel can render the eye-off
// toggle next to them. Distinct from the user-facing ListChannels
// because admins curating need to see what they hid in order to
// un-hide it, but downstream users must NOT (hard constraint).
func (h *IPTVHandler) ListLibraryChannelsAdmin(w http.ResponseWriter, r *http.Request) {
	libraryID := requireParam(w, r, "id")
	if libraryID == "" {
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
	respondData(w, http.StatusOK, result)
}
