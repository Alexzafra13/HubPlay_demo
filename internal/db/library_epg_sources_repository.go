package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrEPGSourceAlreadyAttached signals that an admin tried to add an
// EPG source whose URL is already configured for the library. Exposed
// as a sentinel so the handler layer can translate it into a clean
// 409 Conflict — previously the raw "UNIQUE constraint failed" SQLite
// error bubbled all the way to the user.
var ErrEPGSourceAlreadyAttached = errors.New("epg source with that url already attached")

// LibraryEPGSource is one configured XMLTV provider attached to a
// livetv library. Priority is "lower first": the refresher processes
// sources in ascending priority order and a channel covered by an
// earlier source is not overwritten by a later one.
//
// CatalogID is nullable. When set, it names an entry in
// `internal/iptv/epg_catalog.go` PublicEPGSources(); the URL column
// holds the snapshot at the time the source was added so a stale row
// keeps working even if the catalog entry is later retired.
type LibraryEPGSource struct {
	ID               string
	LibraryID        string
	CatalogID        string // empty = custom URL
	URL              string
	Priority         int
	LastRefreshedAt  time.Time
	LastStatus       string // "ok" | "error" | "" (never refreshed)
	LastError        string
	LastProgramCount int
	LastChannelCount int
	CreatedAt        time.Time
}

// LibraryEPGSourceRepository persists the per-library list of EPG
// providers. The raw SQL is on purpose: at the time this file was
// added the sqlc generator hadn't been re-run for the new 007_
// migration, and keeping the adapter self-contained means a fresh
// clone doesn't need `make sqlc` to build. The next sqlc regen pass
// can replace the bodies with the generated querier without touching
// callers.
type LibraryEPGSourceRepository struct {
	db *sql.DB
}

func NewLibraryEPGSourceRepository(database *sql.DB) *LibraryEPGSourceRepository {
	return &LibraryEPGSourceRepository{db: database}
}

// ListByLibrary returns every source for a library in priority order
// (lowest number first). An empty slice means the library has no
// sources configured — the service layer decides how to handle that
// (e.g. fall back to `libraries.epg_url` for backwards compat).
func (r *LibraryEPGSourceRepository) ListByLibrary(ctx context.Context, libraryID string) ([]*LibraryEPGSource, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, library_id, COALESCE(catalog_id,''), url, priority,
		        COALESCE(last_refreshed_at, ''), COALESCE(last_status,''),
		        COALESCE(last_error,''), last_program_count, last_channel_count, created_at
		 FROM library_epg_sources
		 WHERE library_id = ?
		 ORDER BY priority ASC, created_at ASC`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list epg sources: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []*LibraryEPGSource
	for rows.Next() {
		src := &LibraryEPGSource{}
		var lastRefreshRaw any
		if err := rows.Scan(&src.ID, &src.LibraryID, &src.CatalogID, &src.URL, &src.Priority,
			&lastRefreshRaw, &src.LastStatus, &src.LastError,
			&src.LastProgramCount, &src.LastChannelCount, &src.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan epg source: %w", err)
		}
		// last_refreshed_at uses the same modernc.org/sqlite coercion
		// story as epg_programs.start_time — see coerceSQLiteTime.
		if src.LastRefreshedAt, err = coerceSQLiteTime(lastRefreshRaw); err != nil {
			return nil, fmt.Errorf("parse last_refreshed_at: %w", err)
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// GetByID is used by the DELETE / UPDATE handlers to load a row and
// verify the caller actually has access to its library.
func (r *LibraryEPGSourceRepository) GetByID(ctx context.Context, id string) (*LibraryEPGSource, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, COALESCE(catalog_id,''), url, priority,
		        COALESCE(last_refreshed_at, ''), COALESCE(last_status,''),
		        COALESCE(last_error,''), last_program_count, last_channel_count, created_at
		 FROM library_epg_sources WHERE id = ?`, id)

	src := &LibraryEPGSource{}
	var lastRefreshRaw any
	err := row.Scan(&src.ID, &src.LibraryID, &src.CatalogID, &src.URL, &src.Priority,
		&lastRefreshRaw, &src.LastStatus, &src.LastError,
		&src.LastProgramCount, &src.LastChannelCount, &src.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get epg source: %w", err)
	}
	if src.LastRefreshedAt, err = coerceSQLiteTime(lastRefreshRaw); err != nil {
		return nil, fmt.Errorf("parse last_refreshed_at: %w", err)
	}
	return src, nil
}

// Create inserts a new source. Priority defaults to one past the
// current maximum for the library so a freshly-added source runs last
// and can be reordered from the UI if needed.
func (r *LibraryEPGSourceRepository) Create(ctx context.Context, src *LibraryEPGSource) error {
	if src.Priority == 0 {
		var maxPrio sql.NullInt64
		if err := r.db.QueryRowContext(ctx,
			`SELECT MAX(priority) FROM library_epg_sources WHERE library_id = ?`,
			src.LibraryID).Scan(&maxPrio); err != nil {
			return fmt.Errorf("compute next priority: %w", err)
		}
		if maxPrio.Valid {
			src.Priority = int(maxPrio.Int64) + 1
		}
	}
	if src.CreatedAt.IsZero() {
		src.CreatedAt = time.Now().UTC()
	}
	catalogID := sql.NullString{String: src.CatalogID, Valid: src.CatalogID != ""}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO library_epg_sources
		 (id, library_id, catalog_id, url, priority, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		src.ID, src.LibraryID, catalogID, src.URL, src.Priority, src.CreatedAt)
	if err != nil {
		// modernc.org/sqlite doesn't ship typed error values for
		// constraint failures; the message is stable ("UNIQUE
		// constraint failed: library_epg_sources.library_id, ...url")
		// so a substring match is the pragmatic check.
		if isUniqueConstraintError(err) {
			return ErrEPGSourceAlreadyAttached
		}
		return fmt.Errorf("insert epg source: %w", err)
	}
	return nil
}

// isUniqueConstraintError detects SQLite UNIQUE constraint violations
// regardless of the driver's error shape. Kept at the file level so
// we can reuse it from the other inserts that hit the same table.
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}

// Delete removes a source. CASCADE on `libraries(id)` already covers
// the library-deletion case; this is for the admin "remove provider"
// button.
func (r *LibraryEPGSourceRepository) Delete(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM library_epg_sources WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete epg source: %w", err)
	}
	return nil
}

// UpdatePriorities rewrites the priority of every provided source in
// a single transaction. The handler passes the full ordered id list;
// the repo assigns 0..N-1 in that order. Keeping it atomic prevents a
// torn reorder from leaving two sources with the same priority.
func (r *LibraryEPGSourceRepository) UpdatePriorities(ctx context.Context, libraryID string, orderedIDs []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.PrepareContext(ctx,
		`UPDATE library_epg_sources SET priority = ? WHERE id = ? AND library_id = ?`)
	if err != nil {
		return fmt.Errorf("prepare update: %w", err)
	}
	defer stmt.Close() //nolint:errcheck

	for i, id := range orderedIDs {
		if _, err := stmt.ExecContext(ctx, i, id, libraryID); err != nil {
			return fmt.Errorf("update priority for %s: %w", id, err)
		}
	}
	return tx.Commit()
}

// RecordRefresh persists the per-source status after a refresh pass.
// Called from RefreshEPG once the source has finished (whether ok or
// error) so the admin UI can show "davidmuma: ❌ 404 / epg.pw: ✅ 3200".
func (r *LibraryEPGSourceRepository) RecordRefresh(
	ctx context.Context,
	id, status, errMsg string,
	programs, channels int,
) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE library_epg_sources SET
		    last_refreshed_at  = ?,
		    last_status        = ?,
		    last_error         = ?,
		    last_program_count = ?,
		    last_channel_count = ?
		 WHERE id = ?`,
		now, status, errMsg, programs, channels, id)
	if err != nil {
		return fmt.Errorf("record refresh: %w", err)
	}
	return nil
}
