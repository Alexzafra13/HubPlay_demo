package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/library"
	librarymodel "hubplay/internal/library/model"
)

// libraryOps es el contrato mínimo que LibraryHandler necesita del
// library service. 12 de 25 métodos — excluye ACL, personal IPTV y
// scan síncrono que sólo usan otros handlers. Cierra NN para este
// consumer.
type libraryOps interface {
	Create(ctx context.Context, req library.CreateRequest) (*librarymodel.Library, error)
	GetByID(ctx context.Context, id string) (*librarymodel.Library, error)
	List(ctx context.Context) ([]*librarymodel.Library, error)
	ListForUser(ctx context.Context, userID string) ([]*librarymodel.Library, error)
	Update(ctx context.Context, id string, req library.UpdateRequest) (*librarymodel.Library, error)
	Delete(ctx context.Context, id string) error
	Scan(ctx context.Context, id string, refreshMetadata ...bool) error
	ListItems(ctx context.Context, filter librarymodel.ItemFilter) ([]*librarymodel.Item, int, error)
	LatestItems(ctx context.Context, libraryID string, itemType string, limit int, capRating string) ([]*librarymodel.Item, error)
	LatestSeriesByActivity(ctx context.Context, libraryID string, limit int) ([]*librarymodel.LatestSeriesActivity, error)
	ItemCount(ctx context.Context, libraryID string) (int, error)
	ListGenres(ctx context.Context, itemType string) ([]librarymodel.GenreCount, error)
}

type LibraryHandler struct {
	lib      libraryOps
	images   ImageRepository
	metadata MetadataRepository
	userData UserDataRepository
	users    UserService
	audit    AuditEmitter
	logger   *slog.Logger
}

func NewLibraryHandler(lib libraryOps, images ImageRepository, metadata MetadataRepository, userData UserDataRepository, users UserService, audit AuditEmitter, logger *slog.Logger) *LibraryHandler {
	return &LibraryHandler{lib: lib, images: images, metadata: metadata, userData: userData, users: users, audit: audit, logger: logger}
}

func (h *LibraryHandler) auditEmit() AuditEmitter {
	if h.audit != nil {
		return h.audit
	}
	return noopAudit{}
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
	h.auditEmit().LogLibraryCreated(r.Context(), r, lib.ID, lib.Name, lib.ContentType)

	respondData(w, http.StatusCreated, libraryResponse(lib))
}

func (h *LibraryHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())

	var libs []*librarymodel.Library
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

	respondData(w, http.StatusOK, items)
}

func (h *LibraryHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := requireParam(w, r, "id")
	if id == "" {
		return
	}
	lib, err := h.lib.GetByID(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	count, _ := h.lib.ItemCount(r.Context(), id)

	resp := libraryResponse(lib)
	resp["item_count"] = count
	respondData(w, http.StatusOK, resp)
}

func (h *LibraryHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := requireParam(w, r, "id")
	if id == "" {
		return
	}

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

	respondData(w, http.StatusOK, libraryResponse(lib))
}

func (h *LibraryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := requireParam(w, r, "id")
	if id == "" {
		return
	}
	// Capturamos nombre pre-delete para el audit.
	var name string
	if lib, err := h.lib.GetByID(r.Context(), id); err == nil {
		name = lib.Name
	}
	if err := h.lib.Delete(r.Context(), id); err != nil {
		handleServiceError(w, r, err)
		return
	}
	h.auditEmit().LogLibraryDeleted(r.Context(), r, id, name)
	w.WriteHeader(http.StatusNoContent)
}

func (h *LibraryHandler) Scan(w http.ResponseWriter, r *http.Request) {
	id := requireParam(w, r, "id")
	if id == "" {
		return
	}
	refreshMeta := r.URL.Query().Get("refresh_metadata") == "true"
	if err := h.lib.Scan(r.Context(), id, refreshMeta); err != nil {
		handleServiceError(w, r, err)
		return
	}
	h.auditEmit().LogLibraryScanStarted(r.Context(), r, id)
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
	w.Header().Set("Cache-Control", CacheControlListing)
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"current":     absPath,
			"parent":      parent,
			"directories": dirs,
		},
	})
}

func (h *LibraryHandler) Items(w http.ResponseWriter, r *http.Request) {
	id := requireParam(w, r, "id")
	if id == "" {
		return
	}
	offset, limit, _ := parsePagination(w, r)
	sortBy := r.URL.Query().Get("sort_by")
	sortOrder := r.URL.Query().Get("sort_order")
	itemType := r.URL.Query().Get("type")
	parentID := r.URL.Query().Get("parent_id")
	cursor := r.URL.Query().Get("cursor")

	items, total, err := h.lib.ListItems(r.Context(), librarymodel.ItemFilter{
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

	respondData(w, http.StatusOK, resp)
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
	offset, limit, _ := parsePaginationFromValues(w, r, q)
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

	items, total, err := h.lib.ListItems(r.Context(), librarymodel.ItemFilter{
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

	respondData(w, http.StatusOK, resp)
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
	respondData(w, http.StatusOK, data)
}

func (h *LibraryHandler) LatestItems(w http.ResponseWriter, r *http.Request) {
	libraryID := r.URL.Query().Get("library_id")
	itemType := r.URL.Query().Get("type")
	_, limit, _ := parsePagination(w, r)
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
		// enrichment that operates on []*librarymodel.Item, then we splice the
		// activity stamp + new-episode count back into each entry by
		// position so the wire stays a flat MediaItem-shaped list.
		items := make([]*librarymodel.Item, len(rows))
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

// AdminRecentlyAdded — GET /admin/system/recently-added. Lo que el
// dashboard admin pinta en el strip "Recientemente añadido".
//
// Por que un endpoint dedicado en vez de reusar /items/latest:
//
//  1. /items/latest sin filtros devuelve episodios sueltos porque
//     es lo que mas se añade en bibliotecas de series. Un strip
//     saturado de "Show X · S2E5, Show X · S2E6, Show X · S2E7"
//     no es lo que el operador quiere ver - quiere ver "Show X
//     con 3 nuevos episodios" como hace Plex.
//
//  2. Hace falta MEZCLAR movies + series ordenadas por recency.
//     Movies vienen de LatestItems(type=movie); series vienen de
//     LatestSeriesByActivity (que ya rollupea por serie con un
//     contador new_episodes_count en una ventana de 14 dias).
//     Ningun endpoint del dashboard hacia esta mezcla.
//
//  3. Hereda el cap por content_rating del caller automatico.
//
// Eficiencia: 2 queries SQL en serie (no paralelas porque no merece
// la complejidad de goroutines + sync para un endpoint admin que
// se llama una vez por minuto). Cada una es indexed por added_at +
// type. Merge + sort en memoria sobre N+M items = trivial.
func (h *LibraryHandler) AdminRecentlyAdded(w http.ResponseWriter, r *http.Request) {
	_, limit, _ := parsePagination(w, r)
	if limit <= 0 {
		limit = 12
	}
	if limit > 50 {
		limit = 50
	}
	cap := h.callerCapRating(r.Context())

	// Pedimos a cada query un poco mas que `limit` para tener margen
	// tras filtrar por content_rating + del merge. 2x es generoso y
	// barato (las queries siguen siendo indexed).
	overfetch := limit * 2

	// 1. Movies recientes - solo type=movie cross-library. El cap se
	//    aplica en SQL para no traer rows que despues descartamos.
	movies, err := h.lib.LatestItems(r.Context(), "", "movie", overfetch, cap)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	// 2. Series con actividad reciente cross-library. El cap se
	//    aplica post-fetch porque la query ya tiene JOIN con season
	//    + episode.
	seriesRows, err := h.lib.LatestSeriesByActivity(r.Context(), "", overfetch)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if cap != "" {
		filtered := seriesRows[:0]
		for _, row := range seriesRows {
			if library.AllowedRating(row.ContentRating, cap) {
				filtered = append(filtered, row)
			}
		}
		seriesRows = filtered
	}

	// 3. Merge: unimos ambas listas con su "timestamp efectivo"
	//    (added_at para movies, latest_activity_at para series),
	//    ordenamos desc, tomamos top limit. Mantenemos un map
	//    paralelo de new_episodes_count para que el wire la incluya
	//    solo en las entries de tipo serie.
	type merged struct {
		item             *librarymodel.Item
		latestAt         time.Time
		newEpisodesCount int
	}
	all := make([]merged, 0, len(movies)+len(seriesRows))
	for _, m := range movies {
		all = append(all, merged{item: m, latestAt: m.AddedAt})
	}
	for _, s := range seriesRows {
		item := s.Item
		latest := s.LatestActivityAt
		if latest.IsZero() {
			latest = s.AddedAt
		}
		all = append(all, merged{
			item:             &item,
			latestAt:         latest,
			newEpisodesCount: s.NewEpisodesCount,
		})
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].latestAt.After(all[j].latestAt)
	})
	if len(all) > limit {
		all = all[:limit]
	}

	// 4. Enrich (poster, backdrop, overview...) sobre los items
	//    finales en una sola pasada — h.enrichItemSummaries hace
	//    una query JOIN para imagenes + metadatos.
	items := make([]*librarymodel.Item, len(all))
	for i, m := range all {
		items[i] = m.item
	}
	data := h.enrichItemSummaries(r, items)
	// 5. Splice new_episodes_count + latest_activity_at de vuelta a
	//    cada row por posicion (mismo patron que LatestItems lo
	//    hace para el caso series-by-library).
	for i, m := range all {
		if m.newEpisodesCount > 0 {
			data[i]["new_episodes_count"] = m.newEpisodesCount
		}
		if !m.latestAt.IsZero() {
			data[i]["latest_activity_at"] = m.latestAt.UTC().Format(time.RFC3339)
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"items": data,
			"total": len(all),
			"limit": limit,
		},
	})
}

// enrichItemSummaries adds poster_url, backdrop_url, overview, and genres to item summaries.
func (h *LibraryHandler) enrichItemSummaries(r *http.Request, items []*librarymodel.Item) []map[string]any {
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

func libraryResponse(lib *librarymodel.Library) map[string]any {
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

func itemSummaryResponse(item *librarymodel.Item) map[string]any {
	resp := map[string]any{
		"id":         item.ID,
		"library_id": item.LibraryID,
		"type":       item.Type,
		"title":      item.Title,
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
