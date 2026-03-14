package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/library"

	"github.com/go-chi/chi/v5"
)

type LibraryHandler struct {
	lib    *library.Service
	logger *slog.Logger
}

func NewLibraryHandler(lib *library.Service, logger *slog.Logger) *LibraryHandler {
	return &LibraryHandler{lib: lib, logger: logger}
}

func (h *LibraryHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req library.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	lib, err := h.lib.Create(r.Context(), req)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{"data": libraryResponse(lib)})
}

func (h *LibraryHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())

	var libs []*db.Library
	var err error

	if claims != nil && claims.Role == "admin" {
		libs, err = h.lib.List(r.Context())
	} else if claims != nil {
		libs, err = h.lib.ListForUser(r.Context(), claims.UserID)
	} else {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	if err != nil {
		handleServiceError(w, err)
		return
	}

	items := make([]map[string]any, len(libs))
	for i, lib := range libs {
		items[i] = libraryResponse(lib)
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *LibraryHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	lib, err := h.lib.GetByID(r.Context(), id)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	count, _ := h.lib.ItemCount(r.Context(), id)

	resp := libraryResponse(lib)
	resp["item_count"] = count
	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *LibraryHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req library.UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	lib, err := h.lib.Update(r.Context(), id, req)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": libraryResponse(lib)})
}

func (h *LibraryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.lib.Delete(r.Context(), id); err != nil {
		handleServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *LibraryHandler) Scan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.lib.Scan(r.Context(), id); err != nil {
		handleServiceError(w, err)
		return
	}
	respondJSON(w, http.StatusAccepted, map[string]any{
		"data": map[string]any{"status": "scanning", "library_id": id},
	})
}

func (h *LibraryHandler) Items(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	sortBy := r.URL.Query().Get("sort_by")
	sortOrder := r.URL.Query().Get("sort_order")
	itemType := r.URL.Query().Get("type")
	parentID := r.URL.Query().Get("parent_id")

	items, total, err := h.lib.ListItems(r.Context(), db.ItemFilter{
		LibraryID: id,
		ParentID:  parentID,
		Type:      itemType,
		Limit:     limit,
		Offset:    offset,
		SortBy:    sortBy,
		SortOrder: sortOrder,
	})
	if err != nil {
		handleServiceError(w, err)
		return
	}

	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = itemSummaryResponse(item)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"items":  data,
			"total":  total,
			"offset": offset,
			"limit":  limit,
		},
	})
}

func (h *LibraryHandler) LatestItems(w http.ResponseWriter, r *http.Request) {
	libraryID := r.URL.Query().Get("library_id")
	itemType := r.URL.Query().Get("type")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	items, err := h.lib.LatestItems(r.Context(), libraryID, itemType, limit)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = itemSummaryResponse(item)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"items":  data,
			"total":  len(items),
			"offset": 0,
			"limit":  limit,
		},
	})
}

func libraryResponse(lib *db.Library) map[string]any {
	return map[string]any{
		"id":           lib.ID,
		"name":         lib.Name,
		"content_type": lib.ContentType,
		"scan_mode":    lib.ScanMode,
		"paths":        lib.Paths,
		"created_at":   lib.CreatedAt,
		"updated_at":   lib.UpdatedAt,
	}
}

func itemSummaryResponse(item *db.Item) map[string]any {
	resp := map[string]any{
		"id":             item.ID,
		"library_id":     item.LibraryID,
		"type":           item.Type,
		"title":          item.Title,
		"year":           item.Year,
		"duration_ticks": item.DurationTicks,
		"is_available":   item.IsAvailable,
		"added_at":       item.AddedAt,
	}
	if item.ParentID != "" {
		resp["parent_id"] = item.ParentID
	}
	if item.SeasonNumber != nil {
		resp["season_number"] = *item.SeasonNumber
	}
	if item.EpisodeNumber != nil {
		resp["episode_number"] = *item.EpisodeNumber
	}
	if item.CommunityRating != nil {
		resp["community_rating"] = *item.CommunityRating
	}
	return resp
}
