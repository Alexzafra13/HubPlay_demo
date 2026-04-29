package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/db"
	"hubplay/internal/domain"
)

// PeopleRepository is the subset of db.PeopleRepository the handler
// needs. Defined here to keep the dependency arrow pointing inward
// and to make the handler trivially fakeable from tests.
type PeopleRepository interface {
	GetByID(ctx context.Context, id string) (*db.Person, error)
	ListFilmographyByPerson(ctx context.Context, personID string) ([]*db.FilmographyEntry, error)
}

// PeopleHandler serves cast/crew profile photos. The thumb file
// itself lives at the absolute path stored in `people.thumb_path`
// (under <imageDir>/.people/<id>/...); we validate it sits inside
// imageDir before serving to defend against a poisoned DB row.
type PeopleHandler struct {
	people   PeopleRepository
	imageDir string
	logger   *slog.Logger
}

func NewPeopleHandler(people PeopleRepository, imageDir string, logger *slog.Logger) *PeopleHandler {
	return &PeopleHandler{people: people, imageDir: imageDir, logger: logger}
}

// Thumb serves the profile photo for a person. 404 when the row has
// no thumb_path (provider didn't supply one, or download failed) so
// the client can fall back to its initial-letter placeholder via
// onError.
func (h *PeopleHandler) Thumb(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	person, err := h.people.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			respondError(w, r, http.StatusNotFound, "NOT_FOUND", "person not found")
			return
		}
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "lookup failed")
		return
	}
	if person.ThumbPath == "" {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "person has no thumb")
		return
	}
	if !h.isUnderImageDir(person.ThumbPath) {
		h.logger.Warn("person thumb_path escapes imageDir — refusing to serve", "id", id)
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "person thumb missing")
		return
	}
	if _, err := os.Stat(person.ThumbPath); err != nil {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "person thumb missing")
		return
	}

	// Same caching policy as /api/v1/images/file/{id}: profiles rarely
	// change, the URL is content-stable per person id.
	w.Header().Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")
	http.ServeFile(w, r, person.ThumbPath)
}

// Get returns the person row + their filmography. Shape:
//
//	{
//	  "data": {
//	    "id": "...",
//	    "name": "Tom Hanks",
//	    "type": "actor",
//	    "image_url": "/api/v1/people/{id}/thumb",   // omitted if no thumb
//	    "filmography": [
//	      {"item_id":"...","type":"movie","title":"Forrest Gump","year":1994,
//	       "role":"actor","character":"Forrest Gump"},
//	      ...
//	    ]
//	  }
//	}
//
// Filmography is already deduped by item_id at the repo layer (one
// title per row even if the person has multiple credits on it). The
// `image_url` field is built only when a thumb is on disk; the
// frontend falls back to an initial-letter placeholder via the same
// path it uses on cast strips.
func (h *PeopleHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	person, err := h.people.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			respondError(w, r, http.StatusNotFound, "NOT_FOUND", "person not found")
			return
		}
		h.logger.Error("get person", "id", id, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "lookup failed")
		return
	}

	credits, err := h.people.ListFilmographyByPerson(r.Context(), id)
	if err != nil {
		h.logger.Error("list filmography", "id", id, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "filmography lookup failed")
		return
	}

	resp := map[string]any{
		"id":   person.ID,
		"name": person.Name,
		"type": person.Type,
	}
	// Only surface image_url when a thumb is on disk under imageDir.
	// The Thumb endpoint validates the path before serving anyway, but
	// emitting the URL here when no file exists would force the client
	// to round-trip just to learn there's no image — costs an extra
	// request per cast list with absent thumbs.
	if person.ThumbPath != "" && h.isUnderImageDir(person.ThumbPath) {
		if _, statErr := os.Stat(person.ThumbPath); statErr == nil {
			resp["image_url"] = "/api/v1/people/" + person.ID + "/thumb"
		}
	}

	entries := make([]map[string]any, 0, len(credits))
	for _, c := range credits {
		entry := map[string]any{
			"item_id":   c.ItemID,
			"type":      c.Type,
			"title":     c.Title,
			"role":      c.Role,
			"sort_order": c.SortOrder,
		}
		if c.Year > 0 {
			entry["year"] = c.Year
		}
		if c.CharacterName != "" {
			entry["character"] = c.CharacterName
		}
		entries = append(entries, entry)
	}
	resp["filmography"] = entries

	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *PeopleHandler) isUnderImageDir(p string) bool {
	rootAbs, err := filepath.Abs(h.imageDir)
	if err != nil {
		return false
	}
	pAbs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pAbs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
