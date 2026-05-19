package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// AuditLogRepository persiste el audit log unificado (migración 057).
// Raw SQL — el query principal es un SELECT con filtros opcionales
// (event_type, actor, ventana temporal, búsqueda en payload) que
// sqlc 1.31.1 trunca al primer NULL/OR/AND alternativo. La superficie
// es pequeña (Insert + Query + DeleteOlderThan) y estable.
type AuditLogRepository struct {
	db     *sql.DB
	driver string
}

func NewAuditLogRepository(driver string, database *sql.DB) *AuditLogRepository {
	return &AuditLogRepository{db: database, driver: driver}
}

// AuditLogRow representa una fila in-memory.  Mapea 1-a-1 con la
// migración 057.
type AuditLogRow struct {
	ID          string
	ActorUserID string
	EventType   string
	TargetType  string
	TargetID    string
	Payload     string // JSON o cadena vacía
	IPAddress   string
	UserAgent   string
	CreatedAt   time.Time
}

// Insert append a la tabla. Append-only: no se actualizan ni borran
// filas individuales (sólo el sweep por retención).
//
// Si CreatedAt está zero, se rellena con time.Now().UTC() — el caller
// puede pasarlo cuando ya tiene el momento exacto del evento
// (LoginHandler tras autenticar) o dejarlo a 0 para que el repo lo
// resuelva.
func (r *AuditLogRepository) Insert(ctx context.Context, row AuditLogRow) error {
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	q := rewritePlaceholders(r.driver, `
		INSERT INTO audit_log (
			id, actor_user_id, event_type, target_type, target_id,
			payload, ip_address, user_agent, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	_, err := r.db.ExecContext(ctx, q,
		row.ID,
		nullStringFromOptional(row.ActorUserID),
		row.EventType,
		row.TargetType,
		row.TargetID,
		row.Payload,
		row.IPAddress,
		row.UserAgent,
		row.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

// AuditQuery encapsula los filtros opcionales del panel admin.  Cada
// campo vacío/zero significa "sin filtrar por este eje".
type AuditQuery struct {
	// EventTypePrefix permite filtros tipo "auth." que enganchen
	// todos los eventos de esa categoría. Vacío = todos.
	EventTypePrefix string
	// ActorUserID exacto. Vacío = cualquiera.
	ActorUserID string
	// From / To son la ventana temporal. Zero = no acota ese lado.
	From, To time.Time
	// SearchText busca substring (case-sensitive) en payload, target_id
	// y user_agent. Vacío = no busca. LIKE %x% — no FTS porque la
	// cardinalidad esperada es modesta (cientos de miles como mucho
	// con 90d retention).
	SearchText string
	// Limit / Offset para paginación. Limit 0 = default 50.  Cap 500
	// para evitar requests que bloqueen el panel.
	Limit, Offset int
}

// Query devuelve filas + total que cumplirían sin paginación (para
// que el frontend pinte "Mostrando 50 de 1234"). Hace dos consultas
// — una de COUNT y otra de SELECT con LIMIT/OFFSET — porque emitir
// total + filas en una sola query con window function complica el
// SQL portable entre SQLite y Postgres sin beneficio claro.
func (r *AuditLogRepository) Query(ctx context.Context, q AuditQuery) (rows []AuditLogRow, total int64, err error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Limit > 500 {
		q.Limit = 500
	}
	if q.Offset < 0 {
		q.Offset = 0
	}

	where, args := buildAuditWhere(q)

	// COUNT total. Sin LIMIT.
	countQ := "SELECT COUNT(*) FROM audit_log"
	if where != "" {
		countQ += " WHERE " + where
	}
	countQ = rewritePlaceholders(r.driver, countQ)
	if err := r.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit log: %w", err)
	}

	// SELECT con paginación. ORDER BY created_at DESC + id como
	// desempate determinístico (dos eventos en el mismo timestamp
	// es posible si dos requests aterrizan simultáneamente).
	listQ := `SELECT id, actor_user_id, event_type, target_type, target_id,
	                 payload, ip_address, user_agent, created_at
	          FROM audit_log`
	if where != "" {
		listQ += " WHERE " + where
	}
	listQ += " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	listQ = rewritePlaceholders(r.driver, listQ)
	listArgs := append(append([]any(nil), args...), q.Limit, q.Offset)

	cur, err := r.db.QueryContext(ctx, listQ, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit log: %w", err)
	}
	defer cur.Close() //nolint:errcheck

	for cur.Next() {
		var row AuditLogRow
		var actor sql.NullString
		if err := cur.Scan(
			&row.ID, &actor, &row.EventType, &row.TargetType, &row.TargetID,
			&row.Payload, &row.IPAddress, &row.UserAgent, &row.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan audit log: %w", err)
		}
		row.ActorUserID = actor.String
		rows = append(rows, row)
	}
	if err := cur.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate audit log: %w", err)
	}
	return rows, total, nil
}

// buildAuditWhere compone el WHERE dinámico con sólo los filtros que
// el caller pasó. Devuelve también la slice de args en el orden
// que ? aparece en la cláusula.
func buildAuditWhere(q AuditQuery) (string, []any) {
	var parts []string
	var args []any

	if q.EventTypePrefix != "" {
		// LIKE 'auth.%' engancha el índice idx_audit_log_type_created
		// (prefix scan). Si el caller pasa el tipo exacto sin punto,
		// también funciona — el LIKE con un literal sin wildcard
		// degenera a igualdad.
		if strings.HasSuffix(q.EventTypePrefix, ".") || strings.Contains(q.EventTypePrefix, "*") {
			pattern := strings.ReplaceAll(q.EventTypePrefix, "*", "%")
			if !strings.HasSuffix(pattern, "%") {
				pattern += "%"
			}
			parts = append(parts, "event_type LIKE ?")
			args = append(args, pattern)
		} else {
			parts = append(parts, "event_type = ?")
			args = append(args, q.EventTypePrefix)
		}
	}
	if q.ActorUserID != "" {
		parts = append(parts, "actor_user_id = ?")
		args = append(args, q.ActorUserID)
	}
	if !q.From.IsZero() {
		parts = append(parts, "created_at >= ?")
		args = append(args, q.From.UTC())
	}
	if !q.To.IsZero() {
		parts = append(parts, "created_at <= ?")
		args = append(args, q.To.UTC())
	}
	if q.SearchText != "" {
		// Busca en target_id, payload, ip_address y user_agent. NO en
		// actor_user_id (filtro dedicado) ni event_type (filtro
		// dedicado). LIKE en tres columnas no es óptimo pero la
		// cardinalidad esperada es modesta.
		needle := "%" + q.SearchText + "%"
		parts = append(parts, "(target_id LIKE ? OR payload LIKE ? OR ip_address LIKE ? OR user_agent LIKE ?)")
		args = append(args, needle, needle, needle, needle)
	}

	return strings.Join(parts, " AND "), args
}

// DeleteOlderThan elimina filas con created_at < cutoff. El sweep de
// retención (internal/retention) la llama periódicamente. Devuelve
// cuántas borró para que el sweep loguee la métrica.
func (r *AuditLogRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	q := rewritePlaceholders(r.driver, `DELETE FROM audit_log WHERE created_at < ?`)
	res, err := r.db.ExecContext(ctx, q, cutoff.UTC())
	if err != nil {
		return 0, fmt.Errorf("delete old audit log rows: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}

// DistinctEventTypes devuelve los event_type únicos presentes en la
// tabla. El panel admin lo usa para poblar el dropdown de filtros
// con sólo los tipos que de hecho existen (no la lista teórica de
// todos los productores).
func (r *AuditLogRepository) DistinctEventTypes(ctx context.Context) ([]string, error) {
	q := rewritePlaceholders(r.driver, `SELECT DISTINCT event_type FROM audit_log ORDER BY event_type`)
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("distinct event types: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan event type: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
