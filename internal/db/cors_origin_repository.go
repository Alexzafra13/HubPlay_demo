package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CorsOriginRepository persiste los orígenes CORS añadidos en runtime
// via el panel admin (PR4 feature CORS-dynamic). Raw SQL — la
// superficie es tan pequeña (3 métodos) que un query file sqlc sólo
// añade ceremonia.
//
// Owner-only en el HTTP layer: este repo no enforza permisos, los
// callers (handler) lo hacen. El repo confía en que sólo el owner
// llega aquí.
type CorsOriginRepository struct {
	db     *sql.DB
	driver string
}

func NewCorsOriginRepository(driver string, database *sql.DB) *CorsOriginRepository {
	return &CorsOriginRepository{db: database, driver: driver}
}

// CorsOriginRow es la representación in-memory de una fila. Mapea
// 1-a-1 con la migración 056.
type CorsOriginRow struct {
	Origin    string
	CreatedBy string // user id del owner que lo añadió; "" si NULL en DB
	CreatedAt time.Time
	Note      string
}

// ErrCorsOriginExists: clave única violada al insertar. El handler lo
// traduce a 409 CONFLICT — el operador puede entender mejor "ya existe"
// que un mensaje genérico de constraint.
var ErrCorsOriginExists = errors.New("cors origin already exists")

// Insert añade un origen. origin debe venir ya VALIDADO por el handler
// (esquema http/https, sin path, sin wildcards). El repo no re-valida —
// hacerlo sería duplicación; el handler tiene contexto operacional
// para devolver mensajes útiles.
func (r *CorsOriginRepository) Insert(ctx context.Context, row CorsOriginRow) error {
	query := rewritePlaceholders(r.driver, `
		INSERT INTO cors_origins (origin, created_by, created_at, note)
		VALUES (?, ?, ?, ?)`)
	createdAt := row.CreatedAt
	if createdAt.IsZero() {
		createdAt = timeNow().UTC()
	}
	_, err := r.db.ExecContext(ctx, query,
		row.Origin,
		nullStringFromOptional(row.CreatedBy),
		createdAt,
		row.Note,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return ErrCorsOriginExists
		}
		return fmt.Errorf("insert cors origin: %w", err)
	}
	return nil
}

// Delete borra un origen por su valor exacto. No-op si no existe
// (devuelve nil, no error) — el handler puede traducir a 204 No Content
// sin distinguir "borrado" de "ya no estaba".
func (r *CorsOriginRepository) Delete(ctx context.Context, origin string) error {
	query := rewritePlaceholders(r.driver, `DELETE FROM cors_origins WHERE origin = ?`)
	if _, err := r.db.ExecContext(ctx, query, origin); err != nil {
		return fmt.Errorf("delete cors origin %q: %w", origin, err)
	}
	return nil
}

// List devuelve TODOS los orígenes ordenados por created_at DESC. Sin
// paginación porque la cardinalidad esperada es pequeña (decenas como
// mucho); listar todo es más simple y más útil para el panel.
func (r *CorsOriginRepository) List(ctx context.Context) ([]CorsOriginRow, error) {
	query := rewritePlaceholders(r.driver, `
		SELECT origin, created_by, created_at, note
		FROM cors_origins
		ORDER BY created_at DESC`)
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list cors origins: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []CorsOriginRow
	for rows.Next() {
		var row CorsOriginRow
		var createdBy sql.NullString
		if err := rows.Scan(&row.Origin, &createdBy, &row.CreatedAt, &row.Note); err != nil {
			return nil, fmt.Errorf("scan cors origin row: %w", err)
		}
		row.CreatedBy = createdBy.String
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cors origin rows: %w", err)
	}
	return out, nil
}

// ListOrigins devuelve sólo los origin strings — útil para el
// middleware CORS que sólo necesita la lista para matching. Equivale
// a List() + map, pero ahorra el alloc de las filas completas.
func (r *CorsOriginRepository) ListOrigins(ctx context.Context) ([]string, error) {
	query := rewritePlaceholders(r.driver, `SELECT origin FROM cors_origins`)
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list cors origin strings: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			return nil, fmt.Errorf("scan origin: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
