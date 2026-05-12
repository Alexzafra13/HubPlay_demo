package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
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
	LanguageFilter  string
	TLSInsecure     bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Paths           []string // populated by GetByID/List
}

// LibraryRepository — Pattern A dual-dialect. Mixes sqlc methods
// (Create/Update/Get/...) with raw SQL holdouts (UserHasAccess,
// ListForUser) — the latter pre-date and continue post the sqlc
// 1.31.x parser bug around JOIN+COALESCE.
type LibraryRepository struct {
	db *sql.DB // kept for transactions + raw SQL holdouts
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewLibraryRepository(driver string, database *sql.DB) *LibraryRepository {
	r := &LibraryRepository{db: database}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *LibraryRepository) useSQLite() bool { return r.sq != nil }

func (r *LibraryRepository) Create(ctx context.Context, lib *Library) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if r.useSQLite() {
		qtx := r.sq.WithTx(tx)
		if err := qtx.InsertLibrary(ctx, sqlc.InsertLibraryParams{
			ID:              lib.ID,
			Name:            lib.Name,
			ContentType:     lib.ContentType,
			ScanMode:        lib.ScanMode,
			ScanInterval:    nullableString(lib.ScanInterval),
			M3uUrl:          nullableString(lib.M3UURL),
			EpgUrl:          nullableString(lib.EPGURL),
			RefreshInterval: nullableString(lib.RefreshInterval),
			LanguageFilter:  lib.LanguageFilter,
			TlsInsecure:     boolToInt64(lib.TLSInsecure),
			CreatedAt:       lib.CreatedAt,
			UpdatedAt:       lib.UpdatedAt,
		}); err != nil {
			return fmt.Errorf("insert library: %w", err)
		}
		for _, p := range lib.Paths {
			if err := qtx.InsertLibraryPath(ctx, sqlc.InsertLibraryPathParams{
				LibraryID: lib.ID, Path: p,
			}); err != nil {
				return fmt.Errorf("insert library path: %w", err)
			}
		}
		if err := qtx.GrantPrimaryAdminLibraryAccess(ctx, lib.ID); err != nil {
			return fmt.Errorf("grant primary admin access: %w", err)
		}
		return tx.Commit()
	}

	qtx := r.pq.WithTx(tx)
	if err := qtx.InsertLibrary(ctx, sqlc_pg.InsertLibraryParams{
		ID:              lib.ID,
		Name:            lib.Name,
		ContentType:     lib.ContentType,
		ScanMode:        lib.ScanMode,
		ScanInterval:    nullableString(lib.ScanInterval),
		M3uUrl:          nullableString(lib.M3UURL),
		EpgUrl:          nullableString(lib.EPGURL),
		RefreshInterval: nullableString(lib.RefreshInterval),
		LanguageFilter:  lib.LanguageFilter,
		TlsInsecure:     int32(boolToInt64(lib.TLSInsecure)),
		CreatedAt:       lib.CreatedAt,
		UpdatedAt:       lib.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("insert library: %w", err)
	}
	for _, p := range lib.Paths {
		if err := qtx.InsertLibraryPath(ctx, sqlc_pg.InsertLibraryPathParams{
			LibraryID: lib.ID, Path: p,
		}); err != nil {
			return fmt.Errorf("insert library path: %w", err)
		}
	}
	if err := qtx.GrantPrimaryAdminLibraryAccess(ctx, lib.ID); err != nil {
		return fmt.Errorf("grant primary admin access: %w", err)
	}
	return tx.Commit()
}

func (r *LibraryRepository) GetByID(ctx context.Context, id string) (*Library, error) {
	if r.useSQLite() {
		row, err := r.sq.GetLibraryByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("library %s: %w", id, domain.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get library %s: %w", id, err)
		}
		paths, err := r.sq.ListPathsByLibrary(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get library paths: %w", err)
		}
		lib := libraryFromSqliteGetRow(row)
		lib.Paths = paths
		return &lib, nil
	}
	row, err := r.pq.GetLibraryByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("library %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get library %s: %w", id, err)
	}
	paths, err := r.pq.ListPathsByLibrary(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get library paths: %w", err)
	}
	lib := libraryFromPgGetRow(row)
	lib.Paths = paths
	return &lib, nil
}

func (r *LibraryRepository) List(ctx context.Context) ([]*Library, error) {
	var libs []*Library
	if r.useSQLite() {
		rows, err := r.sq.ListLibraries(ctx)
		if err != nil {
			return nil, fmt.Errorf("list libraries: %w", err)
		}
		libs = make([]*Library, len(rows))
		for i, row := range rows {
			lib := libraryFromSqliteListRow(row)
			libs[i] = &lib
		}
	} else {
		rows, err := r.pq.ListLibraries(ctx)
		if err != nil {
			return nil, fmt.Errorf("list libraries: %w", err)
		}
		libs = make([]*Library, len(rows))
		for i, row := range rows {
			lib := libraryFromPgListRow(row)
			libs[i] = &lib
		}
	}
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

	if r.useSQLite() {
		qtx := r.sq.WithTx(tx)
		n, err := qtx.UpdateLibrary(ctx, sqlc.UpdateLibraryParams{
			Name:            lib.Name,
			ContentType:     lib.ContentType,
			ScanMode:        lib.ScanMode,
			ScanInterval:    nullableString(lib.ScanInterval),
			M3uUrl:          nullableString(lib.M3UURL),
			EpgUrl:          nullableString(lib.EPGURL),
			RefreshInterval: nullableString(lib.RefreshInterval),
			LanguageFilter:  lib.LanguageFilter,
			TlsInsecure:     boolToInt64(lib.TLSInsecure),
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
			if err := qtx.InsertLibraryPath(ctx, sqlc.InsertLibraryPathParams{
				LibraryID: lib.ID, Path: p,
			}); err != nil {
				return fmt.Errorf("insert library path: %w", err)
			}
		}
		return tx.Commit()
	}

	qtx := r.pq.WithTx(tx)
	n, err := qtx.UpdateLibrary(ctx, sqlc_pg.UpdateLibraryParams{
		Name:            lib.Name,
		ContentType:     lib.ContentType,
		ScanMode:        lib.ScanMode,
		ScanInterval:    nullableString(lib.ScanInterval),
		M3uUrl:          nullableString(lib.M3UURL),
		EpgUrl:          nullableString(lib.EPGURL),
		RefreshInterval: nullableString(lib.RefreshInterval),
		LanguageFilter:  lib.LanguageFilter,
		TlsInsecure:     int32(boolToInt64(lib.TLSInsecure)),
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
		if err := qtx.InsertLibraryPath(ctx, sqlc_pg.InsertLibraryPathParams{
			LibraryID: lib.ID, Path: p,
		}); err != nil {
			return fmt.Errorf("insert library path: %w", err)
		}
	}
	return tx.Commit()
}

func (r *LibraryRepository) Delete(ctx context.Context, id string) error {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.DeleteLibrary(ctx, id)
	} else {
		n, err = r.pq.DeleteLibrary(ctx, id)
	}
	if err != nil {
		return fmt.Errorf("delete library: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("library %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

func (r *LibraryRepository) GrantAccess(ctx context.Context, userID, libraryID string) error {
	if r.useSQLite() {
		if err := r.sq.GrantLibraryAccess(ctx, sqlc.GrantLibraryAccessParams{
			UserID: userID, LibraryID: libraryID,
		}); err != nil {
			return fmt.Errorf("grant library access: %w", err)
		}
		return nil
	}
	if err := r.pq.GrantLibraryAccess(ctx, sqlc_pg.GrantLibraryAccessParams{
		UserID: userID, LibraryID: libraryID,
	}); err != nil {
		return fmt.Errorf("grant library access: %w", err)
	}
	return nil
}

func (r *LibraryRepository) RevokeAccess(ctx context.Context, userID, libraryID string) error {
	if r.useSQLite() {
		if err := r.sq.RevokeLibraryAccess(ctx, sqlc.RevokeLibraryAccessParams{
			UserID: userID, LibraryID: libraryID,
		}); err != nil {
			return fmt.Errorf("revoke library access: %w", err)
		}
		return nil
	}
	if err := r.pq.RevokeLibraryAccess(ctx, sqlc_pg.RevokeLibraryAccessParams{
		UserID: userID, LibraryID: libraryID,
	}); err != nil {
		return fmt.Errorf("revoke library access: %w", err)
	}
	return nil
}

func (r *LibraryRepository) ListAccessByUser(ctx context.Context, userID string) ([]string, error) {
	var (
		ids []string
		err error
	)
	if r.useSQLite() {
		ids, err = r.sq.ListLibraryAccessByUser(ctx, userID)
	} else {
		ids, err = r.pq.ListLibraryAccessByUser(ctx, userID)
	}
	if err != nil {
		return nil, fmt.Errorf("list library access by user: %w", err)
	}
	return ids, nil
}

func (r *LibraryRepository) ReplaceAccess(ctx context.Context, userID string, libraryIDs []string) error {
	desired := make(map[string]struct{}, len(libraryIDs))
	for _, id := range libraryIDs {
		desired[id] = struct{}{}
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if r.useSQLite() {
		qtx := r.sq.WithTx(tx)
		current, err := qtx.ListLibraryAccessByUser(ctx, userID)
		if err != nil {
			return fmt.Errorf("list current access: %w", err)
		}
		currentSet := make(map[string]struct{}, len(current))
		for _, id := range current {
			currentSet[id] = struct{}{}
		}
		for id := range desired {
			if _, ok := currentSet[id]; ok {
				continue
			}
			if err := qtx.GrantLibraryAccess(ctx, sqlc.GrantLibraryAccessParams{
				UserID: userID, LibraryID: id,
			}); err != nil {
				return fmt.Errorf("grant library access: %w", err)
			}
		}
		for _, id := range current {
			if _, ok := desired[id]; ok {
				continue
			}
			if err := qtx.RevokeLibraryAccess(ctx, sqlc.RevokeLibraryAccessParams{
				UserID: userID, LibraryID: id,
			}); err != nil {
				return fmt.Errorf("revoke library access: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit replace access: %w", err)
		}
		return nil
	}

	qtx := r.pq.WithTx(tx)
	current, err := qtx.ListLibraryAccessByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list current access: %w", err)
	}
	currentSet := make(map[string]struct{}, len(current))
	for _, id := range current {
		currentSet[id] = struct{}{}
	}
	for id := range desired {
		if _, ok := currentSet[id]; ok {
			continue
		}
		if err := qtx.GrantLibraryAccess(ctx, sqlc_pg.GrantLibraryAccessParams{
			UserID: userID, LibraryID: id,
		}); err != nil {
			return fmt.Errorf("grant library access: %w", err)
		}
	}
	for _, id := range current {
		if _, ok := desired[id]; ok {
			continue
		}
		if err := qtx.RevokeLibraryAccess(ctx, sqlc_pg.RevokeLibraryAccessParams{
			UserID: userID, LibraryID: id,
		}); err != nil {
			return fmt.Errorf("revoke library access: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace access: %w", err)
	}
	return nil
}

// UserHasAccess — raw SQL holdout (JOIN+COALESCE trips sqlc 1.31.1).
// Dialect-aware via rewritePlaceholders.
func (r *LibraryRepository) UserHasAccess(ctx context.Context, userID, libraryID string) (bool, error) {
	driver := DriverSQLite
	if !r.useSQLite() {
		driver = DriverPostgres
	}
	query := rewritePlaceholders(driver, `
		SELECT EXISTS (
			SELECT 1 FROM library_access la
			JOIN users u ON u.id = ?
			WHERE la.library_id = ?
			  AND la.user_id = COALESCE(u.parent_user_id, u.id)
		)`)
	var has int
	if err := r.db.QueryRowContext(ctx, query, userID, libraryID).Scan(&has); err != nil {
		return false, fmt.Errorf("check library access: %w", err)
	}
	return has == 1, nil
}

// ListForUser — raw SQL holdout (JOIN+COALESCE+ORDER BY trips sqlc).
// Dialect-aware via rewritePlaceholders.
func (r *LibraryRepository) ListForUser(ctx context.Context, userID string) ([]*Library, error) {
	driver := DriverSQLite
	if !r.useSQLite() {
		driver = DriverPostgres
	}
	query := rewritePlaceholders(driver, `
		SELECT l.id, l.name, l.content_type, l.scan_mode,
		       COALESCE(l.scan_interval, '6h') AS scan_interval,
		       COALESCE(l.m3u_url, '') AS m3u_url,
		       COALESCE(l.epg_url, '') AS epg_url,
		       COALESCE(l.refresh_interval, '24h') AS refresh_interval,
		       l.language_filter, l.tls_insecure,
		       l.created_at, l.updated_at
		FROM libraries l
		JOIN users u ON u.id = ?
		JOIN library_access la
		  ON la.library_id = l.id
		 AND la.user_id = COALESCE(u.parent_user_id, u.id)
		ORDER BY l.name`)
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list libraries for user: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var libs []*Library
	for rows.Next() {
		var lib Library
		var langFilter sql.NullString
		var tlsInsecure int64
		if err := rows.Scan(
			&lib.ID, &lib.Name, &lib.ContentType, &lib.ScanMode,
			&lib.ScanInterval, &lib.M3UURL, &lib.EPGURL,
			&lib.RefreshInterval, &langFilter, &tlsInsecure,
			&lib.CreatedAt, &lib.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan library row: %w", err)
		}
		if langFilter.Valid {
			lib.LanguageFilter = langFilter.String
		}
		lib.TLSInsecure = tlsInsecure != 0
		libs = append(libs, &lib)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.loadPaths(ctx, libs); err != nil {
		return nil, err
	}
	return libs, nil
}

// loadPaths fetches paths for all given libraries in a single query.
func (r *LibraryRepository) loadPaths(ctx context.Context, libs []*Library) error {
	if len(libs) == 0 {
		return nil
	}

	pathsByLib := make(map[string][]string)

	if r.useSQLite() {
		allPaths, err := r.sq.ListAllPaths(ctx)
		if err != nil {
			return fmt.Errorf("batch load library paths: %w", err)
		}
		for _, lp := range allPaths {
			pathsByLib[lp.LibraryID] = append(pathsByLib[lp.LibraryID], lp.Path)
		}
	} else {
		allPaths, err := r.pq.ListAllPaths(ctx)
		if err != nil {
			return fmt.Errorf("batch load library paths: %w", err)
		}
		for _, lp := range allPaths {
			pathsByLib[lp.LibraryID] = append(pathsByLib[lp.LibraryID], lp.Path)
		}
	}

	for _, lib := range libs {
		lib.Paths = pathsByLib[lib.ID]
	}
	return nil
}

// ── row mapping helpers ─────────────────────────────────────────────────

func libraryFromSqliteGetRow(r sqlc.GetLibraryByIDRow) Library {
	return Library{
		ID: r.ID, Name: r.Name, ContentType: r.ContentType, ScanMode: r.ScanMode,
		ScanInterval: r.ScanInterval, M3UURL: r.M3uUrl, EPGURL: r.EpgUrl,
		RefreshInterval: r.RefreshInterval, LanguageFilter: r.LanguageFilter,
		TLSInsecure: r.TlsInsecure != 0,
		CreatedAt:   r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func libraryFromSqliteListRow(r sqlc.ListLibrariesRow) Library {
	return Library{
		ID: r.ID, Name: r.Name, ContentType: r.ContentType, ScanMode: r.ScanMode,
		ScanInterval: r.ScanInterval, M3UURL: r.M3uUrl, EPGURL: r.EpgUrl,
		RefreshInterval: r.RefreshInterval, LanguageFilter: r.LanguageFilter,
		TLSInsecure: r.TlsInsecure != 0,
		CreatedAt:   r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func libraryFromPgGetRow(r sqlc_pg.GetLibraryByIDRow) Library {
	return Library{
		ID: r.ID, Name: r.Name, ContentType: r.ContentType, ScanMode: r.ScanMode,
		ScanInterval: r.ScanInterval, M3UURL: r.M3uUrl, EPGURL: r.EpgUrl,
		RefreshInterval: r.RefreshInterval, LanguageFilter: r.LanguageFilter,
		TLSInsecure: r.TlsInsecure != 0,
		CreatedAt:   r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func libraryFromPgListRow(r sqlc_pg.ListLibrariesRow) Library {
	return Library{
		ID: r.ID, Name: r.Name, ContentType: r.ContentType, ScanMode: r.ScanMode,
		ScanInterval: r.ScanInterval, M3UURL: r.M3uUrl, EPGURL: r.EpgUrl,
		RefreshInterval: r.RefreshInterval, LanguageFilter: r.LanguageFilter,
		TLSInsecure: r.TlsInsecure != 0,
		CreatedAt:   r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// boolToInt64 maps a Go bool to the 0/1 the schema stores in
// tls_insecure (kept INTEGER not BOOLEAN cross-dialect for type
// parity — see migrations/postgres/017_library_tls_insecure.sql).
func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
