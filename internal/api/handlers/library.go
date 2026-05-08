package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/library"

	"github.com/go-chi/chi/v5"
)

type LibraryHandler struct {
	lib      LibraryService
	images   ImageRepository
	metadata MetadataRepository
	userData UserDataRepository
	// users resolves the caller's max_content_rating so /items/latest
	// can scope its result set to ratings the profile is allowed to
	// see. Optional — when nil the rating filter is skipped (admin
	// or unknown context, fail-open is the right default).
	users  UserService
	logger *slog.Logger
}

func NewLibraryHandler(lib LibraryService, images ImageRepository, metadata MetadataRepository, userData UserDataRepository, users UserService, logger *slog.Logger) *LibraryHandler {
	return &LibraryHandler{lib: lib, images: images, metadata: metadata, userData: userData, users: users, logger: logger}
}

// callerCapRating resolves the authenticated user's content cap, or
// "" when no caller is attached / no cap is set / a lookup error
// happens. Used by browse + latest handlers to gate the result set.
func (h *LibraryHandler) callerCapRating(ctx context.Context) string {
	if h.users == nil {
		return ""
	}
	claims := auth.GetClaims(ctx)
	if claims == nil {
		return ""
	}
	u, err := h.users.GetByID(ctx, claims.UserID)
	if err != nil || u == nil {
		return ""
	}
	return u.MaxContentRating
}

func (h *LibraryHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req library.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	lib, err := h.lib.Create(r.Context(), req)
	if err != nil {
		handleServiceError(w, r, err)
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
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	if err != nil {
		handleServiceError(w, r, err)
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
		handleServiceError(w, r, err)
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
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	lib, err := h.lib.Update(r.Context(), id, req)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": libraryResponse(lib)})
}

func (h *LibraryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.lib.Delete(r.Context(), id); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *LibraryHandler) Scan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	refreshMeta := r.URL.Query().Get("refresh_metadata") == "true"
	if err := h.lib.Scan(r.Context(), id, refreshMeta); err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusAccepted, map[string]any{
		"data": map[string]any{"status": "scanning", "library_id": id},
	})
}

// Browse lists subdirectories of a path on the host filesystem so the
// admin "create / edit library" UI can pick a folder. It's a pure
// read on a server-controlled set of paths (anchored by Abs +
// isSensitiveBrowsePath) so the right HTTP verb is GET, not POST —
// using POST forced this through the CSRF middleware, made the
// response uncacheable by the browser, and meant every open of the
// folder picker paid a full round-trip even when the user had just
// browsed the same directory seconds earlier.
//
// Path comes in via the `path` query parameter; an empty path defaults
// to "/" (the container root, which is what the wizard wants on first
// open).
func (h *LibraryHandler) Browse(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		reqPath = "/"
	}

	absPath, err := filepath.Abs(reqPath)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "BROWSE_ERROR", "invalid path")
		return
	}

	// Block access to sensitive system directories
	if isSensitiveBrowsePath(absPath) {
		respondError(w, r, http.StatusForbidden, "BROWSE_ERROR", "access denied: cannot browse system directories")
		return
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "BROWSE_ERROR", "directory not found or not accessible")
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

	// Short browser-side cache. Folder layout doesn't change second-to-
	// second, but it does change (operator drops a new folder, mounts a
	// drive). 30s is short enough that any real change is picked up
	// quickly while still letting the modal re-open instantly when the
	// user closes and re-opens it within the same flow.
	w.Header().Set("Cache-Control", "private, max-age=30")
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
		LibraryID:             id,
		ParentID:              parentID,
		Type:                  itemType,
		AllowedContentRatings: library.AllowedRatingsAtMost(h.callerCapRating(r.Context())),
		Limit:                 limit,
		Offset:                offset,
		SortBy:                sortBy,
		SortOrder:             sortOrder,
		Cursor:                cursor,
	})
	if err != nil {
		handleServiceError(w, r, err)
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

// AllItems lists items across every library, paginated and sorted.
// Mirrors `Items` but without the LibraryID scope so the global
// browse pages (`/movies`, `/series`) can fetch the full catalogue
// without having to fan out per library on the client. Same response
// shape as `Items` — keyset cursor + offset/total — so the frontend
// `useInfiniteItems` hook works against either path.
//
// Without this route the same pages used to call `/items/latest`
// which is capped at 50 results and doesn't paginate, surfacing as a
// truncated grid.
func (h *LibraryHandler) AllItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	sortBy := q.Get("sort_by")
	sortOrder := q.Get("sort_order")
	itemType := q.Get("type")
	parentID := q.Get("parent_id")
	cursor := q.Get("cursor")
	// Search + facet filters: piped to the repository so a 100k-row
	// catalogue doesn't pay round-trips to surface a single result.
	// Empty / zero values disable each filter — see ItemFilter.
	queryStr := q.Get("q")
	genre := q.Get("genre")
	yearFrom, _ := strconv.Atoi(q.Get("year_from"))
	yearTo, _ := strconv.Atoi(q.Get("year_to"))
	minRating, _ := strconv.ParseFloat(q.Get("min_rating"), 64)

	items, total, err := h.lib.ListItems(r.Context(), db.ItemFilter{
		ParentID:              parentID,
		Type:                  itemType,
		Query:                 queryStr,
		Genre:                 genre,
		YearFrom:              yearFrom,
		YearTo:                yearTo,
		MinRating:             minRating,
		AllowedContentRatings: library.AllowedRatingsAtMost(h.callerCapRating(r.Context())),
		Limit:                 limit,
		Offset:                offset,
		SortBy:                sortBy,
		SortOrder:             sortOrder,
		Cursor:                cursor,
	})
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	data := h.enrichItemSummaries(r, items)

	resp := map[string]any{
		"items":  data,
		"total":  total,
		"offset": offset,
		"limit":  limit,
	}
	if len(items) > 0 && len(items) == limit {
		resp["next_cursor"] = items[len(items)-1].ID
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
}

// Genres exposes the catalogue's genre vocabulary so the /movies and
// /series filter panel can show a complete chip list independent of
// what the infinite scroll has fetched. Optional `type` query param
// scopes the vocabulary ("movie" or "series"); empty returns the union.
func (h *LibraryHandler) Genres(w http.ResponseWriter, r *http.Request) {
	itemType := r.URL.Query().Get("type")
	if itemType != "" && itemType != "movie" && itemType != "series" && itemType != "episode" {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "type must be one of movie, series, episode")
		return
	}
	genres, err := h.lib.ListGenres(r.Context(), itemType)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	data := make([]map[string]any, len(genres))
	for i, g := range genres {
		data[i] = map[string]any{"name": g.Name, "count": g.Count}
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *LibraryHandler) LatestItems(w http.ResponseWriter, r *http.Request) {
	libraryID := r.URL.Query().Get("library_id")
	itemType := r.URL.Query().Get("type")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	cap := h.callerCapRating(r.Context())

	// Activity-aware shows rail: when the caller asks for the latest
	// series scoped to one library, we route to a dedicated query
	// that orders by recent episode activity and includes the
	// per-series new-episode count. Lets the home rail say "Mr Robot
	// got 3 new episodes this week" without a follow-up roundtrip.
	// Movies / mixed / global queries keep the simple `ORDER BY
	// added_at DESC` path.
	if itemType == "series" && libraryID != "" {
		rows, err := h.lib.LatestSeriesByActivity(r.Context(), libraryID, limit)
		if err != nil {
			handleServiceError(w, r, err)
			return
		}
		// Apply the per-profile content-rating cap in memory. The
		// activity-aware query doesn't accept an allow-list because
		// the SQL is already busy joining episodes; filtering N≤20
		// rows post-fetch is cheaper than restructuring the query.
		if cap != "" {
			filtered := rows[:0]
			for _, row := range rows {
				if library.AllowedRating(row.ContentRating, cap) {
					filtered = append(filtered, row)
				}
			}
			rows = filtered
		}
		// Adapter layer: we still want the standard image / metadata
		// enrichment that operates on []*db.Item, then we splice the
		// activity stamp + new-episode count back into each entry by
		// position so the wire stays a flat MediaItem-shaped list.
		items := make([]*db.Item, len(rows))
		for i, r := range rows {
			cp := r.Item
			items[i] = &cp
		}
		data := h.enrichItemSummaries(r, items)
		for i, row := range rows {
			if !row.LatestActivityAt.IsZero() {
				data[i]["latest_activity_at"] = row.LatestActivityAt.UTC().Format(time.RFC3339)
			}
			if row.NewEpisodesCount > 0 {
				data[i]["new_episodes_count"] = row.NewEpisodesCount
			}
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"items":  data,
				"total":  len(rows),
				"offset": 0,
				"limit":  limit,
			},
		})
		return
	}

	items, err := h.lib.LatestItems(r.Context(), libraryID, itemType, limit, cap)
	if err != nil {
		handleServiceError(w, r, err)
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
						data[i]["poster_url"] = poster.Path
						attachPosterPlaceholder(data[i], poster)
					}
					if backdrop, ok := urls["backdrop"]; ok {
						data[i]["backdrop_url"] = backdrop.Path
					}
					if logo, ok := urls["logo"]; ok {
						data[i]["logo_url"] = logo.Path
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

	// Batch fetch per-user state (watched/in-progress/favorite). Only when
	// authenticated; anonymous endpoints (none today, but defensive) skip
	// silently. Failure is logged, not fatal — the listing still renders
	// without badges instead of 500ing.
	if h.userData != nil {
		if claims := auth.GetClaims(r.Context()); claims != nil {
			userDataByID, err := h.userData.GetBatch(r.Context(), claims.UserID, itemIDs)
			if err != nil {
				h.logger.Warn("failed to fetch user data batch", "error", err)
			} else if len(userDataByID) > 0 {
				for i, item := range items {
					if ud, ok := userDataByID[item.ID]; ok {
						data[i]["user_data"] = userDataResponse(ud, item.DurationTicks)
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
		// IPTV-specific fields — always present but empty for non-livetv
		// libraries. Exposed so the admin UI can render the right actions
		// (refresh M3U / refresh EPG) and show configuration at a glance.
		"m3u_url":         lib.M3UURL,
		"epg_url":         lib.EPGURL,
		"language_filter": splitLanguageFilter(lib.LanguageFilter),
		"tls_insecure":    lib.TLSInsecure,
	}
}

// splitLanguageFilter inverts library.normaliseLanguageFilter for the
// wire: the column stores "es,en" and the JSON contract is a string
// array. Empty column → empty array (never null) so the frontend can
// dispatch on `length === 0` without optional-chaining.
func splitLanguageFilter(stored string) []string {
	if stored == "" {
		return []string{}
	}
	parts := strings.Split(stored, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func itemSummaryResponse(item *db.Item) map[string]any {
	resp := map[string]any{
		"id":             item.ID,
		"library_id":     item.LibraryID,
		"type":           item.Type,
		"title":          item.Title,
		// `sort_title` is the lowercased + article-stripped variant the
		// backend stores for SQL ORDER BY (so "The Matrix" sorts as
		// "matrix"). The browse page also re-sorts client-side when
		// the user picks "title" — without this field on the wire it
		// did `undefined.localeCompare(...)` and crashed the grid.
		"sort_title":     item.SortTitle,
		"duration_ticks": item.DurationTicks,
		"is_available":   item.IsAvailable,
		"added_at":       item.AddedAt,
	}
	if item.Year > 0 {
		resp["year"] = item.Year
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
	if item.PremiereDate != nil {
		resp["premiere_date"] = item.PremiereDate
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
