package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/event"
)

// UploadsHandler hospeda los endpoints custom del feature de upload
// que NO son parte del protocolo tus:
//   GET /api/v1/uploads/mine    — lista de las últimas N filas de
//                                  audit del usuario (refresh repobla
//                                  el panel "tus uploads").
//   GET /api/v1/uploads/events  — SSE filtrado a sus uploads (publica
//                                  UploadPhase + UploadDone + UploadError
//                                  cuyo data.user_id coincide).
//
// El POST/PATCH/HEAD/DELETE del protocolo tus los sirve el TusdHandler
// del paquete internal/upload, montado en /api/v1/uploads/ por el
// router. Aquí sólo viven las superficies de lectura.
type UploadsHandler struct {
	audit   UploadAuditLister
	bus     EventBusSubscriber
	limiter *SSELimiter
	logger  *slog.Logger
}

func NewUploadsHandler(audit UploadAuditLister, bus EventBusSubscriber, limiter *SSELimiter, logger *slog.Logger) *UploadsHandler {
	return &UploadsHandler{
		audit:   audit,
		bus:     bus,
		limiter: limiter,
		logger:  logger.With("module", "uploads-handler"),
	}
}

// ListMine devuelve las últimas N filas de audit del usuario que
// autenticó. limit default 50, cap 200 — el cliente paginará pidiendo
// más explícitamente cuando lo necesite.
func (h *UploadsHandler) ListMine(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}

	rows, err := h.audit.ListByUser(r.Context(), claims.UserID, limit)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "AUDIT_QUERY_FAILED", err.Error())
		return
	}

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"id":             row.ID,
			"library_id":     row.LibraryID,
			"original_name":  row.OriginalName,
			"final_path":     row.FinalPath,
			"bytes":          row.Bytes,
			"mime_detected":  row.MimeDetected,
			"outcome":        row.Outcome,
			"error_message":  row.ErrorMessage,
			"started_at":     row.StartedAt.Format(time.RFC3339),
			"finished_at":    row.FinishedAt.Format(time.RFC3339),
			"duration_ms":    row.DurationMs,
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// Stream sirve un SSE filtrado a los uploads del usuario. Reusa el
// patrón del EventHandler base (limiter, keepalive, defer-unsub) pero
// se suscribe sólo a UploadBytes/Phase/Done/Error y descarta eventos
// cuyo data.user_id no case con las claims.
//
// Decisión de filtrar aquí (vs. publicar dos canales): la información
// del user_id ya viaja en el data — un filtrado server-side mantiene
// la garantía de privacidad y simplifica el bus (un solo topic). El
// coste es que un usuario con 50 uploads simultáneos hace que TODOS
// los suscriptores reciban handlers; aceptable para self-hosted donde
// el orden de magnitud es decenas.
func (h *UploadsHandler) Stream(w http.ResponseWriter, r *http.Request) {
	_ = DisableWriteDeadline(w)
	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, r, http.StatusInternalServerError, "SSE_NOT_SUPPORTED", "streaming not supported")
		return
	}

	claims := auth.GetClaims(r.Context())
	if claims == nil || claims.UserID == "" {
		respondError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}
	userID := claims.UserID

	if h.limiter != nil {
		release, err := h.limiter.Acquire(userID)
		if err != nil {
			w.Header().Set("Retry-After", "30")
			respondError(w, r, http.StatusServiceUnavailable, "SSE_CAP_EXCEEDED",
				"too many concurrent event streams; retry shortly")
			return
		}
		defer release()
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", CacheControlNoCache)
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	eventCh := make(chan event.Event, 64)
	types := []event.Type{
		event.UploadBytes,
		event.UploadPhase,
		event.UploadDone,
		event.UploadError,
	}
	unsubs := make([]func(), 0, len(types))
	for _, t := range types {
		t := t
		unsubs = append(unsubs, h.bus.Subscribe(t, func(e event.Event) {
			// Filtrado server-side por user_id. Si el evento no lo
			// trae (defensive), lo dejamos pasar — el cliente decide
			// si lo ignora. Mejor laxo que perder un terminal.
			if uid, ok := e.Data["user_id"].(string); ok && uid != userID {
				return
			}
			select {
			case eventCh <- e:
			default:
				// Slow consumer — drop. Mejor perder un phase que
				// bloquear el publisher (que es síncrono dentro del
				// pipeline).
			}
		}))
	}
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()

	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				continue
			}
			flusher.Flush()
		case evt := <-eventCh:
			data, err := json.Marshal(map[string]any{
				"type": evt.Type,
				"data": evt.Data,
			})
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
			flusher.Flush()
		}
	}
}
