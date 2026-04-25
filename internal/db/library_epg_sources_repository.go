package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"hubplay/internal/db/sqlc"
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
// providers. Sqlc-generated queries handle the per-row scan; this
// adapter projects the optional columns into the domain shape and
// owns the priority-on-insert + UNIQUE-conflict semantics.
type LibraryEPGSourceRepository struct {
	db *sql.DB
	q  *sqlc.Queries
}

func NewLibraryEPGSourceRepository(database *sql.DB) *LibraryEPGSourceRepository {
	return &LibraryEPGSourceRepository{db: database, q: sqlc.New(database)}
}

// ListByLibrary returns every source for a library in priority order
// (lowest number first). An empty slice means the library has no
// sources configured — the service layer decides how to handle that
// (e.g. fall back to `libraries.epg_url` for backwards compat).
func (r *LibraryEPGSourceRepository) ListByLibrary(ctx context.Context, libraryID string) ([]*LibraryEPGSource, error) {
	rows, err := r.q.ListLibraryEPGSourcesByLibrary(ctx, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list epg sources: %w", err)
	}
	out := make([]*LibraryEPGSource, 0, len(rows))
	for _, row := range rows {
		src := epgSourceFromList(row)
		out = append(out, &src)
	}
	return out, nil
}

// GetByID is used by the DELETE / UPDATE handlers to load a row and
// verify the caller actually has access to its library.
func (r *LibraryEPGSourceRepository) GetByID(ctx context.Context, id string) (*LibraryEPGSource, error) {
	row, err := r.q.GetLibraryEPGSourceByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get epg source: %w", err)
	}
	src := epgSourceFromGet(row)
	return &src, nil
}

// Create inserts a new source. Priority defaults to one past the
// current maximum for the library so a freshly-added source runs last
// and can be reordered from the UI if needed.
func (r *LibraryEPGSourceRepository) Create(ctx context.Context, src *LibraryEPGSource) error {
	if src.Priority == 0 {
		raw, err := r.q.NextLibraryEPGSourcePriority(ctx, src.LibraryID)
		if err != nil {
			return fmt.Errorf("compute next priority: %w", err)
		}
		// COALESCE(MAX(priority), -1) returns -1 when the library has
		// no sources yet → the new row goes to priority 0. sqlc
		// surfaces it as interface{} (untyped MAX); the value is an
		// int64 in practice.
		if v, ok := raw.(int64); ok && v >= 0 {
			src.Priority = int(v) + 1
		}
	}
	if src.CreatedAt.IsZero() {
		src.CreatedAt = time.Now().UTC()
	}

	err := r.q.CreateLibraryEPGSource(ctx, sqlc.CreateLibraryEPGSourceParams{
		ID:        src.ID,
		LibraryID: src.LibraryID,
		CatalogID: sql.NullString{String: src.CatalogID, Valid: src.CatalogID != ""},
		Url:       src.URL,
		Priority:  int64(src.Priority),
		CreatedAt: src.CreatedAt,
	})
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
	if err := r.q.DeleteLibraryEPGSource(ctx, id); err != nil {
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
	qtx := r.q.WithTx(tx)
	for i, id := range orderedIDs {
		if err := qtx.UpdateLibraryEPGSourcePriority(ctx, sqlc.UpdateLibraryEPGSourcePriorityParams{
			Priority: int64(i), ID: id, LibraryID: libraryID,
		}); err != nil {
			return fmt.Errorf("update priority for %s: %w", id, err)
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
	if err := r.q.RecordLibraryEPGSourceRefresh(ctx, sqlc.RecordLibraryEPGSourceRefreshParams{
		LastRefreshedAt:  sql.NullTime{Time: now, Valid: true},
		LastStatus:       sql.NullString{String: status, Valid: status != ""},
		LastError:        sql.NullString{String: errMsg, Valid: errMsg != ""},
		LastProgramCount: int64(programs),
		LastChannelCount: int64(channels),
		ID:               id,
	}); err != nil {
		return fmt.Errorf("record refresh: %w", err)
	}
	return nil
}

// epgSourceFromList projects the list-row shape into the domain
// struct. Same column projection as the GET path; kept separate
// because sqlc generates one type per query.
func epgSourceFromList(row sqlc.ListLibraryEPGSourcesByLibraryRow) LibraryEPGSource {
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

// epgSourceFromGet projects the get-row shape into the domain struct.
func epgSourceFromGet(row sqlc.GetLibraryEPGSourceByIDRow) LibraryEPGSource {
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
