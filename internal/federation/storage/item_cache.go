package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/federation"
)

// UpsertCachedItems replaces all rows for (peer, library) with the
// provided batch in a single transaction. Concurrent readers see
// either the old set or the new set, never half-merged.
//
// Year is dialect-divergent (NullInt64 in SQLite, NullInt32 in
// Postgres) — branching at the bind site keeps the domain field a
// plain `int`.
func (r *Repository) UpsertCachedItems(ctx context.Context, peerID, libraryID string, items []*federation.SharedItem, at time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cache upsert tx: %w", err)
	}
	// Rollback after a successful Commit returns sql.ErrTxDone, which
	// is harmless; ignore it deliberately rather than wrap with
	// extra plumbing.
	defer func() { _ = tx.Rollback() }()

	if r.useSQLite() {
		qtx := r.sq.WithTx(tx)
		if err := qtx.DeleteCachedItemsForLibrary(ctx, sqlc.DeleteCachedItemsForLibraryParams{
			PeerID:    peerID,
			LibraryID: libraryID,
		}); err != nil {
			return fmt.Errorf("clear cache: %w", err)
		}
	} else {
		qtx := r.pq.WithTx(tx)
		if err := qtx.DeleteCachedItemsForLibrary(ctx, sqlc_pg.DeleteCachedItemsForLibraryParams{
			PeerID:    peerID,
			LibraryID: libraryID,
		}); err != nil {
			return fmt.Errorf("clear cache: %w", err)
		}
	}

	for _, it := range items {
		// sqlc 1.31.1 truncates the InsertCachedItem statement when
		// the 10+ placeholders combine with adjacent ORDER BY ...
		// COLLATE NOCASE queries in the same file (see
		// architecture-decisions.md). Raw SQL holdout keeps the
		// colour columns flowing without poking the parser bug.
		var yearArg any
		if it.Year != 0 {
			if r.useSQLite() {
				yearArg = sql.NullInt64{Int64: int64(it.Year), Valid: true}
			} else {
				yearArg = sql.NullInt32{Int32: int32(it.Year), Valid: true}
			}
		} else {
			if r.useSQLite() {
				yearArg = sql.NullInt64{}
			} else {
				yearArg = sql.NullInt32{}
			}
		}
		_, err := tx.ExecContext(ctx, r.insertCachedItemSQL,
			peerID, libraryID, it.ID, it.Type, it.Title,
			yearArg,
			nullableString(it.Overview),
			it.HasPoster,
			it.PosterColor, it.PosterColorMuted,
			at,
		)
		if err != nil {
			return fmt.Errorf("insert cached item %s: %w", it.ID, err)
		}
	}
	return tx.Commit()
}

// ListCachedItems reads the cache for (peer, library), paginated.
// Empty result is NOT an error — cache cold.
func (r *Repository) ListCachedItems(ctx context.Context, peerID, libraryID string, offset, limit int) (federation.CachedItemPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var (
		total    int64
		newestCa any
		err      error
	)
	if r.useSQLite() {
		header, herr := r.sq.CountAndNewestCachedItems(ctx, sqlc.CountAndNewestCachedItemsParams{
			PeerID:    peerID,
			LibraryID: libraryID,
		})
		if herr != nil {
			return federation.CachedItemPage{}, fmt.Errorf("count cached items: %w", herr)
		}
		total, newestCa = header.Total, header.NewestCachedAt
	} else {
		header, herr := r.pq.CountAndNewestCachedItems(ctx, sqlc_pg.CountAndNewestCachedItemsParams{
			PeerID:    peerID,
			LibraryID: libraryID,
		})
		if herr != nil {
			return federation.CachedItemPage{}, fmt.Errorf("count cached items: %w", herr)
		}
		total, newestCa = header.Total, header.NewestCachedAt
	}
	if total == 0 {
		return federation.CachedItemPage{Items: []*federation.SharedItem{}}, nil
	}

	// Raw SQL mirrors UpsertCachedItems: the sqlc ListCachedItems
	// query stays on the previously-working column set, and the
	// two colour columns ride a sibling SELECT here. Same reasoning
	// as in UpsertCachedItems re: the sqlc 1.31.1 parser bug. The
	// `COLLATE NOCASE`-style sort is dialect-substituted at
	// construction time via `caseInsensitiveSort`.
	rows, err := r.db.QueryContext(ctx, r.listCachedItemsSQL, peerID, libraryID, limit, offset)
	if err != nil {
		return federation.CachedItemPage{}, fmt.Errorf("list cached items: %w", err)
	}
	defer rows.Close()
	out := []*federation.SharedItem{}
	for rows.Next() {
		var it federation.SharedItem
		if err := rows.Scan(
			&it.ID, &it.Type, &it.Title,
			&it.Year, &it.Overview,
			&it.HasPoster, &it.PosterColor, &it.PosterColorMuted,
		); err != nil {
			return federation.CachedItemPage{}, fmt.Errorf("scan cached item: %w", err)
		}
		out = append(out, &it)
	}
	if err := rows.Err(); err != nil {
		return federation.CachedItemPage{}, err
	}
	// MAX(cached_at) is typed as interface{} by sqlc because the
	// aggregate could legitimately return NULL; coerce defensively.
	cachedAt := time.Time{}
	if t, ok := newestCa.(time.Time); ok {
		cachedAt = t
	}
	return federation.CachedItemPage{Items: out, Total: int(total), LastSync: cachedAt}, nil
}

func (r *Repository) PurgeCachedItemsForLibrary(ctx context.Context, peerID, libraryID string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteCachedItemsForLibrary(ctx, sqlc.DeleteCachedItemsForLibraryParams{
			PeerID:    peerID,
			LibraryID: libraryID,
		})
	} else {
		err = r.pq.DeleteCachedItemsForLibrary(ctx, sqlc_pg.DeleteCachedItemsForLibraryParams{
			PeerID:    peerID,
			LibraryID: libraryID,
		})
	}
	if err != nil {
		return fmt.Errorf("purge cache: %w", err)
	}
	return nil
}
