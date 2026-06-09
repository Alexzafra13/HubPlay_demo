package media

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"hubplay/internal/api/handlers"
	"hubplay/internal/domain"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/provider"
	"hubplay/internal/scanner"
)

// MetadataIdentifier orquesta el rematch de un item contra un metadata
// provider y el editor manual de metadatos. Lo implementa
// *scanner.Scanner — la interfaz vive aquí para que los handlers se
// puedan testear con un mock sin arrastrar todas las deps del scanner
// real.
type MetadataIdentifier interface {
	SearchCandidates(ctx context.Context, itemID, query string, year int) ([]provider.SearchResult, error)
	IdentifyAndApply(ctx context.Context, itemID, externalID string) error
	UpdateItemMetadata(ctx context.Context, itemID string, patch scanner.ItemMetadataPatch) (*librarymodel.Item, error)
	SetMetadataLock(ctx context.Context, itemID string, locked bool) error
	IsMetadataLocked(ctx context.Context, itemID string) (bool, error)
	RefreshItemMetadata(ctx context.Context, itemID string) error
}

// MetadataHandler aísla las rutas admin-only de gestión de metadata:
// identificación TMDb manual + editor de campos + lock + refresh por
// item. Cierra parte del olor P del audit 2026-05-14 — el campo
// `identifier` que vivía en ItemHandler ahora es propio de este sub-
// handler y se promueve via embedding al facade (buildItemDetail sigue
// leyendo `h.identifier` sin cambios porque field promotion intra-
// paquete funciona también para campos no exportados).
type MetadataHandler struct {
	identifier MetadataIdentifier
	audit      handlers.AuditEmitter
	logger     *slog.Logger
}

func newMetadataHandler(identifier MetadataIdentifier, audit handlers.AuditEmitter, logger *slog.Logger) *MetadataHandler {
	return &MetadataHandler{
		identifier: identifier,
		audit:      audit,
		logger:     logger,
	}
}

// auditEmit devuelve el sink real o un noop. Mismo patrón duplicado
// en AuthHandler / ItemHandler — no es scope de esta PR consolidarlo.
func (h *MetadataHandler) auditEmit() handlers.AuditEmitter {
	if h.audit != nil {
		return h.audit
	}
	return handlers.NoopAudit{}
}

// IdentifyCandidates devuelve la lista de candidatos TMDb para reidentificar
// un item. El cliente puede afinar la búsqueda pasando `?query=` y `?year=`;
// sin esos params, el scanner usa el título y año actuales del item como
// semilla. Sólo películas y series — episodios/temporadas devuelven 400.
//
// GET /items/{id}/identify/candidates?query=...&year=...
// Admin-only (montado bajo RequireAdmin).
func (h *MetadataHandler) IdentifyCandidates(w http.ResponseWriter, r *http.Request) {
	if h.identifier == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "NO_PROVIDER", "metadata provider not configured")
		return
	}

	id := handlers.RequireParam(w, r, "id")
	if id == "" {
		return
	}
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
			handlers.RespondAppError(w, r.Context(), domain.NewNotFound("item"))
			return
		}
		h.logger.Warn("identify candidates failed", "id", id, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "PROVIDER_ERROR", "metadata provider search failed")
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

	handlers.RespondData(w, http.StatusOK, data)
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
func (h *MetadataHandler) Identify(w http.ResponseWriter, r *http.Request) {
	if h.identifier == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "NO_PROVIDER", "metadata provider not configured")
		return
	}

	id := handlers.RequireParam(w, r, "id")
	if id == "" {
		return
	}

	var req identifyRequest
	if err := handlers.DecodeJSON(w, r, &req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	if req.ExternalID == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_EXTERNAL_ID", "external_id required")
		return
	}
	if req.Provider != "" && req.Provider != "tmdb" {
		handlers.RespondError(w, r, http.StatusBadRequest, "UNSUPPORTED_PROVIDER", "only tmdb provider supported")
		return
	}

	if err := h.identifier.IdentifyAndApply(r.Context(), id, req.ExternalID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			handlers.RespondAppError(w, r.Context(), domain.NewNotFound("item"))
			return
		}
		h.logger.Error("identify apply failed", "id", id, "external_id", req.ExternalID, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "IDENTIFY_FAILED", "could not apply metadata from provider")
		return
	}
	h.auditEmit().LogMetadataEdited(r.Context(), r, id, "identify_tmdb")

	handlers.RespondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"item_id":     id,
			"provider":    "tmdb",
			"external_id": req.ExternalID,
		},
	})
}

// patchMetadataRequest es el body del PATCH /items/{id}/metadata. Cada
// campo es opcional — un nil deja el campo del item inalterado.
// Cadenas vacías cuentan como "borrar" (e.g. overview="" elimina el
// overview previo, no es lo mismo que no enviar el campo).
type patchMetadataRequest struct {
	Title         *string `json:"title,omitempty"`
	OriginalTitle *string `json:"original_title,omitempty"`
	Year          *int    `json:"year,omitempty"`
	Overview      *string `json:"overview,omitempty"`
	Tagline       *string `json:"tagline,omitempty"`
}

// UpdateItemMetadata aplica una edición manual de metadatos sobre un
// item: actualiza los campos suministrados en items y/o metadata, y
// bloquea el item para que el siguiente refresh del scanner no pise
// el trabajo del operador.
//
// PATCH /items/{id}/metadata
// Body: campos opcionales (title, original_title, year, overview, tagline).
// Admin-only.
func (h *MetadataHandler) UpdateItemMetadata(w http.ResponseWriter, r *http.Request) {
	if h.identifier == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "NO_EDITOR", "metadata editor not configured")
		return
	}
	id := handlers.RequireParam(w, r, "id")
	if id == "" {
		return
	}

	var req patchMetadataRequest
	if err := handlers.DecodeJSON(w, r, &req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	if req.Title == nil && req.OriginalTitle == nil && req.Year == nil && req.Overview == nil && req.Tagline == nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "EMPTY_PATCH", "at least one field required")
		return
	}

	patch := scanner.ItemMetadataPatch{
		Title:         req.Title,
		OriginalTitle: req.OriginalTitle,
		Year:          req.Year,
		Overview:      req.Overview,
		Tagline:       req.Tagline,
	}
	item, err := h.identifier.UpdateItemMetadata(r.Context(), id, patch)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			handlers.RespondAppError(w, r.Context(), domain.NewNotFound("item"))
			return
		}
		h.logger.Error("update item metadata failed", "id", id, "error", err)
		handlers.RespondError(w, r, http.StatusInternalServerError, "UPDATE_FAILED", "could not apply metadata patch")
		return
	}
	h.auditEmit().LogMetadataEdited(r.Context(), r, id, "manual")

	handlers.RespondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"item_id":         item.ID,
			"title":           item.Title,
			"original_title":  item.OriginalTitle,
			"year":            item.Year,
			"metadata_locked": true,
		},
	})
}

type setMetadataLockRequest struct {
	Locked bool `json:"locked"`
}

// SetMetadataLock cambia el flag de lock sin tocar el contenido del
// item — el operador lo usa para "soltar" un item identificado a mano
// para que vuelva a refrescarse automáticamente, o para bloquear un
// item auto-importado sin modificarlo.
//
// PUT /items/{id}/metadata/lock
// Body: {"locked": true|false}
// Admin-only.
func (h *MetadataHandler) SetMetadataLock(w http.ResponseWriter, r *http.Request) {
	if h.identifier == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "NO_EDITOR", "metadata editor not configured")
		return
	}
	id := handlers.RequireParam(w, r, "id")
	if id == "" {
		return
	}
	var req setMetadataLockRequest
	if err := handlers.DecodeJSON(w, r, &req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	if err := h.identifier.SetMetadataLock(r.Context(), id, req.Locked); err != nil {
		h.logger.Error("set metadata lock failed", "id", id, "error", err)
		handlers.RespondError(w, r, http.StatusInternalServerError, "LOCK_FAILED", "could not toggle metadata lock")
		return
	}
	handlers.RespondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"item_id": id, "metadata_locked": req.Locked},
	})
}

// RefreshItemMetadata re-corre el enrich del scanner sobre un item
// concreto. A diferencia del refresh-metadata global del admin
// (POST /libraries/{id}/refresh-metadata) que recorre toda la
// biblioteca, éste vive en el kebab del item y trabaja sobre uno solo.
//
// POST /items/{id}/refresh-metadata
// Admin-only.
func (h *MetadataHandler) RefreshItemMetadata(w http.ResponseWriter, r *http.Request) {
	if h.identifier == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "NO_REFRESH", "metadata refresh not configured")
		return
	}
	id := handlers.RequireParam(w, r, "id")
	if id == "" {
		return
	}
	if err := h.identifier.RefreshItemMetadata(r.Context(), id); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			handlers.RespondAppError(w, r.Context(), domain.NewNotFound("item"))
			return
		}
		h.logger.Error("refresh item metadata failed", "id", id, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "REFRESH_FAILED", "could not refresh metadata")
		return
	}
	h.auditEmit().LogMetadataEdited(r.Context(), r, id, "refresh")
	handlers.RespondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"item_id": id, "refreshed": true},
	})
}
