package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/domain"
)

type Library struct {
	ID              string
	Name            string
	ContentType     string // movies, shows, music, livetv
	ScanMode        string // auto, manual
	ScanInterval    string
	M3UURL          string
	EPGURL          string
	RefreshInterval string
	// LanguageFilter is a comma-separated list of ISO 639-1 lowercase
	// codes (e.g. "es,en"). When non-empty, M3U import drops every
	// channel whose language signals don't match any of the listed
	// codes. Empty string means "no filter" — every channel imports.
	// See iptv.MatchesLanguageFilter for the matching heuristics.
	LanguageFilter  string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Paths           []string // populated by GetByID/List
}

type LibraryRepository struct {
	db *sql.DB // kept for transactions
	q  *sqlc.Queries
}

func NewLibraryRepository(database *sql.DB) *LibraryRepository {
	return &LibraryRepository{db: database, q: sqlc.New(database)}
}

func (r *LibraryRepository) Create(ctx context.Context, lib *Library) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	qtx := r.q.WithTx(tx)

	err = qtx.InsertLibrary(ctx, sqlc.InsertLibraryParams{
		ID:              lib.ID,
		Name:            lib.Name,
		ContentType:     lib.ContentType,
		ScanMode:        lib.ScanMode,
		ScanInterval:    nullableString(lib.ScanInterval),
		M3uUrl:          nullableString(lib.M3UURL),
		EpgUrl:          nullableString(lib.EPGURL),
		RefreshInterval: nullableString(lib.RefreshInterval),
		LanguageFilter:  lib.LanguageFilter,
		CreatedAt:       lib.CreatedAt,
		UpdatedAt:       lib.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert library: %w", err)
	}

	for _, p := range lib.Paths {
		err = qtx.InsertLibraryPath(ctx, sqlc.InsertLibraryPathParams{
			LibraryID: lib.ID,
			Path:      p,
		})
		if err != nil {
			return fmt.Errorf("insert library path: %w", err)
		}
	}

	return tx.Commit()
}

func (r *LibraryRepository) GetByID(ctx context.Context, id string) (*Library, error) {
	row, err := r.q.GetLibraryByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("library %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get library %s: %w", id, err)
	}

	paths, err := r.q.ListPathsByLibrary(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get library paths: %w", err)
	}

	lib := libraryFromGetRow(row)
	lib.Paths = paths
	return &lib, nil
}

func (r *LibraryRepository) List(ctx context.Context) ([]*Library, error) {
	rows, err := r.q.ListLibraries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list libraries: %w", err)
	}

	libs := librariesFromListRows(rows)

	if err := r.loadPaths(ctx, libs); err != nil {
		return nil, err
	}
	return libs, nil
}

func (r *LibraryRepository) Update(ctx context.Context, lib *Library) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	qtx := r.q.WithTx(tx)

	n, err := qtx.UpdateLibrary(ctx, sqlc.UpdateLibraryParams{
		Name:            lib.Name,
		ContentType:     lib.ContentType,
		ScanMode:        lib.ScanMode,
		ScanInterval:    nullableString(lib.ScanInterval),
		M3uUrl:          nullableString(lib.M3UURL),
		EpgUrl:          nullableString(lib.EPGURL),
		RefreshInterval: nullableString(lib.RefreshInterval),
		LanguageFilter:  lib.LanguageFilter,
		UpdatedAt:       lib.UpdatedAt,
		ID:              lib.ID,
	})
	if err != nil {
		return fmt.Errorf("update library: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("library %s: %w", lib.ID, domain.ErrNotFound)
	}

	if err := qtx.DeletePathsByLibrary(ctx, lib.ID); err != nil {
		return fmt.Errorf("delete library paths: %w", err)
	}
	for _, p := range lib.Paths {
		err = qtx.InsertLibraryPath(ctx, sqlc.InsertLibraryPathParams{
			LibraryID: lib.ID,
			Path:      p,
		})
		if err != nil {
			return fmt.Errorf("insert library path: %w", err)
		}
	}

	return tx.Commit()
}

func (r *LibraryRepository) Delete(ctx context.Context, id string) error {
	n, err := r.q.DeleteLibrary(ctx, id)
	if err != nil {
		return fmt.Errorf("delete library: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("library %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// GrantAccess gives a user access to a library.
func (r *LibraryRepository) GrantAccess(ctx context.Context, userID, libraryID string) error {
	err := r.q.GrantLibraryAccess(ctx, sqlc.GrantLibraryAccessParams{
		UserID:    userID,
		LibraryID: libraryID,
	})
	if err != nil {
		return fmt.Errorf("grant library access: %w", err)
	}
	return nil
}

// RevokeAccess removes a user's access to a library.
func (r *LibraryRepository) RevokeAccess(ctx context.Context, userID, libraryID string) error {
	err := r.q.RevokeLibraryAccess(ctx, sqlc.RevokeLibraryAccessParams{
		UserID:    userID,
		LibraryID: libraryID,
	})
	if err != nil {
		return fmt.Errorf("revoke library access: %w", err)
	}
	return nil
}

// UserHasAccess reports whether userID is allowed to access libraryID under
// the opt-in ACL model: the user has an explicit grant, OR no grants at all
// exist for the library (public by default).
//
// Kept as raw SQL (not sqlc) because the library_id parameter appears twice
// and sqlc's SQLite engine mishandles duplicate positional bindings for this
// kind of branched EXISTS query — same limitation already documented for
// NextUp in user_data_repository.go.
func (r *LibraryRepository) UserHasAccess(ctx context.Context, userID, libraryID string) (bool, error) {
	const query = `
		SELECT CASE
			WHEN EXISTS (SELECT 1 FROM library_access WHERE library_id = ? AND user_id = ?) THEN 1
			WHEN NOT EXISTS (SELECT 1 FROM library_access WHERE library_id = ?)            THEN 1
			ELSE 0
		END`
	var has int
	if err := r.db.QueryRowContext(ctx, query, libraryID, userID, libraryID).Scan(&has); err != nil {
		return false, fmt.Errorf("check library access: %w", err)
	}
	return has == 1, nil
}

// ListForUser returns libraries a user has access to. If empty, all are accessible.
func (r *LibraryRepository) ListForUser(ctx context.Context, userID string) ([]*Library, error) {
	rows, err := r.q.ListLibrariesForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list libraries for user: %w", err)
	}

	libs := librariesFromForUserRows(rows)

	if err := r.loadPaths(ctx, libs); err != nil {
		return nil, err
	}
	return libs, nil
}

// loadPaths fetches paths for all given libraries in a single query
// and assigns them to the corresponding Library structs.
func (r *LibraryRepository) loadPaths(ctx context.Context, libs []*Library) error {
	if len(libs) == 0 {
		return nil
	}

	allPaths, err := r.q.ListAllPaths(ctx)
	if err != nil {
		return fmt.Errorf("batch load library paths: %w", err)
	}

	pathsByLib := make(map[string][]string)
	for _, lp := range allPaths {
		pathsByLib[lp.LibraryID] = append(pathsByLib[lp.LibraryID], lp.Path)
	}

	for _, lib := range libs {
		lib.Paths = pathsByLib[lib.ID]
	}
	return nil
}

// ── row mapping helpers ─────────────────────────────────────────────────

func libraryFromGetRow(r sqlc.GetLibraryByIDRow) Library {
	return Library{
		ID:              r.ID,
		Name:            r.Name,
		ContentType:     r.ContentType,
		ScanMode:        r.ScanMode,
		ScanInterval:    r.ScanInterval,
		M3UURL:          r.M3uUrl,
		EPGURL:          r.EpgUrl,
		RefreshInterval: r.RefreshInterval,
		LanguageFilter:  r.LanguageFilter,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

func libraryFromListRow(r sqlc.ListLibrariesRow) Library {
	return Library{
		ID:              r.ID,
		Name:            r.Name,
		ContentType:     r.ContentType,
		ScanMode:        r.ScanMode,
		ScanInterval:    r.ScanInterval,
		M3UURL:          r.M3uUrl,
		EPGURL:          r.EpgUrl,
		RefreshInterval: r.RefreshInterval,
		LanguageFilter:  r.LanguageFilter,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

func libraryFromForUserRow(r sqlc.ListLibrariesForUserRow) Library {
	return Library{
		ID:              r.ID,
		Name:            r.Name,
		ContentType:     r.ContentType,
		ScanMode:        r.ScanMode,
		ScanInterval:    r.ScanInterval,
		M3UURL:          r.M3uUrl,
		EPGURL:          r.EpgUrl,
		RefreshInterval: r.RefreshInterval,
		LanguageFilter:  r.LanguageFilter,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

func librariesFromListRows(rows []sqlc.ListLibrariesRow) []*Library {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*Library, len(rows))
	for i, row := range rows {
		lib := libraryFromListRow(row)
		out[i] = &lib
	}
	return out
}

func librariesFromForUserRows(rows []sqlc.ListLibrariesForUserRow) []*Library {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*Library, len(rows))
	for i, row := range rows {
		lib := libraryFromForUserRow(row)
		out[i] = &lib
	}
	return out
}
