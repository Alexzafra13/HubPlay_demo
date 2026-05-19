package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"hubplay/internal/updates"
)

// UpdatesProvider es la mínima superficie del service que este handler
// necesita. Tener una interface aísla el handler del *updates.Service
// concreto y permite que los tests pasen un fake sin spinnear goroutine.
type UpdatesProvider interface {
	Status() updates.Status
	Check(ctx context.Context) error
}

// UpdatesHandler expone dos endpoints en /api/v1/system/updates:
//
//	GET    /admin/system/updates           → estado cacheado, lectura barata
//	POST   /admin/system/updates/check     → fuerza un check inmediato
//
// Ambos admin-only (el router los gateaa con auth.RequireAdmin).
// El check forzado está rate-limited a 1/min para que un click frenético
// del operador no spamee la API de GitHub.
type UpdatesHandler struct {
	svc     UpdatesProvider
	logger  *slog.Logger
	lastMan time.Time // último check manual (rate-limit)
}

func NewUpdatesHandler(svc UpdatesProvider, logger *slog.Logger) *UpdatesHandler {
	return &UpdatesHandler{
		svc:    svc,
		logger: logger.With("module", "updates-handler"),
	}
}

// Status devuelve el snapshot cacheado. Nunca hace IO.
func (h *UpdatesHandler) Status(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"data": h.svc.Status(),
	})
}

// Check fuerza una comprobación inmediata. Rate-limited a 1/minuto.
func (h *UpdatesHandler) Check(w http.ResponseWriter, r *http.Request) {
	// Rate-limit simple: si han pasado <60s desde el último check
	// manual, devuelve 429 con el snapshot actual (mejor UX que un
	// error opaco — el operador ve "sigue siendo la misma versión").
	if !h.lastMan.IsZero() && time.Since(h.lastMan) < time.Minute {
		respondError(w, r, http.StatusTooManyRequests, "RATE_LIMITED",
			"comprobación manual rate-limited a 1/minuto")
		return
	}
	h.lastMan = time.Now()

	if err := h.svc.Check(r.Context()); err != nil {
		// Devolvemos 200 con el snapshot (que ya tiene LastError seteado)
		// en lugar de 5xx — el frontend pinta el banner de "no se pudo
		// comprobar" sin tratar el endpoint como roto.
		respondJSON(w, http.StatusOK, map[string]any{
			"data": h.svc.Status(),
		})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": h.svc.Status(),
	})
}
