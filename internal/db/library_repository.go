package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Paths           []string // populated by GetByID/List
}

type LibraryRepository struct {
	db *sql.DB
}

func NewLibraryRepository(database *sql.DB) *LibraryRepository {
	return &LibraryRepository{db: database}
}

func (r *LibraryRepository) Create(ctx context.Context, lib *Library) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.ExecContext(ctx,
		`INSERT INTO libraries (id, name, content_type, scan_mode, scan_interval, m3u_url, epg_url, refresh_interval, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		lib.ID, lib.Name, lib.ContentType, lib.ScanMode, lib.ScanInterval,
		lib.M3UURL, lib.EPGURL, lib.RefreshInterval, lib.CreatedAt, lib.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert library: %w", err)
	}

	for _, p := range lib.Paths {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO library_paths (library_id, path) VALUES (?, ?)`, lib.ID, p,
		)
		if err != nil {
			return fmt.Errorf("insert library path: %w", err)
		}
	}

	return tx.Commit()
}

func (r *LibraryRepository) GetByID(ctx context.Context, id string) (*Library, error) {
	lib := &Library{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, content_type, scan_mode, COALESCE(scan_interval,'6h'),
		        COALESCE(m3u_url,''), COALESCE(epg_url,''), COALESCE(refresh_interval,'24h'),
		        created_at, updated_at
		 FROM libraries WHERE id = ?`, id,
	).Scan(&lib.ID, &lib.Name, &lib.ContentType, &lib.ScanMode, &lib.ScanInterval,
		&lib.M3UURL, &lib.EPGURL, &lib.RefreshInterval, &lib.CreatedAt, &lib.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("library %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get library %s: %w", id, err)
	}

	paths, err := r.getPaths(ctx, id)
	if err != nil {
		return nil, err
	}
	lib.Paths = paths
	return lib, nil
}

func (r *LibraryRepository) List(ctx context.Context) ([]*Library, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, content_type, scan_mode, COALESCE(scan_interval,'6h'),
		        COALESCE(m3u_url,''), COALESCE(epg_url,''), COALESCE(refresh_interval,'24h'),
		        created_at, updated_at
		 FROM libraries ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list libraries: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var libs []*Library
	for rows.Next() {
		lib := &Library{}
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.ContentType, &lib.ScanMode, &lib.ScanInterval,
			&lib.M3UURL, &lib.EPGURL, &lib.RefreshInterval, &lib.CreatedAt, &lib.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan library: %w", err)
		}
		libs = append(libs, lib)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Batch-load all paths in a single query instead of N+1 queries.
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

	res, err := tx.ExecContext(ctx,
		`UPDATE libraries SET name = ?, content_type = ?, scan_mode = ?, scan_interval = ?,
		        m3u_url = ?, epg_url = ?, refresh_interval = ?, updated_at = ?
		 WHERE id = ?`,
		lib.Name, lib.ContentType, lib.ScanMode, lib.ScanInterval,
		lib.M3UURL, lib.EPGURL, lib.RefreshInterval, lib.UpdatedAt, lib.ID,
	)
	if err != nil {
		return fmt.Errorf("update library: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("library %s: %w", lib.ID, domain.ErrNotFound)
	}

	// Replace paths
	if _, err := tx.ExecContext(ctx, `DELETE FROM library_paths WHERE library_id = ?`, lib.ID); err != nil {
		return fmt.Errorf("delete library paths: %w", err)
	}
	for _, p := range lib.Paths {
		if _, err := tx.ExecContext(ctx, `INSERT INTO library_paths (library_id, path) VALUES (?, ?)`, lib.ID, p); err != nil {
			return fmt.Errorf("insert library path: %w", err)
		}
	}

	return tx.Commit()
}

func (r *LibraryRepository) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM libraries WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete library: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("library %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

func (r *LibraryRepository) getPaths(ctx context.Context, libraryID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT path FROM library_paths WHERE library_id = ? ORDER BY path`, libraryID,
	)
	if err != nil {
		return nil, fmt.Errorf("get library paths: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan path: %w", err)
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// loadPaths fetches paths for all given libraries in a single query
// and assigns them to the corresponding Library structs.
func (r *LibraryRepository) loadPaths(ctx context.Context, libs []*Library) error {
	if len(libs) == 0 {
		return nil
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT library_id, path FROM library_paths ORDER BY library_id, path`,
	)
	if err != nil {
		return fmt.Errorf("batch load library paths: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	pathsByLib := make(map[string][]string)
	for rows.Next() {
		var libID, path string
		if err := rows.Scan(&libID, &path); err != nil {
			return fmt.Errorf("scan library path: %w", err)
		}
		pathsByLib[libID] = append(pathsByLib[libID], path)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, lib := range libs {
		lib.Paths = pathsByLib[lib.ID]
	}
	return nil
}

// GrantAccess gives a user access to a library.
func (r *LibraryRepository) GrantAccess(ctx context.Context, userID, libraryID string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO library_access (user_id, library_id) VALUES (?, ?)`,
		userID, libraryID,
	)
	if err != nil {
		return fmt.Errorf("grant library access: %w", err)
	}
	return nil
}

// RevokeAccess removes a user's access to a library.
func (r *LibraryRepository) RevokeAccess(ctx context.Context, userID, libraryID string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM library_access WHERE user_id = ? AND library_id = ?`,
		userID, libraryID,
	)
	if err != nil {
		return fmt.Errorf("revoke library access: %w", err)
	}
	return nil
}

// ListForUser returns libraries a user has access to. If empty, all are accessible.
func (r *LibraryRepository) ListForUser(ctx context.Context, userID string) ([]*Library, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT l.id, l.name, l.content_type, l.scan_mode, COALESCE(l.scan_interval,'6h'),
		        COALESCE(l.m3u_url,''), COALESCE(l.epg_url,''), COALESCE(l.refresh_interval,'24h'),
		        l.created_at, l.updated_at
		 FROM libraries l
		 LEFT JOIN library_access la ON la.library_id = l.id AND la.user_id = ?
		 WHERE la.user_id IS NOT NULL
		    OR NOT EXISTS (SELECT 1 FROM library_access WHERE library_id = l.id)
		 ORDER BY l.name`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list libraries for user: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var libs []*Library
	for rows.Next() {
		lib := &Library{}
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.ContentType, &lib.ScanMode, &lib.ScanInterval,
			&lib.M3UURL, &lib.EPGURL, &lib.RefreshInterval, &lib.CreatedAt, &lib.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan library: %w", err)
		}
		libs = append(libs, lib)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Batch-load all paths in a single query instead of N+1 queries.
	if err := r.loadPaths(ctx, libs); err != nil {
		return nil, err
	}
	return libs, nil
}
