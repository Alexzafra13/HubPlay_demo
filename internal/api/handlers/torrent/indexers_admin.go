package torrenthandler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"hubplay/internal/api/handlers"
	"hubplay/internal/torrentstream"
)

// IndexerAPI is the slice of *torrentstream.TorznabClient the admin handler
// needs. Kept as an interface so tests can stub it.
type IndexerAPI interface {
	Status(ctx context.Context) []torrentstream.IndexerStatus
}

// IndexerAdminHandler serves the admin surface for the configured Torznab
// indexers (typically Prowlarr instances). Listing + connection testing
// only — HubPlay never mutates Prowlarr's own indexer config; the operator
// manages that in Prowlarr itself.
type IndexerAdminHandler struct {
	api    IndexerAPI
	logger *slog.Logger
}

// NewIndexerAdminHandler builds the handler. api may be nil (no indexer
// configured → endpoints return 503).
func NewIndexerAdminHandler(api IndexerAPI, logger *slog.Logger) *IndexerAdminHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &IndexerAdminHandler{api: api, logger: logger}
}

// List returns the configured indexers with a live reachability status.
//
// GET /admin/indexers
func (h *IndexerAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	if h.api == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "INDEXER_DISABLED",
			"no source indexer is configured on this server")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	handlers.RespondData(w, http.StatusOK, h.api.Status(ctx))
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
