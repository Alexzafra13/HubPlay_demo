package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
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

// LibraryEPGSourceRepository — Pattern A dual-dialect. Priority +
// last_*_count are declared INTEGER, surfacing as int64 in SQLite and
// int32 in Postgres; param casts happen at the call site so the
// domain stays on plain `int`. UpdatePriorities runs inside a tx so
// a torn reorder cannot leave two sources at the same priority.
type LibraryEPGSourceRepository struct {
	db *sql.DB
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewLibraryEPGSourceRepository(driver string, database *sql.DB) *LibraryEPGSourceRepository {
	r := &LibraryEPGSourceRepository{db: database}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *LibraryEPGSourceRepository) useSQLite() bool { return r.sq != nil }

// ListByLibrary returns every source for a library in priority order
// (lowest number first). An empty slice means the library has no
// sources configured — the service layer decides how to handle that
// (e.g. fall back to `libraries.epg_url` for backwards compat).
func (r *LibraryEPGSourceRepository) ListByLibrary(ctx context.Context, libraryID string) ([]*LibraryEPGSource, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListLibraryEPGSourcesByLibrary(ctx, libraryID)
		if err != nil {
			return nil, fmt.Errorf("list epg sources: %w", err)
		}
		out := make([]*LibraryEPGSource, 0, len(rows))
		for _, row := range rows {
			src := epgSourceFromSqliteListRow(row)
			out = append(out, &src)
		}
		return out, nil
	}
	rows, err := r.pq.ListLibraryEPGSourcesByLibrary(ctx, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list epg sources: %w", err)
	}
	out := make([]*LibraryEPGSource, 0, len(rows))
	for _, row := range rows {
		src := epgSourceFromPgListRow(row)
		out = append(out, &src)
	}
	return out, nil
}

// GetByID is used by the DELETE / UPDATE handlers to load a row and
// verify the caller actually has access to its library.
func (r *LibraryEPGSourceRepository) GetByID(ctx context.Context, id string) (*LibraryEPGSource, error) {
	if r.useSQLite() {
		row, err := r.sq.GetLibraryEPGSourceByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get epg source: %w", err)
		}
		src := epgSourceFromSqliteGetRow(row)
		return &src, nil
	}
	row, err := r.pq.GetLibraryEPGSourceByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get epg source: %w", err)
	}
	src := epgSourceFromPgGetRow(row)
	return &src, nil
}

// Create inserts a new source. Priority defaults to one past the
// current maximum for the library so a freshly-added source runs last
// and can be reordered from the UI if needed.
func (r *LibraryEPGSourceRepository) Create(ctx context.Context, src *LibraryEPGSource) error {
	if src.Priority == 0 {
		next, err := r.nextPriority(ctx, src.LibraryID)
		if err != nil {
			return fmt.Errorf("compute next priority: %w", err)
		}
		src.Priority = next
	}
	if src.CreatedAt.IsZero() {
		src.CreatedAt = time.Now().UTC()
	}

	var err error
	if r.useSQLite() {
		err = r.sq.CreateLibraryEPGSource(ctx, sqlc.CreateLibraryEPGSourceParams{
			ID:        src.ID,
			LibraryID: src.LibraryID,
			CatalogID: sql.NullString{String: src.CatalogID, Valid: src.CatalogID != ""},
			Url:       src.URL,
			Priority:  int64(src.Priority),
			CreatedAt: src.CreatedAt,
		})
	} else {
		err = r.pq.CreateLibraryEPGSource(ctx, sqlc_pg.CreateLibraryEPGSourceParams{
			ID:        src.ID,
			LibraryID: src.LibraryID,
			CatalogID: sql.NullString{String: src.CatalogID, Valid: src.CatalogID != ""},
			Url:       src.URL,
			Priority:  int32(src.Priority),
			CreatedAt: src.CreatedAt,
		})
	}
	if err != nil {
		if isUniqueConstraintError(err) {
			return ErrEPGSourceAlreadyAttached
		}
		return fmt.Errorf("insert epg source: %w", err)
	}
	return nil
}

// nextPriority computes the default-priority slot for a newly created
// source: one past the current max for the library. The underlying
// query returns COALESCE(MAX(priority), -1) so an empty library lands
// the new row at 0. sqlc surfaces it as `interface{}` (untyped MAX)
// in both backends; the runtime value is INTEGER, which lib/pq + pgx
// can either widen to int64 or keep at int32. Both paths are
// accepted so the call works regardless of which driver Sesión F
// wires up.
func (r *LibraryEPGSourceRepository) nextPriority(ctx context.Context, libraryID string) (int, error) {
	var (
		raw any
		err error
	)
	if r.useSQLite() {
		raw, err = r.sq.NextLibraryEPGSourcePriority(ctx, libraryID)
	} else {
		raw, err = r.pq.NextLibraryEPGSourcePriority(ctx, libraryID)
	}
	if err != nil {
		return 0, err
	}
	switch v := raw.(type) {
	case int64:
		if v >= 0 {
			return int(v) + 1, nil
		}
	case int32:
		if v >= 0 {
			return int(v) + 1, nil
		}
	}
	return 0, nil
}

// isUniqueConstraintError detects UNIQUE-constraint violations from
// both backends. SQLite (modernc.org/sqlite) emits "UNIQUE constraint
// failed: …"; Postgres emits SQLSTATE 23505 with "duplicate key
// value violates unique constraint". Substring matching is the
// pragmatic check while the project ships without a typed pgx /
// lib/pq dependency — once Sesión F wires the driver, this can
// switch to `errors.As(*pgconn.PgError)`.
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE") {
		return true
	}
	return strings.Contains(msg, "duplicate key value violates unique constraint") ||
		strings.Contains(msg, "SQLSTATE 23505")
}

// Delete removes a source. CASCADE on `libraries(id)` already covers
// the library-deletion case; this is for the admin "remove provider"
// button.
func (r *LibraryEPGSourceRepository) Delete(ctx context.Context, id string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteLibraryEPGSource(ctx, id)
	} else {
		err = r.pq.DeleteLibraryEPGSource(ctx, id)
	}
	if err != nil {
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
	if r.useSQLite() {
		qtx := r.sq.WithTx(tx)
		for i, id := range orderedIDs {
			if err := qtx.UpdateLibraryEPGSourcePriority(ctx, sqlc.UpdateLibraryEPGSourcePriorityParams{
				Priority: int64(i), ID: id, LibraryID: libraryID,
			}); err != nil {
				return fmt.Errorf("update priority for %s: %w", id, err)
			}
		}
	} else {
		qtx := r.pq.WithTx(tx)
		for i, id := range orderedIDs {
			if err := qtx.UpdateLibraryEPGSourcePriority(ctx, sqlc_pg.UpdateLibraryEPGSourcePriorityParams{
				Priority: int32(i), ID: id, LibraryID: libraryID,
			}); err != nil {
				return fmt.Errorf("update priority for %s: %w", id, err)
			}
		}
	}
	return tx.Commit()
}

// RecordRefresh persists the per-source status after a refresh pass.
// Called from RefreshEPG once the source has finished (whether ok or
// error) so the admin UI can show "davidmuma: 404 / epg.pw: 3200".
func (r *LibraryEPGSourceRepository) RecordRefresh(
	ctx context.Context,
	id, status, errMsg string,
	programs, channels int,
) error {
	now := time.Now().UTC()
	var err error
	if r.useSQLite() {
		err = r.sq.RecordLibraryEPGSourceRefresh(ctx, sqlc.RecordLibraryEPGSourceRefreshParams{
			LastRefreshedAt:  sql.NullTime{Time: now, Valid: true},
			LastStatus:       sql.NullString{String: status, Valid: status != ""},
			LastError:        sql.NullString{String: errMsg, Valid: errMsg != ""},
			LastProgramCount: int64(programs),
			LastChannelCount: int64(channels),
			ID:               id,
		})
	} else {
		err = r.pq.RecordLibraryEPGSourceRefresh(ctx, sqlc_pg.RecordLibraryEPGSourceRefreshParams{
			LastRefreshedAt:  sql.NullTime{Time: now, Valid: true},
			LastStatus:       sql.NullString{String: status, Valid: status != ""},
			LastError:        sql.NullString{String: errMsg, Valid: errMsg != ""},
			LastProgramCount: int32(programs),
			LastChannelCount: int32(channels),
			ID:               id,
		})
	}
	if err != nil {
		return fmt.Errorf("record refresh: %w", err)
	}
	return nil
}

func epgSourceFromSqliteListRow(row sqlc.ListLibraryEPGSourcesByLibraryRow) LibraryEPGSource {
	src := LibraryEPGSource{
		ID:               row.ID,
		LibraryID:        row.LibraryID,
		CatalogID:        row.CatalogID,
		URL:              row.Url,
		Priority:         int(row.Priority),
		LastStatus:       row.LastStatus,
		LastError:        row.LastError,
		LastProgramCount: int(row.LastProgramCount),
		LastChannelCount: int(row.LastChannelCount),
		CreatedAt:        row.CreatedAt,
	}
	if row.LastRefreshedAt.Valid {
		src.LastRefreshedAt = row.LastRefreshedAt.Time
	}
	return src
}

func epgSourceFromPgListRow(row sqlc_pg.ListLibraryEPGSourcesByLibraryRow) LibraryEPGSource {
	src := LibraryEPGSource{
		ID:               row.ID,
		LibraryID:        row.LibraryID,
		CatalogID:        row.CatalogID,
		URL:              row.Url,
		Priority:         int(row.Priority),
		LastStatus:       row.LastStatus,
		LastError:        row.LastError,
		LastProgramCount: int(row.LastProgramCount),
		LastChannelCount: int(row.LastChannelCount),
		CreatedAt:        row.CreatedAt,
	}
	if row.LastRefreshedAt.Valid {
		src.LastRefreshedAt = row.LastRefreshedAt.Time
	}
	return src
}

func epgSourceFromSqliteGetRow(row sqlc.GetLibraryEPGSourceByIDRow) LibraryEPGSource {
	src := LibraryEPGSource{
		ID:               row.ID,
		LibraryID:        row.LibraryID,
		CatalogID:        row.CatalogID,
		URL:              row.Url,
		Priority:         int(row.Priority),
		LastStatus:       row.LastStatus,
		LastError:        row.LastError,
		LastProgramCount: int(row.LastProgramCount),
		LastChannelCount: int(row.LastChannelCount),
		CreatedAt:        row.CreatedAt,
	}
	if row.LastRefreshedAt.Valid {
		src.LastRefreshedAt = row.LastRefreshedAt.Time
	}
	return src
}

func epgSourceFromPgGetRow(row sqlc_pg.GetLibraryEPGSourceByIDRow) LibraryEPGSource {
	src := LibraryEPGSource{
		ID:               row.ID,
		LibraryID:        row.LibraryID,
		CatalogID:        row.CatalogID,
		URL:              row.Url,
		Priority:         int(row.Priority),
		LastStatus:       row.LastStatus,
		LastError:        row.LastError,
		LastProgramCount: int(row.LastProgramCount),
		LastChannelCount: int(row.LastChannelCount),
		CreatedAt:        row.CreatedAt,
	}
	if row.LastRefreshedAt.Valid {
		src.LastRefreshedAt = row.LastRefreshedAt.Time
	}
	return src
}
