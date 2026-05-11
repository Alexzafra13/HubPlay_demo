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
	// TLSInsecure, when true, makes the M3U / EPG fetcher skip TLS
	// certificate verification for THIS library's HTTPS URLs. Off by
	// default. Per-library so a typo can't weaken every fetch the
	// server makes; only the playlist/EPG fetch path honours the
	// flag — the stream proxy keeps strict verification regardless.
	// Provided to handle the IPTV reality that many providers ship
	// expired Let's Encrypt or self-signed certs (every comparable
	// tool — Threadfin, xTeVe, Tuliprox — exposes the same toggle).
	TLSInsecure     bool
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
		TlsInsecure:     boolToInt64(lib.TLSInsecure),
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

// GrantAccess gives a top-level user (and all its profiles) access
// to a library. Profiles inherit automatically through the
// COALESCE(parent_user_id, id) predicate, so the handler MUST pass
// the parent's id — passing a profile id would create an orphan row
// the access predicate never consults. The admin endpoint enforces
// this by resolving the user to its top-level before reaching here.
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

// RevokeAccess removes a top-level user's access to a library. All
// profiles below that user lose access in the same operation.
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

// ListAccessByUser returns the library_ids the given user has explicit
// grants for. Admin-only surface: bypasses the strict profile-inheritance
// predicate so the admin UI can paint the matrix exactly as stored. The
// caller must pass a top-level user id; passing a profile id returns the
// empty slice (grants always target the parent, per ADR-014).
func (r *LibraryRepository) ListAccessByUser(ctx context.Context, userID string) ([]string, error) {
	ids, err := r.q.ListLibraryAccessByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list library access by user: %w", err)
	}
	return ids, nil
}

// ReplaceAccess overwrites a top-level user's grant set with the given
// libraryIDs in a single transaction. Idempotent: missing libraries get
// granted, extra grants get revoked, untouched rows stay. The caller is
// responsible for resolving the user to its top-level id (ADR-014) and
// for validating that every libraryID exists; ReplaceAccess only enforces
// uniqueness within libraryIDs.
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

	qtx := r.q.WithTx(tx)

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
			UserID:    userID,
			LibraryID: id,
		}); err != nil {
			return fmt.Errorf("grant library access: %w", err)
		}
	}
	for _, id := range current {
		if _, ok := desired[id]; ok {
			continue
		}
		if err := qtx.RevokeLibraryAccess(ctx, sqlc.RevokeLibraryAccessParams{
			UserID:    userID,
			LibraryID: id,
		}); err != nil {
			return fmt.Errorf("revoke library access: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace access: %w", err)
	}
	return nil
}

// UserHasAccess reports whether userID is allowed to access libraryID.
// Modelo strict post-migración 040: necesita grant explícito en
// library_access. Los profiles (parent_user_id != NULL) heredan el
// grant de su parent vía COALESCE — el admin solo necesita gestionar
// accesos del top-level user y todos sus miembros los reciben.
//
// Kept as raw SQL: el JOIN+COALESCE no es trivial de pintar con sqlc
// 1.31.1 y la versión anterior ya era raw SQL por la misma razón.
func (r *LibraryRepository) UserHasAccess(ctx context.Context, userID, libraryID string) (bool, error) {
	const query = `
		SELECT EXISTS (
			SELECT 1 FROM library_access la
			JOIN users u ON u.id = ?
			WHERE la.library_id = ?
			  AND la.user_id = COALESCE(u.parent_user_id, u.id)
		)`
	var has int
	if err := r.db.QueryRowContext(ctx, query, userID, libraryID).Scan(&has); err != nil {
		return false, fmt.Errorf("check library access: %w", err)
	}
	return has == 1, nil
}

// ListForUser returns libraries a user has access to under the strict
// (post-migración 040) modelo: necesita grant explícito en
// library_access apuntando al top-level user. Los profiles
// (parent_user_id != NULL) ven lo mismo que su parent vía COALESCE.
//
// Raw SQL holdout: sqlc 1.31.1 trunca el ORDER BY cuando el JOIN
// usa COALESCE en la condición. Mismo precedente que UserHasAccess y
// las queries de federation poster colours.
func (r *LibraryRepository) ListForUser(ctx context.Context, userID string) ([]*Library, error) {
	const query = `
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
		ORDER BY l.name`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list libraries for user: %w", err)
	}
	defer rows.Close()

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
		TLSInsecure:     r.TlsInsecure != 0,
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
		TLSInsecure:     r.TlsInsecure != 0,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

// boolToInt64 maps a Go bool to the SQLite-friendly 0/1 the schema
// stores in tls_insecure (the only boolean column not modelled as
// NUMERIC nullable). Kept here so future toggles can reuse it.
func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
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

