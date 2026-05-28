package media

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"hubplay/internal/api/handlers"
	"hubplay/internal/domain"
	"hubplay/internal/provider"
)

// ProviderHandler handles provider management and metadata/image/subtitle lookups.
type ProviderHandler struct {
	manager handlers.ProviderManager
	repo    handlers.ProviderRepository
	logger  *slog.Logger
}

// NewProviderHandler creates a new provider handler.
func NewProviderHandler(manager handlers.ProviderManager, repo handlers.ProviderRepository, logger *slog.Logger) *ProviderHandler {
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
		handlers.HandleServiceError(w, r, err)
		return
	}

	result := make([]map[string]any, 0, len(configs))
	for _, c := range configs {
		// Parse config JSON into a map for the frontend. Fail-soft: si el
		// JSON está corrupto el frontend recibe un map vacío (mejor que
		// 500 — el resto de campos del provider siguen siendo útiles).
		// Log permite diagnosticar si alguien edita config_json a mano y
		// rompe el shape.
		cfgMap := make(map[string]string)
		if c.ConfigJSON != "" {
			if err := json.Unmarshal([]byte(c.ConfigJSON), &cfgMap); err != nil {
				h.logger.Warn("provider config json malformed",
					"provider", c.Name, "error", err)
			}
		}
		entry := map[string]any{
			"name":        c.Name,
			"type":        c.Type,
			"status":      c.Status,
			"priority":    c.Priority,
			"has_api_key": c.APIKey != "",
			"config":      cfgMap,
		}
		if c.APIKey != "" {
			entry["api_key_masked"] = maskAPIKey(c.APIKey)
		}
		result = append(result, entry)
	}

	handlers.RespondData(w, http.StatusOK, result)
}

type updateProviderRequest struct {
	Status   *string           `json:"status"`
	APIKey   *string           `json:"api_key"`
	Priority *int              `json:"priority"`
	Config   map[string]string `json:"config"`
}

// Update modifies a provider's configuration (API key, status, priority).
func (h *ProviderHandler) Update(w http.ResponseWriter, r *http.Request) {
	name := handlers.RequireParam(w, r, "name")
	if name == "" {
		return
	}

	var req updateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	cfg, err := h.repo.GetByName(r.Context(), name)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if cfg == nil {
		handlers.RespondAppError(w, r.Context(), domain.NewNotFound("provider"))
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
		handlers.HandleServiceError(w, r, err)
		return
	}

	handlers.RespondJSON(w, http.StatusOK, map[string]any{
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
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_TITLE", "title parameter required")
		return
	}

	query := provider.SearchQuery{
		Title:    title,
		ItemType: provider.ItemType(r.URL.Query().Get("type")),
	}

	results, err := h.manager.SearchMetadata(r.Context(), query)
	if err != nil {
		h.logger.Error("metadata search failed", "error", err)
		handlers.RespondError(w, r, http.StatusInternalServerError, "SEARCH_ERROR", "metadata search failed")
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

	handlers.RespondData(w, http.StatusOK, data)
}

// GetMetadata fetches full metadata for a specific external ID.
func (h *ProviderHandler) GetMetadata(w http.ResponseWriter, r *http.Request) {
	externalID := handlers.RequireParam(w, r, "externalId")
	if externalID == "" {
		return
	}
	itemType := provider.ItemType(r.URL.Query().Get("type"))
	if itemType == "" {
		itemType = provider.ItemMovie
	}

	result, err := h.manager.FetchMetadata(r.Context(), externalID, itemType)
	if err != nil {
		// Warn (no Error) + 502 (no 404): TMDb caído o ID inexistente.
		// Antes devolvía 404 + log Error → operador veía Error sin saber
		// si era provider caído o ID genuinamente faltante. Ahora 502
		// refleja que el upstream falló y Warn evita ruido si la red
		// de TMDb tiembla intermitente.
		h.logger.Warn("metadata fetch failed",
			"external_id", externalID, "type", itemType, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "PROVIDER_UNAVAILABLE",
			"metadata provider failed")
		return
	}

	handlers.RespondData(w, http.StatusOK, result)
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
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_IDS", "at least one external ID required (tmdb, imdb, tvdb)")
		return
	}

	itemType := provider.ItemType(r.URL.Query().Get("type"))
	if itemType == "" {
		itemType = provider.ItemMovie
	}

	images, err := h.manager.FetchImages(r.Context(), externalIDs, itemType)
	if err != nil {
		h.logger.Error("image fetch failed", "error", err)
		handlers.RespondError(w, r, http.StatusInternalServerError, "FETCH_ERROR", "image fetch failed")
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

	handlers.RespondData(w, http.StatusOK, data)
}

// SearchSubtitles searches for subtitles across all providers.
func (h *ProviderHandler) SearchSubtitles(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	if title == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_TITLE", "title parameter required")
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
		handlers.RespondError(w, r, http.StatusInternalServerError, "SEARCH_ERROR", "subtitle search failed")
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

	handlers.RespondData(w, http.StatusOK, data)
}

// maskAPIKey returns a masked version of an API key for display.
// Shows the first 4 and last 4 characters with asterisks in between.
func maskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
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
