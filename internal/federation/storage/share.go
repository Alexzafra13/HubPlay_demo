package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"hubplay/internal/db"
	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/federation"
)

func (r *Repository) UpsertLibraryShare(ctx context.Context, s *federation.LibraryShare) error {
	var err error
	if r.useSQLite() {
		err = r.sq.UpsertLibraryShare(ctx, sqlc.UpsertLibraryShareParams{
			ID:          s.ID,
			PeerID:      s.PeerID,
			LibraryID:   s.LibraryID,
			CanBrowse:   s.CanBrowse,
			CanPlay:     s.CanPlay,
			CanDownload: s.CanDownload,
			CanLivetv:   s.CanLiveTV,
			ExtraScopes: nullableString(s.ExtraScopes),
			CreatedBy:   s.CreatedByUserID,
			CreatedAt:   s.CreatedAt,
		})
	} else {
		err = r.pq.UpsertLibraryShare(ctx, sqlc_pg.UpsertLibraryShareParams{
			ID:          s.ID,
			PeerID:      s.PeerID,
			LibraryID:   s.LibraryID,
			CanBrowse:   s.CanBrowse,
			CanPlay:     s.CanPlay,
			CanDownload: s.CanDownload,
			CanLivetv:   s.CanLiveTV,
			ExtraScopes: nullableString(s.ExtraScopes),
			CreatedBy:   s.CreatedByUserID,
			CreatedAt:   s.CreatedAt,
		})
	}
	if err != nil {
		return fmt.Errorf("upsert library share: %w", err)
	}
	return nil
}

func (r *Repository) DeleteLibraryShare(ctx context.Context, peerID, shareID string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteLibraryShare(ctx, sqlc.DeleteLibraryShareParams{
			PeerID: peerID,
			ID:     shareID,
		})
	} else {
		err = r.pq.DeleteLibraryShare(ctx, sqlc_pg.DeleteLibraryShareParams{
			PeerID: peerID,
			ID:     shareID,
		})
	}
	if err != nil {
		return fmt.Errorf("delete library share: %w", err)
	}
	return nil
}

func (r *Repository) GetLibraryShare(ctx context.Context, peerID, libraryID string) (*federation.LibraryShare, error) {
	if r.useSQLite() {
		row, err := r.sq.GetLibraryShare(ctx, sqlc.GetLibraryShareParams{
			PeerID:    peerID,
			LibraryID: libraryID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get library share: %w", err)
		}
		return libraryShareFromSqliteRow(row), nil
	}
	row, err := r.pq.GetLibraryShare(ctx, sqlc_pg.GetLibraryShareParams{
		PeerID:    peerID,
		LibraryID: libraryID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get library share: %w", err)
	}
	return libraryShareFromPgRow(row), nil
}

func (r *Repository) ListSharesByPeer(ctx context.Context, peerID string) ([]*federation.LibraryShare, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListSharesByPeer(ctx, peerID)
		if err != nil {
			return nil, fmt.Errorf("list shares by peer: %w", err)
		}
		out := make([]*federation.LibraryShare, 0, len(rows))
		for i := range rows {
			out = append(out, libraryShareFromSqliteRow(rows[i]))
		}
		return out, nil
	}
	rows, err := r.pq.ListSharesByPeer(ctx, peerID)
	if err != nil {
		return nil, fmt.Errorf("list shares by peer: %w", err)
	}
	out := make([]*federation.LibraryShare, 0, len(rows))
	for i := range rows {
		out = append(out, libraryShareFromPgRow(rows[i]))
	}
	return out, nil
}

func libraryShareFromSqliteRow(row sqlc.FederationLibraryShare) *federation.LibraryShare {
	s := &federation.LibraryShare{
		ID:              row.ID,
		PeerID:          row.PeerID,
		LibraryID:       row.LibraryID,
		CanBrowse:       row.CanBrowse,
		CanPlay:         row.CanPlay,
		CanDownload:     row.CanDownload,
		CanLiveTV:       row.CanLivetv,
		CreatedByUserID: row.CreatedBy,
		CreatedAt:       row.CreatedAt,
	}
	if row.ExtraScopes.Valid {
		s.ExtraScopes = row.ExtraScopes.String
	}
	return s
}

func libraryShareFromPgRow(row sqlc_pg.FederationLibraryShare) *federation.LibraryShare {
	s := &federation.LibraryShare{
		ID:              row.ID,
		PeerID:          row.PeerID,
		LibraryID:       row.LibraryID,
		CanBrowse:       row.CanBrowse,
		CanPlay:         row.CanPlay,
		CanDownload:     row.CanDownload,
		CanLiveTV:       row.CanLivetv,
		CreatedByUserID: row.CreatedBy,
		CreatedAt:       row.CreatedAt,
	}
	if row.ExtraScopes.Valid {
		s.ExtraScopes = row.ExtraScopes.String
	}
	return s
}

func (r *Repository) ListSharedLibrariesForPeer(ctx context.Context, peerID string) ([]*federation.SharedLibrary, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListSharedLibrariesForPeer(ctx, peerID)
		if err != nil {
			return nil, fmt.Errorf("list shared libraries: %w", err)
		}
		out := make([]*federation.SharedLibrary, 0, len(rows))
		for _, row := range rows {
			out = append(out, &federation.SharedLibrary{
				ID:          row.ID,
				Name:        row.Name,
				ContentType: row.ContentType,
				Scopes: federation.ShareScopes{
					CanBrowse:   row.CanBrowse,
					CanPlay:     row.CanPlay,
					CanDownload: row.CanDownload,
					CanLiveTV:   row.CanLivetv,
				},
			})
		}
		return out, nil
	}
	rows, err := r.pq.ListSharedLibrariesForPeer(ctx, peerID)
	if err != nil {
		return nil, fmt.Errorf("list shared libraries: %w", err)
	}
	out := make([]*federation.SharedLibrary, 0, len(rows))
	for _, row := range rows {
		out = append(out, &federation.SharedLibrary{
			ID:          row.ID,
			Name:        row.Name,
			ContentType: row.ContentType,
			Scopes: federation.ShareScopes{
				CanBrowse:   row.CanBrowse,
				CanPlay:     row.CanPlay,
				CanDownload: row.CanDownload,
				CanLiveTV:   row.CanLivetv,
			},
		})
	}
	return out, nil
}

// ListSharedItems returns shared items in (peer, library), paginated.
// Caller must have already validated the share + scope; the SQL JOIN
// against federation_library_shares is defence in depth.
//
// PosterColor / PosterColorMuted are attached via a separate batch
// query rather than baked into the sqlc ListSharedItems statement —
// the sqlc 1.31.1 parser truncates `ORDER BY ... COLLATE NOCASE` on
// queries with extra COALESCE-over-subquery columns (see
// architecture-decisions.md). The batch query reads the same
// images.dominant_color* the inline subquery would have, so the
// behaviour is identical: empty strings when the item pre-dates
// migration 014 or extraction failed.
func (r *Repository) ListSharedItems(ctx context.Context, peerID, libraryID string, offset, limit int) ([]*federation.SharedItem, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var (
		total int64
		out   []*federation.SharedItem
		err   error
	)
	if r.useSQLite() {
		total, err = r.sq.CountSharedItems(ctx, sqlc.CountSharedItemsParams{
			LibraryID: libraryID,
			PeerID:    peerID,
		})
		if err != nil {
			return nil, 0, fmt.Errorf("count shared items: %w", err)
		}
		rows, lerr := r.sq.ListSharedItems(ctx, sqlc.ListSharedItemsParams{
			LibraryID: libraryID,
			PeerID:    peerID,
			Limit:     int64(limit),
			Offset:    int64(offset),
		})
		if lerr != nil {
			return nil, 0, fmt.Errorf("list shared items: %w", lerr)
		}
		out = make([]*federation.SharedItem, 0, len(rows))
		for _, row := range rows {
			out = append(out, &federation.SharedItem{
				ID:        row.ID,
				Type:      row.Type,
				Title:     row.Title,
				Year:      int(row.Year),
				Overview:  row.Overview,
				HasPoster: row.HasPoster,
			})
		}
	} else {
		total, err = r.pq.CountSharedItems(ctx, sqlc_pg.CountSharedItemsParams{
			LibraryID: libraryID,
			PeerID:    peerID,
		})
		if err != nil {
			return nil, 0, fmt.Errorf("count shared items: %w", err)
		}
		rows, lerr := r.pq.ListSharedItems(ctx, sqlc_pg.ListSharedItemsParams{
			LibraryID: libraryID,
			PeerID:    peerID,
			Limit:     int32(limit),
			Offset:    int32(offset),
		})
		if lerr != nil {
			return nil, 0, fmt.Errorf("list shared items: %w", lerr)
		}
		out = make([]*federation.SharedItem, 0, len(rows))
		for _, row := range rows {
			out = append(out, &federation.SharedItem{
				ID:        row.ID,
				Type:      row.Type,
				Title:     row.Title,
				Year:      int(row.Year),
				Overview:  row.Overview,
				HasPoster: row.HasPoster,
			})
		}
	}

	if err := r.attachPrimaryImageColors(ctx, out); err != nil {
		return nil, 0, fmt.Errorf("attach poster colours: %w", err)
	}
	return out, int(total), nil
}

// ListRecentSharedItems returns the most recently added items across
// every library shared with `peerID` (CanBrowse gate). Powers the
// consumer-side "Recently added on peers" rail: each paired peer
// answers with its top-N freshest titles and the consumer fan-out
// merges them. library_id is included on each row so the consumer
// can route a click into /peers/{peerID}/libraries/{libraryID}/items/{id}.
func (r *Repository) ListRecentSharedItems(ctx context.Context, peerID string, limit int) ([]*federation.SharedItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	var out []*federation.SharedItem
	if r.useSQLite() {
		rows, err := r.sq.ListRecentSharedItems(ctx, sqlc.ListRecentSharedItemsParams{
			PeerID: peerID,
			Limit:  int64(limit),
		})
		if err != nil {
			return nil, fmt.Errorf("list recent shared items: %w", err)
		}
		out = make([]*federation.SharedItem, 0, len(rows))
		for _, row := range rows {
			out = append(out, &federation.SharedItem{
				ID:        row.ID,
				Type:      row.Type,
				Title:     row.Title,
				Year:      int(row.Year),
				Overview:  row.Overview,
				HasPoster: row.HasPoster,
				LibraryID: row.LibraryID,
			})
		}
	} else {
		rows, err := r.pq.ListRecentSharedItems(ctx, sqlc_pg.ListRecentSharedItemsParams{
			PeerID: peerID,
			Limit:  int32(limit),
		})
		if err != nil {
			return nil, fmt.Errorf("list recent shared items: %w", err)
		}
		out = make([]*federation.SharedItem, 0, len(rows))
		for _, row := range rows {
			out = append(out, &federation.SharedItem{
				ID:        row.ID,
				Type:      row.Type,
				Title:     row.Title,
				Year:      int(row.Year),
				Overview:  row.Overview,
				HasPoster: row.HasPoster,
				LibraryID: row.LibraryID,
			})
		}
	}

	if err := r.attachPrimaryImageColors(ctx, out); err != nil {
		return nil, fmt.Errorf("attach poster colours: %w", err)
	}
	return out, nil
}

// attachPrimaryImageColors fills SharedItem.PosterColor and
// PosterColorMuted in place by batch-fetching primary-image swatches
// from the images table. Single query for the whole page, so no N+1.
//
// Lives here (raw SQL, not sqlc) because the natural place — a
// correlated subquery on the parent query — trips the sqlc 1.31.1
// parser when combined with ORDER BY ... COLLATE NOCASE. Empty
// `items` and items whose image has no extracted swatch are both
// no-ops (the field already defaults to "").
func (r *Repository) attachPrimaryImageColors(ctx context.Context, items []*federation.SharedItem) error {
	if len(items) == 0 {
		return nil
	}
	placeholders := make([]string, len(items))
	args := make([]any, len(items))
	for i, it := range items {
		placeholders[i] = "?"
		args[i] = it.ID
	}
	// BOOLEAN predicate `is_primary` (truthy) is portable across
	// dialects. The placeholder rewrite has to happen AFTER building
	// the IN list so the counter sees every `?`.
	q := db.RewritePlaceholders(r.driver, `SELECT item_id, dominant_color, dominant_color_muted
		      FROM images
		      WHERE type = 'primary' AND is_primary
		        AND item_id IN (`+strings.Join(placeholders, ",")+`)`)
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	colors := make(map[string][2]string, len(items))
	for rows.Next() {
		var itemID, vibrant, muted string
		if err := rows.Scan(&itemID, &vibrant, &muted); err != nil {
			return err
		}
		colors[itemID] = [2]string{vibrant, muted}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, it := range items {
		if c, ok := colors[it.ID]; ok {
			it.PosterColor = c[0]
			it.PosterColorMuted = c[1]
		}
	}
	return nil
}

// SearchSharedItems runs a full-text query across libraries the calling
// peer has CanBrowse on. Reuses the items_fts virtual table (SQLite)
// or the search_vector tsvector column (Postgres) that powers local
// search; ACL gate is the JOIN against federation_library_shares so a
// peer cannot match titles in libraries not shared with them.
//
// Implemented as raw SQL because sqlc parses neither FTS5 MATCH nor
// tsvector @@ to_tsquery. Same precedent as item_repository.go's
// List path. The query parameter is appended with '*' for prefix
// matching (SQLite); the Postgres variant runs through `toTSQueryPrefix`.
//
// Caller is expected to apply a sensible per-peer limit; the function
// caps at 100 defensively so a runaway query cannot stream a peer's
// whole catalog.
func (r *Repository) SearchSharedItems(ctx context.Context, peerID, query string, limit int) ([]*federation.SharedItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if query == "" {
		return []*federation.SharedItem{}, nil
	}

	var ftsParam string
	if r.useSQLite() {
		ftsParam = query + "*"
	} else {
		ftsParam = toTSQueryPrefix(query)
	}

	rows, err := r.db.QueryContext(ctx, r.searchSharedItemsSQL, peerID, ftsParam, limit)
	if err != nil {
		return nil, fmt.Errorf("search shared items: %w", err)
	}
	defer rows.Close()

	out := []*federation.SharedItem{}
	for rows.Next() {
		var it federation.SharedItem
		if err := rows.Scan(&it.ID, &it.Type, &it.Title, &it.Year, &it.Overview, &it.HasPoster, &it.LibraryID); err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}
		out = append(out, &it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.attachPrimaryImageColors(ctx, out); err != nil {
		return nil, fmt.Errorf("attach poster colours: %w", err)
	}
	return out, nil
}
