package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/updates"
)

// settingKeyUpdatesEnabled es la key persistida en app_settings que
// representa el toggle runtime del checker. "true"/"false". Vive aquí
// (no en settings.go) porque la edición no pasa por el SettingsHandler
// genérico — se hace desde el panel de updates, al lado del banner.
const settingKeyUpdatesEnabled = "updates.check_enabled"

// UpdatesProvider es la mínima superficie del service que este handler
// necesita. Tener una interface aísla el handler del *updates.Service
// concreto y permite que los tests pasen un fake sin spinnear goroutine.
type UpdatesProvider interface {
	Status() updates.Status
	Check(ctx context.Context) error
	SetUserEnabled(enabled bool)
	IsUserEnabled() bool
}

// UpdatesHandler expone los endpoints en /api/v1/admin/system/updates:
//
//	GET    /admin/system/updates           → estado cacheado, lectura barata
//	POST   /admin/system/updates/check     → fuerza un check inmediato
//	GET    /admin/system/updates/config    → estado del toggle del admin
//	PUT    /admin/system/updates/config    → cambia el toggle del admin
//
// Todos admin-only (el router los gateaa con auth.RequireAdmin).
// El check forzado está rate-limited a 1/min para que un click frenético
// del operador no spamee la API de GitHub.
type UpdatesHandler struct {
	svc      UpdatesProvider
	settings *db.SettingsRepository
	logger   *slog.Logger
	lastMan  time.Time // último check manual (rate-limit)
}

// NewUpdatesHandler construye el handler. settings puede ser nil — en
// ese caso los endpoints /config devuelven 503 (la persistencia del
// toggle requiere DB). El check de Status/Check sigue funcionando.
func NewUpdatesHandler(svc UpdatesProvider, settings *db.SettingsRepository, logger *slog.Logger) *UpdatesHandler {
	return &UpdatesHandler{
		svc:      svc,
		settings: settings,
		logger:   logger.With("module", "updates-handler"),
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

// updatesConfigResponse es el shape del GET/PUT /config. Mantengo "data"
// envelope para casar con el resto de admin endpoints.
type updatesConfigResponse struct {
	Enabled bool `json:"enabled"`
}

// GetConfig devuelve el estado actual del toggle runtime del admin.
// No lee de la DB en caliente — el bootstrap ya cargó el setting al
// arrancar y lo aplicó al Service; aquí leemos del Service directamente.
func (h *UpdatesHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"data": updatesConfigResponse{Enabled: h.svc.IsUserEnabled()},
	})
}

// UpdateConfig persiste el toggle y lo propaga al Service. Body:
//
//	{"enabled": true|false}
//
// Cuando enabled=false el ticker sigue armado pero cada tick es no-op.
// Cuando enabled=true el ticker vuelve a chequear al siguiente disparo
// (no fuerza un check inmediato — el admin tiene el botón "Comprobar
// ahora" justo al lado si lo quiere).
func (h *UpdatesHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	if h.settings == nil {
		respondError(w, r, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE",
			"persistencia de configuración no disponible")
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	defer r.Body.Close() //nolint:errcheck
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "JSON inválido")
		return
	}

	if err := h.settings.Set(r.Context(), settingKeyUpdatesEnabled, strconv.FormatBool(body.Enabled)); err != nil {
		h.logger.Error("persist updates toggle", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL",
			"no se pudo persistir el ajuste")
		return
	}
	h.svc.SetUserEnabled(body.Enabled)
	h.logger.Info("updates toggle changed", "enabled", body.Enabled)

	respondJSON(w, http.StatusOK, map[string]any{
		"data": updatesConfigResponse{Enabled: body.Enabled},
	})
}
