package handlers

import (
	"log/slog"
	"net/http"

	"hubplay/internal/provider"

	"github.com/go-chi/chi/v5"
)

// RecommendationsHandler atiende el rail "more like this" del detail
// page. Llama a TMDb vía `ProviderManager.FetchRecommendations` y
// cruza cada candidato contra el catálogo local para marcar
// "in_library" (con un local_id que el frontend usa para deep-link).
//
// Extraído como sub-handler para cerrar parte del olor P. Deps
// disjuntas del resto: lib (item lookup), externalIDs (TMDb id +
// reverse-lookup para in_library), providers (la fuente de las
// recomendaciones), logger.
type RecommendationsHandler struct {
	lib         LibraryService
	externalIDs ExternalIDsRepository
	providers   ProviderManager
	logger      *slog.Logger
}

func newRecommendationsHandler(lib LibraryService, externalIDs ExternalIDsRepository, providers ProviderManager, logger *slog.Logger) *RecommendationsHandler {
	return &RecommendationsHandler{
		lib:         lib,
		externalIDs: externalIDs,
		providers:   providers,
		logger:      logger,
	}
}

// Recommendations devuelve sugerencias "more like this" para un item
// (powered by `/movie/{id}/recommendations` o `/tv/{id}/recommendations`
// de TMDb). Cada candidato se cross-referencia con la library local
// para que el frontend pueda marcar "in library" con deep-link al
// el rail en ambos casos.
func (h *RecommendationsHandler) Recommendations(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		respondError(w, r, http.StatusServiceUnavailable, "RECOMMENDATIONS_DISABLED",
			"no metadata provider is configured")
		return
	}
	id := chi.URLParam(r, "id")
	item, err := h.lib.GetItem(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if item == nil {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "item not found")
		return
	}

	// Pull el external id map per-item y leer el slot tmdb — mismo
	// shape que attachExternalIDs ya usa en la respuesta del detail.
	extIDs, err := h.externalIDs.ListByItem(r.Context(), id)
	if err != nil || len(extIDs) == 0 {
		// Sin external ids = nada que queryarle a TMDb. Rail vacío
		// en lugar de 4xx; el usuario simplemente no ve la sección.
		respondJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{"items": []any{}},
		})
		return
	}
	var tmdbExt string
	for _, e := range extIDs {
		if e.Provider == "tmdb" {
			tmdbExt = e.ExternalID
			break
		}
	}
	if tmdbExt == "" {
		respondJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{"items": []any{}},
		})
		return
	}

	itemType := provider.ItemMovie
	if item.Type == "series" {
		itemType = provider.ItemSeries
	}

	recs, err := h.providers.FetchRecommendations(r.Context(), tmdbExt, itemType, 12)
	if err != nil {
		h.logger.Warn("fetch recommendations", "item_id", id, "error", err)
		// Las recommendations son decorativas — un 502 aquí
		// escondería la página detail entera. Rail vacío es el
		// failure mode correcto.
		respondJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{"items": []any{}},
		})
		return
	}

	// Cross-referenciar cada candidato contra la library para marcar
	// los que el user ya tiene. El reverse-lookup index sobre
	// (provider, external_id) mantiene cada llamada O(log n).
	out := make([]map[string]any, 0, len(recs))
	for _, rec := range recs {
		entry := map[string]any{
			"tmdb_id":    rec.ExternalID,
			"title":      rec.Title,
			"year":       rec.Year,
			"overview":   rec.Overview,
			"poster_url": rec.PosterURL,
		}
		if rec.Rating != nil {
			entry["rating"] = *rec.Rating
		}
		localID, lookupErr := h.externalIDs.GetItemIDByExternalID(r.Context(), "tmdb", rec.ExternalID)
		if lookupErr == nil && localID != "" {
			entry["local_id"] = localID
			entry["in_library"] = true
		} else {
			entry["in_library"] = false
		}
		out = append(out, entry)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"items": out},
	})
}
