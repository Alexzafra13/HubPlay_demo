package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"hubplay/internal/auth"
	"hubplay/internal/event"

	"github.com/go-chi/chi/v5"
)

// ProgressHandler handles watch progress and user engagement endpoints.
type ProgressHandler struct {
	userData UserDataRepository
	images   ImageRepository
	bus      EventBusPublisher
	logger   *slog.Logger
}

// NewProgressHandler creates a new progress handler. The bus is
// optional — pass nil in test rigs that don't care about cross-device
// sync; the handler skips Publish calls cleanly in that case.
func NewProgressHandler(userData UserDataRepository, images ImageRepository, bus EventBusPublisher, logger *slog.Logger) *ProgressHandler {
	return &ProgressHandler{
		userData: userData,
		images:   images,
		bus:      bus,
		logger:   logger.With("module", "progress-handler"),
	}
}

// publish fans out a user-scoped event to the bus. The /me/events SSE
// endpoint reads `user_id` from Data and only forwards events to that
// user's connected clients — other users on the same server never see
// these. nil-bus is a no-op so test rigs without a bus stay simple.
func (h *ProgressHandler) publish(t event.Type, userID, itemID string, extra map[string]any) {
	if h.bus == nil {
		return
	}
	data := map[string]any{
		"user_id": userID,
		"item_id": itemID,
	}
	for k, v := range extra {
		data[k] = v
	}
	h.bus.Publish(event.Event{Type: t, Data: data})
}

// GetProgress returns the user's data for a specific item.
func (h *ProgressHandler) GetProgress(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	itemID := chi.URLParam(r, "itemId")
	ud, err := h.userData.Get(r.Context(), claims.UserID, itemID)
	if err != nil {
		h.logger.Error("get progress", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get progress")
		return
	}

	if ud == nil {
		respondJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"item_id":        itemID,
				"position_ticks": 0,
				"play_count":     0,
				"completed":      false,
				"is_favorite":    false,
			},
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"item_id":               ud.ItemID,
			"position_ticks":        ud.PositionTicks,
			"play_count":            ud.PlayCount,
			"completed":             ud.Completed,
			"is_favorite":           ud.IsFavorite,
			"liked":                 ud.Liked,
			"audio_stream_index":    ud.AudioStreamIndex,
			"subtitle_stream_index": ud.SubtitleStreamIndex,
			"last_played_at":        ud.LastPlayedAt,
		},
	})
}

type updateProgressRequest struct {
	PositionTicks int64 `json:"position_ticks"`
	Completed     *bool `json:"completed"`
}

// UpdateProgress saves the current playback position.
func (h *ProgressHandler) UpdateProgress(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	itemID := chi.URLParam(r, "itemId")

	var req updateProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	completed := false
	if req.Completed != nil {
		completed = *req.Completed
	}

	if err := h.userData.UpdateProgress(r.Context(), claims.UserID, itemID, req.PositionTicks, completed); err != nil {
		h.logger.Error("update progress", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update progress")
		return
	}
	h.publish(event.ProgressUpdated, claims.UserID, itemID, map[string]any{
		"position_ticks": req.PositionTicks,
		"completed":      completed,
	})

	w.WriteHeader(http.StatusNoContent)
}

// MarkPlayed marks an item as fully played.
func (h *ProgressHandler) MarkPlayed(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	itemID := chi.URLParam(r, "itemId")
	if err := h.userData.MarkPlayed(r.Context(), claims.UserID, itemID); err != nil {
		h.logger.Error("mark played", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to mark played")
		return
	}
	h.publish(event.PlayedToggled, claims.UserID, itemID, map[string]any{
		"played":    true,
		"completed": true,
	})

	w.WriteHeader(http.StatusNoContent)
}

// MarkUnplayed resets an item's watch state.
func (h *ProgressHandler) MarkUnplayed(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	itemID := chi.URLParam(r, "itemId")
	if err := h.userData.Delete(r.Context(), claims.UserID, itemID); err != nil {
		h.logger.Error("mark unplayed", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to mark unplayed")
		return
	}
	h.publish(event.PlayedToggled, claims.UserID, itemID, map[string]any{
		"played":    false,
		"completed": false,
	})

	w.WriteHeader(http.StatusNoContent)
}

// ToggleFavorite toggles the favorite state for an item.
func (h *ProgressHandler) ToggleFavorite(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	itemID := chi.URLParam(r, "itemId")

	// Get current state
	ud, err := h.userData.Get(r.Context(), claims.UserID, itemID)
	if err != nil {
		h.logger.Error("get favorite state", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get favorite state")
		return
	}

	newState := true
	if ud != nil {
		newState = !ud.IsFavorite
	}

	if err := h.userData.SetFavorite(r.Context(), claims.UserID, itemID, newState); err != nil {
		h.logger.Error("toggle favorite", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to toggle favorite")
		return
	}
	h.publish(event.FavoriteToggled, claims.UserID, itemID, map[string]any{
		"is_favorite": newState,
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"item_id":     itemID,
			"is_favorite": newState,
		},
	})
}

// ContinueWatching returns items the user has started but not finished.
func (h *ProgressHandler) ContinueWatching(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	items, err := h.userData.ContinueWatching(r.Context(), claims.UserID, limit)
	if err != nil {
		h.logger.Error("continue watching", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get continue watching")
		return
	}

	// Batch-fetch images for all items
	itemIDs := make([]string, len(items))
	for i, item := range items {
		itemIDs[i] = item.ItemID
	}
	imageMap, _ := h.images.GetPrimaryURLs(r.Context(), itemIDs)

	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{
			"id":             item.ItemID,
			"item_id":        item.ItemID, // backwards compat
			"title":          item.Title,
			"type":           item.Type,
			"position_ticks": item.PositionTicks,
			"duration_ticks": item.DurationTicks,
			"last_played_at": item.LastPlayedAt,
			"parent_id":      item.ParentID,
			"poster_url":     nil,
			"backdrop_url":   nil,
			"logo_url":       nil,
		}
		// Episode coordinates so the SeriesHero / season "Sigue viendo"
		// panel can render the SXXEYY badge without a second hop.
		// 0 means absent (movie / orphan episode); only emit when set.
		if item.SeasonNumber > 0 {
			entry["season_number"] = item.SeasonNumber
		}
		if item.EpisodeNumber > 0 {
			entry["episode_number"] = item.EpisodeNumber
		}
		if item.SeriesID != "" {
			entry["series_id"] = item.SeriesID
		}
		if urls, ok := imageMap[item.ItemID]; ok {
			if u, ok := urls["primary"]; ok {
				entry["poster_url"] = u.Path
				attachPosterPlaceholder(entry, u)
			}
			if u, ok := urls["backdrop"]; ok {
				entry["backdrop_url"] = u.Path
			}
			if u, ok := urls["logo"]; ok {
				entry["logo_url"] = u.Path
			}
		}
		result = append(result, entry)
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

// Favorites returns items the user has marked as favorite.
func (h *ProgressHandler) Favorites(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if o, err := strconv.Atoi(v); err == nil && o >= 0 {
			offset = o
		}
	}

	items, err := h.userData.Favorites(r.Context(), claims.UserID, limit, offset)
	if err != nil {
		h.logger.Error("favorites", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get favorites")
		return
	}

	// Batch-fetch images for all items
	favIDs := make([]string, len(items))
	for i, item := range items {
		favIDs[i] = item.ItemID
	}
	favImageMap, _ := h.images.GetPrimaryURLs(r.Context(), favIDs)

	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{
			"id":             item.ItemID,
			"item_id":        item.ItemID, // backwards compat
			"title":          item.Title,
			"type":           item.Type,
			"year":           item.Year,
			"duration_ticks": item.DurationTicks,
			"favorited_at":   item.FavoritedAt,
			"poster_url":     nil,
			"backdrop_url":   nil,
		}
		if urls, ok := favImageMap[item.ItemID]; ok {
			if u, ok := urls["primary"]; ok {
				entry["poster_url"] = u.Path
				attachPosterPlaceholder(entry, u)
			}
			if u, ok := urls["backdrop"]; ok {
				entry["backdrop_url"] = u.Path
			}
		}
		result = append(result, entry)
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

// NextUp returns the next unwatched episode per series.
func (h *ProgressHandler) NextUp(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	items, err := h.userData.NextUp(r.Context(), claims.UserID, limit)
	if err != nil {
		h.logger.Error("next up", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get next up")
		return
	}

	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{
			"id":             item.EpisodeID,
			"episode_id":     item.EpisodeID, // backwards compat
			"episode_title":  item.EpisodeTitle,
			"season_number":  item.SeasonNumber,
			"episode_number": item.EpisodeNumber,
			"duration_ticks": item.DurationTicks,
			"series_title":   item.SeriesTitle,
			"series_id":      item.SeriesID,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}
