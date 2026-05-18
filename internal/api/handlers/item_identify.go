package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"hubplay/internal/domain"
	"hubplay/internal/provider"

	"github.com/go-chi/chi/v5"
)

// MetadataIdentifier orquesta el rematch de un item contra un metadata
// provider. Lo implementa *scanner.Scanner — la interfaz vive aquí para
// que los handlers se puedan testear con un mock sin arrastrar todas las
// deps del scanner real.
type MetadataIdentifier interface {
	SearchCandidates(ctx context.Context, itemID, query string, year int) ([]provider.SearchResult, error)
	IdentifyAndApply(ctx context.Context, itemID, externalID string) error
}

// IdentifyCandidates devuelve la lista de candidatos TMDb para reidentificar
// un item. El cliente puede afinar la búsqueda pasando `?query=` y `?year=`;
// sin esos params, el scanner usa el título y año actuales del item como
// semilla. Sólo películas y series — episodios/temporadas devuelven 400.
//
// GET /items/{id}/identify/candidates?query=...&year=...
// Admin-only (montado bajo RequireAdmin).
func (h *ItemHandler) IdentifyCandidates(w http.ResponseWriter, r *http.Request) {
	if h.identifier == nil {
		respondError(w, r, http.StatusServiceUnavailable, "NO_PROVIDER", "metadata provider not configured")
		return
	}

	id := chi.URLParam(r, "id")
	query := r.URL.Query().Get("query")
	year := 0
	if v := r.URL.Query().Get("year"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			year = parsed
		}
	}

	results, err := h.identifier.SearchCandidates(r.Context(), id, query, year)
	if err != nil {
		// Distinguimos "el item no existe / es del tipo equivocado"
		// (4xx, decidible por el cliente) del fallo de provider (5xx,
		// reintentable). El servicio devuelve domain.ErrNotFound para
		// el primero; el resto se trata como 5xx genérico.
		if errors.Is(err, domain.ErrNotFound) {
			respondAppError(w, r.Context(), domain.NewNotFound("item"))
			return
		}
		h.logger.Warn("identify candidates failed", "id", id, "error", err)
		respondError(w, r, http.StatusBadGateway, "PROVIDER_ERROR", "metadata provider search failed")
		return
	}

	data := make([]map[string]any, 0, len(results))
	for _, c := range results {
		data = append(data, map[string]any{
			"external_id": c.ExternalID,
			"provider":    "tmdb",
			"title":       c.Title,
			"year":        c.Year,
			"overview":    c.Overview,
			"poster_url":  c.PosterURL,
			"score":       c.Score,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": data})
}

type identifyRequest struct {
	// Provider del que viene el ExternalID. Hoy sólo aceptamos "tmdb",
	// pero el campo está aquí desde el día uno para no tener que cambiar
	// el contrato cuando se sume IMDb / TVDb. Vacío equivale a "tmdb".
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
}

// Identify aplica un match concreto sobre el item: descarga la metadata
// completa del externalID elegido por el operador, sobrescribe título,
// overview, géneros, estudio, reparto e imágenes locales. Borra el
// estado anterior antes de aplicar — un rematch manual implica que lo
// que había era incorrecto.
//
// POST /items/{id}/identify
// Body: {"provider": "tmdb", "external_id": "550"}
// Admin-only.
func (h *ItemHandler) Identify(w http.ResponseWriter, r *http.Request) {
	if h.identifier == nil {
		respondError(w, r, http.StatusServiceUnavailable, "NO_PROVIDER", "metadata provider not configured")
		return
	}

	id := chi.URLParam(r, "id")

	var req identifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	if req.ExternalID == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_EXTERNAL_ID", "external_id required")
		return
	}
	if req.Provider != "" && req.Provider != "tmdb" {
		respondError(w, r, http.StatusBadRequest, "UNSUPPORTED_PROVIDER", "only tmdb provider supported")
		return
	}

	if err := h.identifier.IdentifyAndApply(r.Context(), id, req.ExternalID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			respondAppError(w, r.Context(), domain.NewNotFound("item"))
			return
		}
		h.logger.Error("identify apply failed", "id", id, "external_id", req.ExternalID, "error", err)
		respondError(w, r, http.StatusBadGateway, "IDENTIFY_FAILED", "could not apply metadata from provider")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"item_id":     id,
			"provider":    "tmdb",
			"external_id": req.ExternalID,
		},
	})
}
