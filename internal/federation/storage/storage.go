// Package storage is the persistence adapter for the federation
// subsystem. Pattern A dual-dialect: every sqlc-backed method has a
// SQLite branch and a Postgres branch; raw-SQL holdouts (FTS search,
// poster colour batch, federation_item_cache writer/reader) carry
// dialect-specific template variants pre-rewritten at construction
// time.
//
// The raw holdouts exist because:
//
//   - SearchSharedItems uses an FTS clause: SQLite's `items_fts MATCH ?`
//     (FTS5 virtual table) vs Postgres' `search_vector @@ to_tsquery(...)`
//     (tsvector column). sqlc doesn't parse either, hence raw.
//   - attachPrimaryImageColors has a runtime-sized IN list.
//   - ListCachedItems uses `ORDER BY ... COLLATE NOCASE` which is
//     SQLite-only; the Postgres branch substitutes `LOWER(...)`.
//   - UpsertCachedItems' inner INSERT was raw historically because
//     sqlc 1.31.1 truncated the surrounding query file; we keep it
//     raw with dialect-aware placeholder rewrite.
//
// El paquete vive bajo `internal/federation/` para cerrar la inversión
// de capa documentada como olor B en la auditoría 2026-05-14: los
// tipos de dominio de federation viven en el feature, y el adapter de
// persistencia es un sub-paquete del mismo feature en lugar de un
// importador de tipos desde `internal/db`.
package storage

import (
	"database/sql"
	"fmt"

	"hubplay/internal/db"
	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
)

// Repository agrupa queries de federation. Thread-safe (delega en *sql.DB).
type Repository struct {
	db     *sql.DB
	sq     *sqlc.Queries
	pq     *sqlc_pg.Queries
	driver string

	searchSharedItemsSQL string
	insertCachedItemSQL  string
	listCachedItemsSQL   string
}

// NewRepository crea un repo para la conexion DB dada.
func NewRepository(driver string, database *sql.DB) *Repository {
	r := &Repository{db: database, driver: driver}
	if db.IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}

	// Pre-rewrite de SQL raw por dialecto (FTS + sort case-insensitive).
	r.searchSharedItemsSQL = db.RewritePlaceholders(driver, buildSearchSharedItemsSQL(driver))
	r.insertCachedItemSQL = db.RewritePlaceholders(driver, `
		INSERT INTO federation_item_cache
		    (peer_id, library_id, remote_id, type, title,
		     year, overview, has_poster,
		     poster_color, poster_color_muted, cached_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	r.listCachedItemsSQL = db.RewritePlaceholders(driver, fmt.Sprintf(`
		SELECT remote_id, type, title,
		       COALESCE(year, 0) AS year,
		       COALESCE(overview, '') AS overview,
		       has_poster, poster_color, poster_color_muted
		FROM federation_item_cache
		WHERE peer_id = ? AND library_id = ?
		ORDER BY %s ASC
		LIMIT ? OFFSET ?`, caseInsensitiveSort(driver, "title")))

	return r
}

func (r *Repository) useSQLite() bool { return r.sq != nil }

// caseInsensitiveSort: expresion por dialecto para ORDER BY case-insensitive.
// SQLite: COLLATE NOCASE. Postgres: LOWER(col).
func caseInsensitiveSort(driver, col string) string {
	if db.IsPostgres(driver) {
		return "LOWER(" + col + ")"
	}
	return col + " COLLATE NOCASE"
}

// buildSearchSharedItemsSQL construye la query FTS por dialecto.
// SQLite: items_fts MATCH. Postgres: search_vector @@ to_tsquery.
func buildSearchSharedItemsSQL(driver string) string {
	var ftsClause string
	if db.IsPostgres(driver) {
		ftsClause = "i.search_vector @@ to_tsquery('simple', ?)"
	} else {
		ftsClause = "i.rowid IN (SELECT rowid FROM items_fts WHERE items_fts MATCH ?)"
	}
	return fmt.Sprintf(`
		SELECT i.id, i.type, i.title,
		       COALESCE(i.year, 0),
		       COALESCE(m.overview, ''),
		       EXISTS (
		         SELECT 1 FROM images img
		          WHERE img.item_id = i.id
		            AND img.type = 'primary'
		            AND img.is_primary
		       ) AS has_poster,
		       i.library_id
		  FROM items i
		  JOIN federation_library_shares s ON s.library_id = i.library_id
		  LEFT JOIN metadata m ON m.item_id = i.id
		 WHERE s.peer_id = ?
		   AND s.can_browse
		   AND i.parent_id IS NULL
		   AND %s
		 ORDER BY %s ASC
		 LIMIT ?`, ftsClause, caseInsensitiveSort(driver, "i.sort_title"))
}
