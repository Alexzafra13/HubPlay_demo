package iptvhandler

import (
	"context"
	"log/slog"
	"net/http"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	"hubplay/internal/domain"
	"hubplay/internal/event"
	iptvmodel "hubplay/internal/iptv/model"
)

// Per-user channel personalisation. The admin uploads M3U lists +
// sets the default channel order; this surface lets each user
// reorder + hide channels for their own view. All routes here are
// /me/iptv/* — user-owned, no admin gate, the caller is always
// derived from JWT claims.
//
// Routes:
//   PUT    /me/iptv/channels/order            — replace full ordering
//   PUT    /me/iptv/channels/{channelId}/visibility — toggle hidden
//   DELETE /me/iptv/channels/order            — restore admin defaults

// channelPersonaliser es el contrato mínimo para la personalización
// per-user de canales. 4 de ~50 métodos de IPTVService.
type channelPersonaliser interface {
	ReplaceChannelOrder(ctx context.Context, userID string, orderedIDs []string, hiddenIDs map[string]bool) error
	SetChannelVisibility(ctx context.Context, userID, channelID string, hidden bool) error
	ResetChannelOrder(ctx context.Context, userID string) error
	GetChannel(ctx context.Context, id string) (*iptvmodel.Channel, error)
}

type iptvPersonalisationHandler struct {
	svc    channelPersonaliser
	access handlers.LibraryAccessService
	bus    handlers.EventBusPublisher
	logger *slog.Logger
}

func (h *iptvPersonalisationHandler) publishOrderUpdated(userID string) {
	if h.bus == nil {
		return
	}
	h.bus.Publish(event.Event{
		Type: event.ChannelOrderUpdated,
		Data: map[string]any{"user_id": userID},
	})
}

type meIPTVChannelOrderRequest struct {
	// OrderedChannelIDs is the user's complete preferred ordering.
	// Channels NOT in this list lose their override row and fall
	// back to the admin's default position. The panel always sends
	// the full visible list so the server doesn't have to merge
	// partial orderings — keeps the contract dead simple.
	OrderedChannelIDs []string `json:"ordered_channel_ids"`
	// HiddenChannelIDs is the set of channel IDs the user wants
	// hidden. Pass-through to the service which writes one row per
	// (ordered or hidden) channel. Channels that appear ONLY in
	// HiddenChannelIDs (not in OrderedChannelIDs) get a row too —
	// the personalisation panel calls this when the user hides a
	// channel without reordering.
	HiddenChannelIDs []string `json:"hidden_channel_ids"`
}

// ReplaceChannelOrder accepts the full reordered + hidden list and
// persists it in one transaction.
func (h *iptvPersonalisationHandler) ReplaceChannelOrder(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		handlers.RespondAppError(w, r.Context(), domain.NewUnauthorized("auth required"))
		return
	}
	var req meIPTVChannelOrderRequest
	if err := handlers.DecodeJSON(w, r, &req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	hiddenSet := make(map[string]bool, len(req.HiddenChannelIDs))
	// Build the union of ordered + hidden IDs so a channel the user
	// hid without reordering still gets persisted. The service's
	// ReplaceAll wipes any row not present in the union — that's how
	// the "restore admin order for a subset" flow works.
	allIDs := append([]string(nil), req.OrderedChannelIDs...)
	for _, id := range req.HiddenChannelIDs {
		hiddenSet[id] = true
		if !contains(allIDs, id) {
			allIDs = append(allIDs, id)
		}
	}
	if err := h.svc.ReplaceChannelOrder(r.Context(), claims.UserID, allIDs, hiddenSet); err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	h.publishOrderUpdated(claims.UserID)
	handlers.RespondData(w, http.StatusOK, map[string]any{"status": "ok"})
}

type meIPTVVisibilityRequest struct {
	Hidden bool `json:"hidden"`
}

// SetChannelVisibility is the per-channel "hide / show" toggle. Used
// by the inline button on the channel list when the user wants to
// hide a single channel without opening the full personalisation panel.
func (h *iptvPersonalisationHandler) SetChannelVisibility(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		handlers.RespondAppError(w, r.Context(), domain.NewUnauthorized("auth required"))
		return
	}
	channelID := handlers.RequireParam(w, r, "channelId")
	if channelID == "" {
		return
	}
	var req meIPTVVisibilityRequest
	if err := handlers.DecodeJSON(w, r, &req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	// Defence-in-depth: confirm the user actually has access to the
	// channel's library before persisting their override. Otherwise
	// the personalisation table could grow rows for channels the
	// user couldn't even see — minor data integrity issue, but the
	// check is one DB hit.
	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if !canAccessLibrary(r, h.access, h.logger, ch.LibraryID) {
		iptvDenyForbidden(w, r)
		return
	}
	if err := h.svc.SetChannelVisibility(r.Context(), claims.UserID, channelID, req.Hidden); err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	h.publishOrderUpdated(claims.UserID)
	handlers.RespondData(w, http.StatusOK, map[string]any{"status": "ok"})
}

// ResetChannelOrder wipes the user's overrides, restoring the admin
// defaults for ordering and visibility.
func (h *iptvPersonalisationHandler) ResetChannelOrder(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		handlers.RespondAppError(w, r.Context(), domain.NewUnauthorized("auth required"))
		return
	}
	if err := h.svc.ResetChannelOrder(r.Context(), claims.UserID); err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	h.publishOrderUpdated(claims.UserID)
	handlers.RespondData(w, http.StatusOK, map[string]any{"status": "ok"})
}

func contains(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}
