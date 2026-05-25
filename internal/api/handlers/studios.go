package handlers

import (
	"context"
	"log/slog"
	"net/http"

	librarymodel "hubplay/internal/library/model"
)

// StudioRepository is the slice of db.StudioRepository the handler
// needs. Inverted-dependency interface so the handler is trivially
// fakeable in tests.
type StudioRepository interface {
	GetBySlug(ctx context.Context, slug string) (*librarymodel.Studio, error)
	List(ctx context.Context) ([]*librarymodel.StudioListEntry, error)
	ListItemsForStudio(ctx context.Context, studioID string) ([]*librarymodel.StudioItem, error)
}

// StudioHandler serves the /studios browse + /studios/{slug} detail
// endpoints. The detail endpoint is the data source for the
// "click the studio mark on a movie/series detail page → see the
// rest of the catalogue from this studio" flow.
type StudioHandler struct {
	studios StudioRepository
	logger  *slog.Logger
}

func NewStudioHandler(studios StudioRepository, logger *slog.Logger) *StudioHandler {
	return &StudioHandler{studios: studios, logger: logger}
}

// List returns every studio that has at least one item linked to it.
//
//	GET /api/v1/studios
//	{ "data": { "studios": [ {id,name,slug,logo_url,item_count}, ... ] } }
//
// Sorted by item_count desc on the way out (handled SQL-side) so the
// browse grid renders with the headline studios on top.
func (h *StudioHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.studios.List(r.Context())
	if err != nil {
		h.logger.Error("list studios", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "list studios failed")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"id":         row.ID,
			"name":       row.Name,
			"slug":       row.Slug,
			"item_count": row.ItemCount,
		}
		if row.LogoURL != "" {
			entry["logo_url"] = row.LogoURL
		}
		out = append(out, entry)
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"studios": out},
	})
}

// Get returns a studio's metadata + the items linked to it.
//
//	GET /api/v1/studios/{slug}
//	{ "data": {
//	    "id": "...", "name": "Marvel Studios", "slug": "marvel-studios",
//	    "logo_url": "https://image.tmdb.org/t/p/w300/...",
//	    "items": [ {id,type,title,year,poster_url}, ... ]
//	} }
//
// 404 when the slug doesn't match a studio. Empty `items` is a valid
// response — the browse listing uses EXISTS to avoid empty rows, but
// a direct hit on a slug that no longer has items should still return
// the studio header (so the user lands on a coherent page if they
// share the URL).
func (h *StudioHandler) Get(w http.ResponseWriter, r *http.Request) {
	slug := requireParam(w, r, "slug")
	if slug == "" {
		return
	}
	studio, err := h.studios.GetBySlug(r.Context(), slug)
	if err != nil {
		h.logger.Error("get studio", "slug", slug, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "lookup failed")
		return
	}
	if studio == nil {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "studio not found")
		return
	}
	items, err := h.studios.ListItemsForStudio(r.Context(), studio.ID)
	if err != nil {
		h.logger.Error("list studio items", "slug", slug, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "list items failed")
		return
	}

	resp := map[string]any{
		"id":   studio.ID,
		"name": studio.Name,
		"slug": studio.Slug,
	}
	if studio.LogoURL != "" {
		resp["logo_url"] = studio.LogoURL
	}
	if studio.TMDBID != nil {
		resp["tmdb_id"] = *studio.TMDBID
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

	respondData(w, http.StatusOK, resp)
}
