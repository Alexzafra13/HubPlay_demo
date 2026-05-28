package media

import (
	"context"
	"log/slog"
	"net/http"

	"hubplay/internal/api/handlers"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/provider"
)

// itemGetter es el contrato mínimo que RecommendationsHandler necesita:
// buscar un item por ID para resolver su tipo (movie/series) antes de
// consultar TMDb. Cierra NN para este consumer.
type itemGetter interface {
	GetItem(ctx context.Context, id string) (*librarymodel.Item, error)
}

// RecommendationsHandler atiende el rail "more like this" del detail
// page. Llama a TMDb vía `handlers.ProviderManager.FetchRecommendations` y
// cruza cada candidato contra el catálogo local para marcar
// "in_library" (con un local_id que el frontend usa para deep-link).
type RecommendationsHandler struct {
	lib         itemGetter
	externalIDs handlers.ExternalIDsRepository
	providers   handlers.ProviderManager
	logger      *slog.Logger
}

func newRecommendationsHandler(lib itemGetter, externalIDs handlers.ExternalIDsRepository, providers handlers.ProviderManager, logger *slog.Logger) *RecommendationsHandler {
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
// item local, mientras genuinamente-nuevos surface como posters
// externos.
//
// Lista vacía es respuesta válida (item sin match TMDb, o TMDb sin
// recomendaciones). 503 cuando no hay provider — el frontend oculta
// el rail en ambos casos.
func (h *RecommendationsHandler) Recommendations(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "RECOMMENDATIONS_DISABLED",
			"no metadata provider is configured")
		return
	}
	id := handlers.RequireParam(w, r, "id")
	if id == "" {
		return
	}
	item, err := h.lib.GetItem(r.Context(), id)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if item == nil {
		handlers.RespondError(w, r, http.StatusNotFound, "NOT_FOUND", "item not found")
		return
	}

	// Pull el external id map per-item y leer el slot tmdb — mismo
	// shape que attachExternalIDs ya usa en la respuesta del detail.
	extIDs, err := h.externalIDs.ListByItem(r.Context(), id)
	if err != nil || len(extIDs) == 0 {
		// Sin external ids = nada que queryarle a TMDb. Rail vacío
		// en lugar de 4xx; el usuario simplemente no ve la sección.
		handlers.RespondJSON(w, http.StatusOK, map[string]any{
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
		handlers.RespondJSON(w, http.StatusOK, map[string]any{
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
		handlers.RespondJSON(w, http.StatusOK, map[string]any{
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

	handlers.RespondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"items": out},
	})
}
