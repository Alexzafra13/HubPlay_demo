// EPG source management endpoints.
//
//   GET    /api/v1/iptv/epg-catalog                          (auth user)
//   GET    /api/v1/libraries/{id}/epg-sources                (auth + ACL)
//   POST   /api/v1/libraries/{id}/epg-sources                (admin)
//   DELETE /api/v1/libraries/{id}/epg-sources/{sourceId}     (admin)
//   PATCH  /api/v1/libraries/{id}/epg-sources/reorder        (admin)
//
// The catalog endpoint is intentionally viewer-accessible: the shape
// is public data (provider names + URLs) and exposing it to the
// frontend keeps the admin dropdown code identical across roles.

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/db"
)

// EPGCatalog returns the curated EPG provider list.
func (h *IPTVHandler) EPGCatalog(w http.ResponseWriter, r *http.Request) {
	catalog := h.svc.PublicEPGCatalog()
	out := make([]map[string]any, 0, len(catalog))
	for _, src := range catalog {
		out = append(out, map[string]any{
			"id":          src.ID,
			"name":        src.Name,
			"description": src.Description,
			"language":    src.Language,
			"countries":   src.Countries,
			"url":         src.URL,
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// ListEPGSources returns the EPG providers attached to a library.
// Gated by the library ACL — the EPG source list leaks URL info we'd
// rather keep library-private.
func (h *IPTVHandler) ListEPGSources(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}
	sources, err := h.svc.ListEPGSources(r.Context(), libraryID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": epgSourcesToJSON(sources)})
}

type addEPGSourceRequest struct {
	CatalogID string `json:"catalog_id"`
	URL       string `json:"url"`
}

// AddEPGSource attaches a new provider. Admin-only at the route level.
func (h *IPTVHandler) AddEPGSource(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}
	var body addEPGSourceRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}
	src, err := h.svc.AddEPGSource(r.Context(), libraryID, body.CatalogID, body.URL)
	if err != nil {
		// Duplicate URL is the expected failure mode when the admin
		// re-adds a source (or the catalog entry for a URL they'd
		// already pasted custom). Map to 409 + clean message so the
		// UI can render "ya añadida" instead of a raw SQL error.
		if errors.Is(err, db.ErrEPGSourceAlreadyAttached) {
			respondError(w, r, http.StatusConflict, "ALREADY_ATTACHED",
				"esa fuente EPG ya está añadida a esta biblioteca")
			return
		}
		// Other errors from AddEPGSource are shape problems (unknown
		// catalog id, missing fields) — surface them as 400.
		if _, ok := err.(interface{ Kind() string }); !ok {
			respondError(w, r, http.StatusBadRequest, "INVALID_SOURCE", err.Error())
			return
		}
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"data": epgSourceToJSON(src)})
}

// RemoveEPGSource deletes one provider from the library.
func (h *IPTVHandler) RemoveEPGSource(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	sourceID := chi.URLParam(r, "sourceId")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}
	if err := h.svc.RemoveEPGSource(r.Context(), libraryID, sourceID); err != nil {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type reorderEPGSourcesRequest struct {
	SourceIDs []string `json:"source_ids"`
}

// ReorderEPGSources rewrites every source's priority. Body is the
// full ordered id list.
func (h *IPTVHandler) ReorderEPGSources(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}
	var body reorderEPGSourcesRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}
	if err := h.svc.ReorderEPGSources(r.Context(), libraryID, body.SourceIDs); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_ORDER", err.Error())
		return
	}
	sources, err := h.svc.ListEPGSources(r.Context(), libraryID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": epgSourcesToJSON(sources)})
}

func epgSourcesToJSON(sources []*db.LibraryEPGSource) []map[string]any {
	out := make([]map[string]any, 0, len(sources))
	for _, s := range sources {
		out = append(out, epgSourceToJSON(s))
	}
	return out
}

func epgSourceToJSON(s *db.LibraryEPGSource) map[string]any {
	var lastRefreshed any
	if !s.LastRefreshedAt.IsZero() {
		lastRefreshed = s.LastRefreshedAt
	}
	return map[string]any{
		"id":                 s.ID,
		"library_id":         s.LibraryID,
		"catalog_id":         s.CatalogID,
		"url":                s.URL,
		"priority":           s.Priority,
		"last_refreshed_at":  lastRefreshed,
		"last_status":        s.LastStatus,
		"last_error":         s.LastError,
		"last_program_count": s.LastProgramCount,
		"last_channel_count": s.LastChannelCount,
		"created_at":         s.CreatedAt,
	}
}
