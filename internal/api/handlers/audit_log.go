package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"hubplay/internal/db"
)

// AuditLogStore es la mínima superficie del repo que este handler
// necesita.  Aísla del *db.AuditLogRepository concreto para que
// tests pasen un fake.
type AuditLogStore interface {
	Query(ctx context.Context, q db.AuditQuery) ([]db.AuditLogRow, int64, error)
	DistinctEventTypes(ctx context.Context) ([]string, error)
}

// AuditLogHandler hospeda los endpoints de lectura del audit log
// unificado (PR5).
//
//   GET /api/v1/admin/audit-log
//     query params:
//       type   string  prefix-matching ("auth." engancha todos los
//                      eventos de auth). vacío = sin filtro.
//       actor  string  user id exacto.
//       from   string  RFC3339, lower bound inclusive.
//       to     string  RFC3339, upper bound inclusive.
//       q      string  search libre (target_id + payload + ip + ua).
//       limit  int     default 50, cap 500.
//       offset int     paginación.
//
//   GET /api/v1/admin/audit-log/types
//     devuelve la lista de event_type distintos presentes en la
//     tabla, para que el frontend pueda poblar el dropdown sin
//     hardcodear la lista (que crece cada vez que un productor
//     añade un evento).
//
// Gate: en el router, owner-OR-can_view_audit.
type AuditLogHandler struct {
	store  AuditLogStore
	logger *slog.Logger
}

func NewAuditLogHandler(store AuditLogStore, logger *slog.Logger) *AuditLogHandler {
	return &AuditLogHandler{
		store:  store,
		logger: logger.With("module", "audit-log-handler"),
	}
}

// Query parsea los filtros del query string + llama al repo.
//
// La respuesta:
//   {
//     "data": {
//       "rows":   [ {id, actor_user_id, event_type, ...}, ... ],
//       "total":  1234,
//       "limit":  50,
//       "offset": 0
//     }
//   }
func (h *AuditLogHandler) Query(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	query := db.AuditQuery{
		EventTypePrefix: q.Get("type"),
		ActorUserID:     q.Get("actor"),
		SearchText:      q.Get("q"),
		Limit:           limit,
		Offset:          offset,
	}
	if from := q.Get("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			query.From = t
		} else {
			respondError(w, r, http.StatusBadRequest, "BAD_FROM",
				"from must be RFC3339")
			return
		}
	}
	if to := q.Get("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			query.To = t
		} else {
			respondError(w, r, http.StatusBadRequest, "BAD_TO",
				"to must be RFC3339")
			return
		}
	}

	rows, total, err := h.store.Query(r.Context(), query)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"id":              row.ID,
			"actor_user_id":   row.ActorUserID,
			"actor_username":  row.ActorUsername,
			"event_type":      row.EventType,
			"target_type":     row.TargetType,
			"target_id":       row.TargetID,
			"target_username": row.TargetUsername,
			"payload":         row.Payload,
			"ip_address":      row.IPAddress,
			"user_agent":      row.UserAgent,
			"created_at":      row.CreatedAt.Format(time.RFC3339),
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"rows":   out,
			"total":  total,
			"limit":  effectiveLimit(query.Limit),
			"offset": offset,
		},
	})
}

func effectiveLimit(l int) int {
	if l <= 0 {
		return 50
	}
	if l > 500 {
		return 500
	}
	return l
}

// EventTypes devuelve la lista de event_type distintos en la tabla.
func (h *AuditLogHandler) EventTypes(w http.ResponseWriter, r *http.Request) {
	types, err := h.store.DistinctEventTypes(r.Context())
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": types})
}
