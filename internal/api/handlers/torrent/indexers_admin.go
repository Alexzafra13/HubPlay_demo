package torrenthandler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers"
	"hubplay/internal/torrentstream"
)

// IndexerStoreAPI is the slice of *torrentstream.IndexerStore the admin
// handler needs for CRUD + live status. Kept as an interface for testing.
type IndexerStoreAPI interface {
	Statuses(ctx context.Context) ([]torrentstream.IndexerStatus, error)
	Add(ctx context.Context, in torrentstream.IndexerInput) (torrentstream.IndexerRecord, error)
	Update(ctx context.Context, id string, in torrentstream.IndexerInput) error
	Delete(ctx context.Context, id string) error
}

// Invalidator clears the source cache so an indexer change takes effect on
// the next search. Implemented by *torrentstream.SourceService.
type Invalidator interface {
	Invalidate()
}

// IndexerAdminHandler serves the plug-and-play admin surface for Torznab/
// Prowlarr indexers: list with live status + full CRUD, all persisted in
// the database (no yaml/env edits, no restart). HubPlay never mutates
// Prowlarr's own indexer config — the operator manages that in Prowlarr.
type IndexerAdminHandler struct {
	store       IndexerStoreAPI
	invalidator Invalidator
	logger      *slog.Logger
}

// NewIndexerAdminHandler builds the handler. store may be nil (feature
// unavailable → endpoints return 503). invalidator may be nil.
func NewIndexerAdminHandler(store IndexerStoreAPI, invalidator Invalidator, logger *slog.Logger) *IndexerAdminHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &IndexerAdminHandler{store: store, invalidator: invalidator, logger: logger}
}

// List returns the configured indexers with a live reachability status.
//
// GET /admin/indexers
func (h *IndexerAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "INDEXER_DISABLED",
			"indexer management is not available on this server")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	statuses, err := h.store.Statuses(ctx)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	handlers.RespondData(w, http.StatusOK, statuses)
}

// indexerBody is the create/update payload.
type indexerBody struct {
	Name             string   `json:"name"`
	BaseURL          string   `json:"base_url"`
	URL              string   `json:"url"`
	APIKey           string   `json:"api_key"`
	Enabled          bool     `json:"enabled"`
	MovieCategories  []string `json:"movie_categories"`
	SeriesCategories []string `json:"series_categories"`
	Trackers         []string `json:"trackers"`
}

// toInput validates and converts the body. Returns ("", input) on success
// or (code+message via the bool) — the caller writes the 400.
func (b indexerBody) toInput() (torrentstream.IndexerInput, string) {
	name := strings.TrimSpace(b.Name)
	if name == "" {
		return torrentstream.IndexerInput{}, "name is required"
	}
	if strings.TrimSpace(b.BaseURL) == "" && strings.TrimSpace(b.URL) == "" {
		return torrentstream.IndexerInput{}, "base_url or url is required"
	}
	return torrentstream.IndexerInput{
		Name:             name,
		BaseURL:          strings.TrimSpace(b.BaseURL),
		URL:              strings.TrimSpace(b.URL),
		APIKey:           strings.TrimSpace(b.APIKey),
		Enabled:          b.Enabled,
		MovieCategories:  b.MovieCategories,
		SeriesCategories: b.SeriesCategories,
		Trackers:         b.Trackers,
	}, ""
}

// Create adds an indexer.
//
// POST /admin/indexers
func (h *IndexerAdminHandler) Create(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "INDEXER_DISABLED",
			"indexer management is not available on this server")
		return
	}
	var body indexerBody
	if err := handlers.DecodeJSON(w, r, &body); err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	in, verr := body.toInput()
	if verr != "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_INDEXER", verr)
		return
	}
	rec, err := h.store.Add(r.Context(), in)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	h.invalidate()
	handlers.RespondData(w, http.StatusCreated, rec.Info())
}

// Update replaces an indexer.
//
// PUT /admin/indexers/{id}
func (h *IndexerAdminHandler) Update(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "INDEXER_DISABLED",
			"indexer management is not available on this server")
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_ID", "indexer id required")
		return
	}
	var body indexerBody
	if err := handlers.DecodeJSON(w, r, &body); err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	in, verr := body.toInput()
	if verr != "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_INDEXER", verr)
		return
	}
	if err := h.store.Update(r.Context(), id, in); err != nil {
		handlers.HandleServiceError(w, r, err) // domain.ErrNotFound → 404
		return
	}
	h.invalidate()
	handlers.RespondData(w, http.StatusOK, map[string]bool{"ok": true})
}

// Delete removes an indexer.
//
// DELETE /admin/indexers/{id}
func (h *IndexerAdminHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "INDEXER_DISABLED",
			"indexer management is not available on this server")
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_ID", "indexer id required")
		return
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	h.invalidate()
	w.WriteHeader(http.StatusNoContent)
}

// testIndexerRequest is the body of the connection-test endpoint. Either
// base_url (a Prowlarr root) or url (a full Torznab endpoint) is required.
type testIndexerRequest struct {
	BaseURL string `json:"base_url"`
	URL     string `json:"url"`
	APIKey  string `json:"api_key"`
}

// Test validates a Torznab/Prowlarr connection + API key before the
// operator saves it.
//
// POST /admin/indexers/test
func (h *IndexerAdminHandler) Test(w http.ResponseWriter, r *http.Request) {
	var req testIndexerRequest
	if err := handlers.DecodeJSON(w, r, &req); err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	target := strings.TrimSpace(req.URL)
	if target == "" {
		target = strings.TrimSpace(req.BaseURL)
	}
	if target == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_TARGET",
			"base_url or url is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if err := torrentstream.TestTorznabConnection(ctx, target, req.APIKey); err != nil {
		h.logger.Info("indexer test failed", "target", target, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "INDEXER_TEST_FAILED", err.Error())
		return
	}
	handlers.RespondData(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *IndexerAdminHandler) invalidate() {
	if h.invalidator != nil {
		h.invalidator.Invalidate()
	}
}
