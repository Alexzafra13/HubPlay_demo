package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"hubplay/internal/db"
	"hubplay/internal/provider"

	"github.com/go-chi/chi/v5"
)

// ProviderHandler handles provider management and metadata/image/subtitle lookups.
type ProviderHandler struct {
	manager *provider.Manager
	repo    *db.ProviderRepository
	logger  *slog.Logger
}

// NewProviderHandler creates a new provider handler.
func NewProviderHandler(manager *provider.Manager, repo *db.ProviderRepository, logger *slog.Logger) *ProviderHandler {
	return &ProviderHandler{
		manager: manager,
		repo:    repo,
		logger:  logger.With("module", "provider-handler"),
	}
}

// List returns all registered providers and their status.
func (h *ProviderHandler) List(w http.ResponseWriter, r *http.Request) {
	configs, err := h.repo.ListAll(r.Context())
	if err != nil {
		handleServiceError(w, err)
		return
	}

	result := make([]map[string]any, 0, len(configs))
	for _, c := range configs {
		// Parse config JSON into a map for the frontend
		cfgMap := make(map[string]string)
		if c.ConfigJSON != "" {
			_ = json.Unmarshal([]byte(c.ConfigJSON), &cfgMap)
		}
		result = append(result, map[string]any{
			"name":        c.Name,
			"type":        c.Type,
			"status":      c.Status,
			"priority":    c.Priority,
			"has_api_key": c.APIKey != "",
			"config":      cfgMap,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

type updateProviderRequest struct {
	Status   *string            `json:"status"`
	APIKey   *string            `json:"api_key"`
	Priority *int               `json:"priority"`
	Config   map[string]string  `json:"config"`
}

// Update modifies a provider's configuration (API key, status, priority).
func (h *ProviderHandler) Update(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	var req updateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	cfg, err := h.repo.GetByName(r.Context(), name)
	if err != nil {
		handleServiceError(w, err)
		return
	}
	if cfg == nil {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "provider not found")
		return
	}

	if req.Status != nil {
		cfg.Status = *req.Status
	}
	if req.APIKey != nil {
		cfg.APIKey = *req.APIKey
	}
	if req.Priority != nil {
		cfg.Priority = *req.Priority
	}
	if req.Config != nil {
		// Merge config into existing ConfigJSON
		existing := make(map[string]string)
		if cfg.ConfigJSON != "" {
			_ = json.Unmarshal([]byte(cfg.ConfigJSON), &existing)
		}
		for k, v := range req.Config {
			existing[k] = v
		}
		raw, _ := json.Marshal(existing)
		cfg.ConfigJSON = string(raw)
	}

	if err := h.repo.Upsert(r.Context(), cfg); err != nil {
		handleServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"name":     cfg.Name,
			"status":   cfg.Status,
			"priority": cfg.Priority,
		},
	})
}

// SearchMetadata searches for metadata across all providers.
func (h *ProviderHandler) SearchMetadata(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	if title == "" {
		respondError(w, http.StatusBadRequest, "MISSING_TITLE", "title parameter required")
		return
	}

	query := provider.SearchQuery{
		Title:    title,
		ItemType: provider.ItemType(r.URL.Query().Get("type")),
	}

	results, err := h.manager.SearchMetadata(r.Context(), query)
	if err != nil {
		h.logger.Error("metadata search failed", "error", err)
		respondError(w, http.StatusInternalServerError, "SEARCH_ERROR", "metadata search failed")
		return
	}

	data := make([]map[string]any, 0, len(results))
	for _, r := range results {
		data = append(data, map[string]any{
			"external_id": r.ExternalID,
			"title":       r.Title,
			"year":        r.Year,
			"overview":    r.Overview,
			"score":       r.Score,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": data})
}

// GetMetadata fetches full metadata for a specific external ID.
func (h *ProviderHandler) GetMetadata(w http.ResponseWriter, r *http.Request) {
	externalID := chi.URLParam(r, "externalId")
	itemType := provider.ItemType(r.URL.Query().Get("type"))
	if itemType == "" {
		itemType = provider.ItemMovie
	}

	result, err := h.manager.FetchMetadata(r.Context(), externalID, itemType)
	if err != nil {
		h.logger.Error("metadata fetch failed", "error", err)
		respondError(w, http.StatusNotFound, "NOT_FOUND", "metadata not found")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

// GetImages fetches images for an item by its external IDs.
func (h *ProviderHandler) GetImages(w http.ResponseWriter, r *http.Request) {
	externalIDs := make(map[string]string)
	for _, key := range []string{"tmdb", "imdb", "tvdb"} {
		if v := r.URL.Query().Get(key); v != "" {
			externalIDs[key] = v
		}
	}
	if len(externalIDs) == 0 {
		respondError(w, http.StatusBadRequest, "MISSING_IDS", "at least one external ID required (tmdb, imdb, tvdb)")
		return
	}

	itemType := provider.ItemType(r.URL.Query().Get("type"))
	if itemType == "" {
		itemType = provider.ItemMovie
	}

	images, err := h.manager.FetchImages(r.Context(), externalIDs, itemType)
	if err != nil {
		h.logger.Error("image fetch failed", "error", err)
		respondError(w, http.StatusInternalServerError, "FETCH_ERROR", "image fetch failed")
		return
	}

	data := make([]map[string]any, 0, len(images))
	for _, img := range images {
		data = append(data, map[string]any{
			"url":      img.URL,
			"type":     img.Type,
			"language": img.Language,
			"width":    img.Width,
			"height":   img.Height,
			"score":    img.Score,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": data})
}

// SearchSubtitles searches for subtitles across all providers.
func (h *ProviderHandler) SearchSubtitles(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	if title == "" {
		respondError(w, http.StatusBadRequest, "MISSING_TITLE", "title parameter required")
		return
	}

	query := provider.SubtitleQuery{
		Title:    title,
		ItemType: provider.ItemType(r.URL.Query().Get("type")),
	}
	if langs := r.URL.Query().Get("languages"); langs != "" {
		query.Languages = splitComma(langs)
	}

	// Pass external IDs if available
	query.ExternalIDs = make(map[string]string)
	for _, key := range []string{"imdb", "tmdb"} {
		if v := r.URL.Query().Get(key); v != "" {
			query.ExternalIDs[key] = v
		}
	}

	results, err := h.manager.SearchSubtitles(r.Context(), query)
	if err != nil {
		h.logger.Error("subtitle search failed", "error", err)
		respondError(w, http.StatusInternalServerError, "SEARCH_ERROR", "subtitle search failed")
		return
	}

	data := make([]map[string]any, 0, len(results))
	for _, s := range results {
		data = append(data, map[string]any{
			"language": s.Language,
			"format":   s.Format,
			"url":      s.URL,
			"score":    s.Score,
			"source":   s.Source,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": data})
}

func splitComma(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
