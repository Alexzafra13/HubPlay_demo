package iptvhandler

import (
	"context"
	"log/slog"
	"net/http"

	"hubplay/internal/api/handlers"
	iptvmodel "hubplay/internal/iptv/model"
)

// adminChannelOrderManager es el contrato mínimo para la curación
// admin de orden de canales. 4 de ~50 métodos.
type adminChannelOrderManager interface {
	GetChannelsForLibraryAdmin(ctx context.Context, libraryID string, includeHidden bool) ([]*iptvmodel.Channel, []iptvmodel.LibraryChannelOrderEntry, error)
	ReplaceLibraryChannelOrder(ctx context.Context, libraryID string, orderedIDs []string, hiddenIDs map[string]bool) error
	SetLibraryChannelVisibility(ctx context.Context, libraryID, channelID string, hidden bool) error
	ResetLibraryChannelOrder(ctx context.Context, libraryID string) error
}

type iptvAdminOrderHandler struct {
	svc    adminChannelOrderManager
	logger *slog.Logger
}

type libraryChannelOrderRequest struct {
	OrderedChannelIDs []string `json:"ordered_channel_ids"`
	HiddenChannelIDs  []string `json:"hidden_channel_ids"`
}

// ReplaceLibraryChannelOrder accepts the full reordered + hidden
// list and persists it in one transaction. Same shape as the
// per-user endpoint so the frontend can reuse its serialisation
// code.
func (h *iptvAdminOrderHandler) ReplaceLibraryChannelOrder(w http.ResponseWriter, r *http.Request) {
	libraryID := handlers.RequireParam(w, r, "id")
	if libraryID == "" {
		return
	}
	var req libraryChannelOrderRequest
	if err := handlers.DecodeJSON(w, r, &req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
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
		handlers.HandleServiceError(w, r, err)
		return
	}
	handlers.RespondData(w, http.StatusOK, map[string]any{"status": "ok"})
}

type libraryChannelVisibilityRequest struct {
	Hidden bool `json:"hidden"`
}

// SetLibraryChannelVisibility flips a single channel's hidden
// state at the admin level. Surgical edit for the eye toggle on
// each row of the curation panel — avoids re-uploading the full
// reordered list when the admin just wants to hide one channel.
func (h *iptvAdminOrderHandler) SetLibraryChannelVisibility(w http.ResponseWriter, r *http.Request) {
	libraryID := handlers.RequireParam(w, r, "id")
	if libraryID == "" {
		return
	}
	channelID := handlers.RequireParam(w, r, "channelId")
	if channelID == "" {
		return
	}
	var req libraryChannelVisibilityRequest
	if err := handlers.DecodeJSON(w, r, &req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if err := h.svc.SetLibraryChannelVisibility(r.Context(), libraryID, channelID, req.Hidden); err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	handlers.RespondData(w, http.StatusOK, map[string]any{"status": "ok"})
}

// ResetLibraryChannelOrder wipes the admin overlay for a library,
// restoring the order + visibility from the M3U import.
func (h *iptvAdminOrderHandler) ResetLibraryChannelOrder(w http.ResponseWriter, r *http.Request) {
	libraryID := handlers.RequireParam(w, r, "id")
	if libraryID == "" {
		return
	}
	if err := h.svc.ResetLibraryChannelOrder(r.Context(), libraryID); err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	handlers.RespondData(w, http.StatusOK, map[string]any{"status": "ok"})
}

// ListLibraryChannelsAdmin returns every channel for a library with
// the admin overlay applied (position + hidden marker), including
// admin-hidden rows so the curation panel can render the eye-off
// toggle next to them. Distinct from the user-facing ListChannels
// because admins curating need to see what they hid in order to
// un-hide it, but downstream users must NOT (hard constraint).
func (h *iptvAdminOrderHandler) ListLibraryChannelsAdmin(w http.ResponseWriter, r *http.Request) {
	libraryID := handlers.RequireParam(w, r, "id")
	if libraryID == "" {
		return
	}
	channels, rows, err := h.svc.GetChannelsForLibraryAdmin(r.Context(), libraryID, true)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
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
	handlers.RespondData(w, http.StatusOK, result)
}
