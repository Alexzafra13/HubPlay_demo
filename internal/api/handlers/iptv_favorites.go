// Channel favorites + continue-watching rail.
//
// Routes:
//   GET    /api/v1/favorites/channels                  — list (full channels)
//   GET    /api/v1/favorites/channels/ids              — list (just IDs)
//   PUT    /api/v1/favorites/channels/{channelId}      — add favorite
//   DELETE /api/v1/favorites/channels/{channelId}      — remove favorite
//   POST   /api/v1/channels/{channelId}/watch          — beacon
//   GET    /api/v1/me/channels/continue-watching       — recent rail
//
// Authorization: user is derived from JWT claims. Add/Remove/Watch
// additionally verify the caller can access the channel's library
// (same ACL gate as `canAccessLibrary` — consistent with the rest
// of the IPTV surface).

package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/domain"
)

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

// ── Continue watching ────────────────────────────────────────────
//
// Beacon (POST /channels/{id}/watch) is fire-and-forget from the
// player. Rail (GET /me/channels/continue-watching) reads back the
// top-N most recent. Limit is capped at 20 to bound payload.

const (
	continueWatchingMaxLimit     = 20
	continueWatchingDefaultLimit = 10
)

// RecordChannelWatch receives the player beacon. Admin-or-user gated:
// any authenticated user can record their own history, but they must
// have library access to the channel — otherwise the endpoint would
// leak channel existence via "can I insert a row against this id?".
func (h *IPTVHandler) RecordChannelWatch(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondAppError(w, r.Context(), domain.NewUnauthorized("auth required"))
		return
	}
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

	ts, err := h.svc.RecordWatch(r.Context(), claims.UserID, channelID)
	if err != nil {
		if errors.Is(err, db.ErrChannelNotFound) {
			h.denyForbidden(w, r)
			return
		}
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"channel_id":      channelID,
			"last_watched_at": ts.UTC().Format(time.RFC3339),
		},
	})
}

// ListContinueWatching returns the caller's most recently watched
// channels, newest first. Limit defaults to 10 and is capped at 20.
//
// ACL: admins see everything, non-admin users see only channels in
// libraries they have access to. The filter is applied in the service
// via accessibleLibraries (nil = admin bypass).
func (h *IPTVHandler) ListContinueWatching(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondAppError(w, r.Context(), domain.NewUnauthorized("auth required"))
		return
	}

	limit := continueWatchingDefaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > continueWatchingMaxLimit {
		limit = continueWatchingMaxLimit
	}

	// accessibleLibraries==nil signals "no filter" (admin bypass).
	// For regular users we materialise the ACL set once and pass it
	// through. Empty map means "deny everything" and correctly
	// produces an empty rail.
	var accessible map[string]bool
	if claims.Role != "admin" {
		libs, err := h.libraries.ListForUser(r.Context(), claims.UserID)
		if err != nil {
			h.logger.Error("list user libraries for continue-watching",
				"user", claims.UserID, "error", err)
			handleServiceError(w, r, err)
			return
		}
		accessible = make(map[string]bool, len(libs))
		for _, lib := range libs {
			accessible[lib.ID] = true
		}
	}

	channels, watched, err := h.svc.ListContinueWatching(r.Context(), claims.UserID, limit, accessible)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	result := make([]map[string]any, 0, len(channels))
	for i, ch := range channels {
		dto := toChannelDTO(ch, "/api/v1/channels/"+ch.ID+"/stream")
		row := map[string]any{
			"id":              dto.ID,
			"name":            dto.Name,
			"number":          dto.Number,
			"group":           dto.Group,
			"group_name":      dto.GroupName,
			"category":        dto.Category,
			"logo_url":        dto.LogoURL,
			"logo_initials":   dto.LogoInitials,
			"logo_bg":         dto.LogoBg,
			"logo_fg":         dto.LogoFg,
			"stream_url":      dto.StreamURL,
			"library_id":      dto.LibraryID,
			"tvg_id":          dto.TvgID,
			"language":        dto.Language,
			"country":         dto.Country,
			"is_active":       dto.IsActive,
			"added_at":        dto.AddedAt,
			"last_watched_at": watched[i].UTC().Format(time.RFC3339),
		}
		result = append(result, row)
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}
