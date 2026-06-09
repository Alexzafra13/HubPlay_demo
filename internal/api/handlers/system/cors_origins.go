package system

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	"hubplay/internal/db"
)

// CorsOriginStore es la mínima superficie del repo que estos handlers
// necesitan. Aísla de *db.CorsOriginRepository para que tests pasen
// un fake sin DB.
type CorsOriginStore interface {
	List(ctx context.Context) ([]db.CorsOriginRow, error)
	Insert(ctx context.Context, row db.CorsOriginRow) error
	Delete(ctx context.Context, origin string) error
	ListOrigins(ctx context.Context) ([]string, error)
}

// CorsRegistryReloader es la mínima superficie del registry CORS que
// los handlers necesitan para refrescar el snapshot dinámico tras
// añadir/quitar un origen. El handler NO conoce el tipo concreto del
// registry — recibe sólo la función que necesita.
type CorsRegistryReloader interface {
	SetDynamics(origins []string)
	Statics() []string
}

// CorsOriginsHandler hospeda los endpoints del panel admin de CORS:
//
//	GET    /api/v1/admin/cors-origins        — lista (statics + dynamics).
//	POST   /api/v1/admin/cors-origins        — añade un dynamic.
//	DELETE /api/v1/admin/cors-origins        — quita un dynamic.
//
// TODOS owner-only en el router. Modificar CORS = abrir superficie de
// CSRF cross-origin. La decisión la toma quien custodia la instalación.
type CorsOriginsHandler struct {
	store    CorsOriginStore
	registry CorsRegistryReloader
	// validate es inyectado vía constructor para que los tests puedan
	// asumir un validator no-op y centrar el test en el repo + reload.
	// En producción se pasa api.ValidateCorsOrigin.
	validate func(string) (string, error)
	audit    handlers.AuditEmitter
	logger   *slog.Logger
}

func NewCorsOriginsHandler(
	store CorsOriginStore,
	registry CorsRegistryReloader,
	validate func(string) (string, error),
	audit handlers.AuditEmitter,
	logger *slog.Logger,
) *CorsOriginsHandler {
	return &CorsOriginsHandler{
		store:    store,
		registry: registry,
		validate: validate,
		audit:    audit,
		logger:   logger.With("module", "cors-origins-handler"),
	}
}

func (h *CorsOriginsHandler) auditEmit() handlers.AuditEmitter {
	if h.audit != nil {
		return h.audit
	}
	return handlers.NoopAudit{}
}

// List devuelve los orígenes statics (YAML, read-only) + dynamics
// (DB) con metadata. La respuesta es la única lectura que el panel
// hace.
//
// Shape de la respuesta:
//
//	{
//	  "data": {
//	    "statics":  ["https://app.example.com", "http://localhost:5173"],
//	    "dynamics": [
//	      { "origin": "...", "created_by": "u-1", "created_at": "...", "note": "..." }
//	    ]
//	  }
//	}
func (h *CorsOriginsHandler) List(w http.ResponseWriter, r *http.Request) {
	dynamics, err := h.store.List(r.Context())
	if err != nil {
		handlers.RespondError(w, r, http.StatusInternalServerError, "CORS_LIST_FAILED", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(dynamics))
	for _, row := range dynamics {
		out = append(out, map[string]any{
			"origin":     row.Origin,
			"created_by": row.CreatedBy,
			"created_at": row.CreatedAt.Format(time.RFC3339),
			"note":       row.Note,
		})
	}
	handlers.RespondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"statics":  h.registry.Statics(),
			"dynamics": out,
		},
	})
}

// AddRequest mapea el body POST.
type AddCorsOriginRequest struct {
	Origin string `json:"origin"`
	Note   string `json:"note"`
}

// Add inserta un origen dynamic. Validaciones:
//  1. ValidateCorsOrigin pasa (scheme http/https, sin path, etc.).
//  2. El origen ya no es uno de los STATICS (sería redundante y
//     confundiría al operador). Devuelve 409 ALREADY_STATIC.
//  3. INSERT atómico; si la unique key choca, 409 ALREADY_EXISTS.
//
// Tras éxito, recarga el registry y devuelve la lista completa (igual
// que List) para que el frontend repintene sin pedir GET separado.
func (h *CorsOriginsHandler) Add(w http.ResponseWriter, r *http.Request) {
	var req AddCorsOriginRequest
	if err := handlers.DecodeJSON(w, r, &req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "BAD_BODY", err.Error())
		return
	}

	canonical, err := h.validate(req.Origin)
	if err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_ORIGIN", err.Error())
		return
	}

	// Defensa contra duplicar un static via la UI. Si el operador lo
	// intenta, mejor un error claro que un INSERT que SQL tendría que
	// rechazar (no choca con la UNIQUE de cors_origins porque los
	// statics no están en esa tabla).
	for _, s := range h.registry.Statics() {
		if s == canonical {
			handlers.RespondError(w, r, http.StatusConflict, "ALREADY_STATIC",
				"this origin is already defined in the YAML config")
			return
		}
	}

	claims := auth.GetClaims(r.Context())
	createdBy := ""
	if claims != nil {
		createdBy = claims.UserID
	}

	row := db.CorsOriginRow{
		Origin:    canonical,
		CreatedBy: createdBy,
		CreatedAt: time.Now().UTC(),
		Note:      sanitizeNote(req.Note),
	}
	if err := h.store.Insert(r.Context(), row); err != nil {
		if errors.Is(err, db.ErrCorsOriginExists) {
			handlers.RespondError(w, r, http.StatusConflict, "ALREADY_EXISTS",
				"this origin is already in the dynamic list")
			return
		}
		handlers.RespondError(w, r, http.StatusInternalServerError, "INSERT_FAILED", err.Error())
		return
	}

	if err := h.reload(r.Context()); err != nil {
		// El INSERT ya está; el reload falló. La fila persiste y el
		// próximo reload (otro Add, restart, etc.) la pondrá en
		// vigor. Loggeamos pero NO 500 — el operador ve la fila
		// nueva en la lista y entiende que algo va lento.
		h.logger.Warn("cors registry reload failed after insert",
			"origin", canonical, "error", err)
	}

	h.logger.Info("cors origin added",
		"origin", canonical, "created_by", createdBy, "note", row.Note)
	h.auditEmit().LogCorsOriginAdded(r.Context(), r, canonical, row.Note)
	// Devolvemos la lista completa — mismo shape que List, para que
	// el frontend repinte sin un GET extra.
	h.List(w, r)
}

// Delete quita un origen dynamic. Idempotente: borrar un origen que
// no existe devuelve 204 igualmente (consistente con el repo).
// Borrar un static NO está soportado — la respuesta es 409
// IMMUTABLE_STATIC con el origen tal cual lo pasó el cliente.
func (h *CorsOriginsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	// El origin viaja en query string (?origin=...) porque chi no
	// puede meter un URL completo en un path param sin escapado
	// pesado.  Más simple para todos.
	raw := r.URL.Query().Get("origin")
	if raw == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_ORIGIN",
			"origin query parameter is required")
		return
	}
	// Decodificación URL — el cliente DEBE encoded el origin para
	// que el `:` y `/` viajen.
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "BAD_ORIGIN", err.Error())
		return
	}

	// Defensa: borrar un STATIC desde la UI no tiene sentido —
	// devolvemos un mensaje claro en vez de un 204 falso-éxito.
	for _, s := range h.registry.Statics() {
		if s == decoded {
			handlers.RespondError(w, r, http.StatusConflict, "IMMUTABLE_STATIC",
				"this origin is from the YAML config and cannot be removed from the UI")
			return
		}
	}

	if err := h.store.Delete(r.Context(), decoded); err != nil {
		handlers.RespondError(w, r, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}

	if err := h.reload(r.Context()); err != nil {
		// Misma estrategia que en Add — log + sigue.
		h.logger.Warn("cors registry reload failed after delete",
			"origin", decoded, "error", err)
	}

	claims := auth.GetClaims(r.Context())
	deletedBy := ""
	if claims != nil {
		deletedBy = claims.UserID
	}
	h.logger.Info("cors origin removed", "origin", decoded, "by", deletedBy)
	h.auditEmit().LogCorsOriginRemoved(r.Context(), r, decoded)
	w.WriteHeader(http.StatusNoContent)
}

// reload pide al store la lista actual de dynamics y se la pasa al
// registry. Atómico — el snapshot se reemplaza enseguida y los
// preflights siguientes ven el cambio.
func (h *CorsOriginsHandler) reload(ctx context.Context) error {
	origins, err := h.store.ListOrigins(ctx)
	if err != nil {
		return err
	}
	h.registry.SetDynamics(origins)
	return nil
}

// sanitizeNote trunca la nota a un tamaño razonable y la limpia de
// caracteres de control. El operador puede escribir lo que quiera
// pero no inyectar saltos de línea ni payloads que rompan UI.
func sanitizeNote(s string) string {
	const max = 200
	out := make([]rune, 0, len(s))
	for i, r := range s {
		if i >= max {
			break
		}
		// Filtra control chars salvo espacios normales.
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7F {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
