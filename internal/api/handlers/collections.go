package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	librarymodel "hubplay/internal/library/model"
)

// CollectionRepository is the slice of db.CollectionRepository the
// handler needs.
type CollectionRepository interface {
	GetByID(ctx context.Context, id string) (*librarymodel.Collection, error)
	List(ctx context.Context) ([]*librarymodel.CollectionListEntry, error)
	ListItemsForCollection(ctx context.Context, collectionID string) ([]*librarymodel.CollectionItem, error)
}

// CollectionHandler serves /collections (browse) and /collections/{id}
// (detail). Powers the Jellyfin-style "Movie Collections" surface
// where saga members (X-Men, MCU, Toy Story) cluster under one page.
type CollectionHandler struct {
	collections CollectionRepository
	logger      *slog.Logger
}

func NewCollectionHandler(collections CollectionRepository, logger *slog.Logger) *CollectionHandler {
	return &CollectionHandler{collections: collections, logger: logger}
}

// List returns every collection with at least one member movie in the
// catalogue, sorted by member count desc.
//
//	GET /api/v1/collections
//	{ "data": { "collections": [ {id,name,poster_url,backdrop_url,item_count}, ... ] } }
func (h *CollectionHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.collections.List(r.Context())
	if err != nil {
		h.logger.Error("list collections", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "list collections failed")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"id":         row.ID,
			"name":       row.Name,
			"item_count": row.ItemCount,
		}
		if row.PosterURL != "" {
			entry["poster_url"] = row.PosterURL
		}
		if row.BackdropURL != "" {
			entry["backdrop_url"] = row.BackdropURL
		}
		out = append(out, entry)
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"collections": out},
	})
}

// Get returns a collection's metadata + member movies in release
// order. 404 when the id doesn't match — the handler accepts the
// stable "collection:<tmdb_id>" id directly so the frontend never
// has to slug-encode it.
//
//	GET /api/v1/collections/{id}
//	{ "data": {
//	    "id": "collection:86311", "tmdb_id": 86311,
//	    "name": "Marvel Cinematic Universe",
//	    "overview": "...", "poster_url": "...", "backdrop_url": "...",
//	    "items": [ {id,type,title,year,poster_url}, ... ]
//	} }
func (h *CollectionHandler) Get(w http.ResponseWriter, r *http.Request) {
	// chi v5 returns URL parameters in their raw, percent-encoded form
	// (it matches against r.URL.RawPath when set). Collection IDs are
	// "collection:<tmdb_id>" so the frontend's encodeURIComponent
	// turns the colon into "%3A" before navigation, and that escaped
	// form is what lands here. Decode it before the DB lookup or the
	// query searches for the literal "%3A" string and 404s every saga
	// the home rail just listed. PathUnescape returning an error is
	// theoretically impossible for a value that already came out of a
	// validly-routed request, but we fall back to the raw value so a
	// future malformed input surfaces as 404 from the lookup rather
	// than crashing the handler.
	rawID := chi.URLParam(r, "id")
	id := rawID
	if decoded, err := url.PathUnescape(rawID); err == nil {
		id = decoded
	}
	col, err := h.collections.GetByID(r.Context(), id)
	if err != nil {
		h.logger.Error("get collection", "id", id, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "lookup failed")
		return
	}
	if col == nil {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "collection not found")
		return
	}
	items, err := h.collections.ListItemsForCollection(r.Context(), col.ID)
	if err != nil {
		h.logger.Error("list collection items", "id", id, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "list items failed")
		return
	}

	resp := map[string]any{
		"id":      col.ID,
		"tmdb_id": col.TMDBID,
		"name":    col.Name,
	}
	if col.Overview != "" {
		resp["overview"] = col.Overview
	}
	if col.PosterURL != "" {
		resp["poster_url"] = col.PosterURL
	}
	if col.BackdropURL != "" {
		resp["backdrop_url"] = col.BackdropURL
	}

	entries := make([]map[string]any, 0, len(items))
	for _, it := range items {
		entry := map[string]any{
			"id":    it.ID,
			"type":  it.Type,
			"title": it.Title,
		}
		if it.Year > 0 {
			entry["year"] = it.Year
		}
		if it.PrimaryImageID != "" {
			entry["poster_url"] = "/api/v1/images/file/" + it.PrimaryImageID
		}
		entries = append(entries, entry)
	}
	resp["items"] = entries

	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
}
