package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/library"

	"github.com/go-chi/chi/v5"
)

type LibraryHandler struct {
	lib      LibraryService
	images   ImageRepository
	metadata MetadataRepository
	logger   *slog.Logger
}

func NewLibraryHandler(lib LibraryService, images ImageRepository, metadata MetadataRepository, logger *slog.Logger) *LibraryHandler {
	return &LibraryHandler{lib: lib, images: images, metadata: metadata, logger: logger}
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
		resp := libraryResponse(lib)
		count, _ := h.lib.ItemCount(r.Context(), lib.ID)
		resp["item_count"] = count
		items[i] = resp
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
	refreshMeta := r.URL.Query().Get("refresh_metadata") == "true"
	if err := h.lib.Scan(r.Context(), id, refreshMeta); err != nil {
		handleServiceError(w, err)
		return
	}
	respondJSON(w, http.StatusAccepted, map[string]any{
		"data": map[string]any{"status": "scanning", "library_id": id},
	})
}

func (h *LibraryHandler) Browse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if req.Path == "" {
		req.Path = "/"
	}

	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		respondError(w, http.StatusBadRequest, "BROWSE_ERROR", "invalid path")
		return
	}

	// Block access to sensitive system directories
	if isSensitiveBrowsePath(absPath) {
		respondError(w, http.StatusForbidden, "BROWSE_ERROR", "access denied: cannot browse system directories")
		return
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		respondError(w, http.StatusBadRequest, "BROWSE_ERROR", "directory not found or not accessible")
		return
	}

	type dirEntry struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	dirs := make([]dirEntry, 0)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dirs = append(dirs, dirEntry{
			Name: entry.Name(),
			Path: filepath.Join(absPath, entry.Name()),
		})
	}

	parent := filepath.Dir(absPath)
	if parent == absPath {
		parent = ""
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"current":     absPath,
			"parent":      parent,
			"directories": dirs,
		},
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
	cursor := r.URL.Query().Get("cursor")

	items, total, err := h.lib.ListItems(r.Context(), db.ItemFilter{
		LibraryID: id,
		ParentID:  parentID,
		Type:      itemType,
		Limit:     limit,
		Offset:    offset,
		SortBy:    sortBy,
		SortOrder: sortOrder,
		Cursor:    cursor,
	})
	if err != nil {
		handleServiceError(w, err)
		return
	}

	data := h.enrichItemSummaries(r, items)

	resp := map[string]any{
		"items":  data,
		"total":  total,
		"offset": offset,
		"limit":  limit,
	}
	// Return next_cursor for keyset pagination
	if len(items) > 0 && len(items) == limit {
		resp["next_cursor"] = items[len(items)-1].ID
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
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

	data := h.enrichItemSummaries(r, items)

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"items":  data,
			"total":  len(items),
			"offset": 0,
			"limit":  limit,
		},
	})
}

// enrichItemSummaries adds poster_url, backdrop_url, overview, and genres to item summaries.
func (h *LibraryHandler) enrichItemSummaries(r *http.Request, items []*db.Item) []map[string]any {
	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = itemSummaryResponse(item)
	}

	if len(items) == 0 {
		return data
	}

	itemIDs := make([]string, len(items))
	for i, item := range items {
		itemIDs[i] = item.ID
	}

	// Batch fetch image URLs
	if h.images != nil {
		imageURLs, err := h.images.GetPrimaryURLs(r.Context(), itemIDs)
		if err != nil {
			h.logger.Warn("failed to fetch image URLs", "error", err)
		} else {
			for i, item := range items {
				if urls, ok := imageURLs[item.ID]; ok {
					if poster, ok := urls["primary"]; ok {
						data[i]["poster_url"] = poster
					}
					if backdrop, ok := urls["backdrop"]; ok {
						data[i]["backdrop_url"] = backdrop
					}
					if logo, ok := urls["logo"]; ok {
						data[i]["logo_url"] = logo
					}
				}
			}
		}
	}

	// Batch fetch metadata (overview, genres)
	if h.metadata != nil {
		metas, err := h.metadata.GetMetadataBatch(r.Context(), itemIDs)
		if err != nil {
			h.logger.Warn("failed to fetch metadata batch", "error", err)
		} else {
			for i, item := range items {
				if m, ok := metas[item.ID]; ok {
					if m.Overview != "" {
						data[i]["overview"] = m.Overview
					}
					if m.Tagline != "" {
						data[i]["tagline"] = m.Tagline
					}
					if m.GenresJSON != "" {
						var genres []string
						if err := json.Unmarshal([]byte(m.GenresJSON), &genres); err == nil {
							data[i]["genres"] = genres
						}
					}
				}
			}
		}
	}

	return data
}

func libraryResponse(lib *db.Library) map[string]any {
	// Check which paths are accessible
	pathStatus := make([]map[string]any, len(lib.Paths))
	for i, p := range lib.Paths {
		_, err := os.Stat(p)
		pathStatus[i] = map[string]any{
			"path":       p,
			"accessible": err == nil,
		}
	}

	return map[string]any{
		"id":           lib.ID,
		"name":         lib.Name,
		"content_type": lib.ContentType,
		"scan_mode":    lib.ScanMode,
		"paths":        lib.Paths,
		"path_status":  pathStatus,
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
	if item.ContentRating != "" {
		resp["content_rating"] = item.ContentRating
	}
	return resp
}

// sensitiveBrowsePaths are system directories that should not be browsable.
var sensitiveBrowsePaths = []string{
	"/etc", "/proc", "/sys", "/dev", "/boot", "/root",
	"/var/run", "/var/log", "/run", "/sbin", "/usr/sbin",
}

// isSensitiveBrowsePath returns true if the path is inside a sensitive system directory.
func isSensitiveBrowsePath(absPath string) bool {
	cleaned := filepath.Clean(absPath)
	for _, sp := range sensitiveBrowsePaths {
		if cleaned == sp || strings.HasPrefix(cleaned, sp+"/") {
			return true
		}
	}
	return false
}
