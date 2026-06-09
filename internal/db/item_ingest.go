package db

import (
	"context"
	"database/sql"
	"fmt"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	librarymodel "hubplay/internal/library/model"
)

// sqliteCreateItemParams builds the SQLite CreateItem params from a
// domain Item. Extracted so Create and the transactional IngestItem
// share one definition and can't drift.
func sqliteCreateItemParams(item *librarymodel.Item) sqlc.CreateItemParams {
	return sqlc.CreateItemParams{
		ID:              item.ID,
		LibraryID:       item.LibraryID,
		ParentID:        nullableString(item.ParentID),
		Type:            item.Type,
		Title:           item.Title,
		SortTitle:       item.SortTitle,
		OriginalTitle:   nullableString(item.OriginalTitle),
		Year:            sql.NullInt64{Int64: int64(item.Year), Valid: true},
		Path:            nullableString(item.Path),
		Size:            sql.NullInt64{Int64: item.Size, Valid: true},
		DurationTicks:   sql.NullInt64{Int64: item.DurationTicks, Valid: true},
		Container:       nullableString(item.Container),
		Fingerprint:     nullableString(item.Fingerprint),
		SeasonNumber:    nullableIntPtr(item.SeasonNumber),
		EpisodeNumber:   nullableIntPtr(item.EpisodeNumber),
		CommunityRating: nullableFloat64Ptr(item.CommunityRating),
		ContentRating:   nullableString(item.ContentRating),
		PremiereDate:    nullableTimePtr(item.PremiereDate),
		AddedAt:         item.AddedAt,
		UpdatedAt:       item.UpdatedAt,
		IsAvailable:     item.IsAvailable,
	}
}

// pgCreateItemParams is the Postgres mirror of sqliteCreateItemParams.
func pgCreateItemParams(item *librarymodel.Item) sqlc_pg.CreateItemParams {
	return sqlc_pg.CreateItemParams{
		ID:              item.ID,
		LibraryID:       item.LibraryID,
		ParentID:        nullableString(item.ParentID),
		Type:            item.Type,
		Title:           item.Title,
		SortTitle:       item.SortTitle,
		OriginalTitle:   nullableString(item.OriginalTitle),
		Year:            sql.NullInt32{Int32: int32(item.Year), Valid: true},
		Path:            nullableString(item.Path),
		Size:            sql.NullInt64{Int64: item.Size, Valid: true},
		DurationTicks:   sql.NullInt64{Int64: item.DurationTicks, Valid: true},
		Container:       nullableString(item.Container),
		Fingerprint:     nullableString(item.Fingerprint),
		SeasonNumber:    nullableIntPtrInt32(item.SeasonNumber),
		EpisodeNumber:   nullableIntPtrInt32(item.EpisodeNumber),
		CommunityRating: nullableFloat64Ptr(item.CommunityRating),
		ContentRating:   nullableString(item.ContentRating),
		PremiereDate:    nullableTimePtr(item.PremiereDate),
		AddedAt:         item.AddedAt,
		UpdatedAt:       item.UpdatedAt,
		IsAvailable:     item.IsAvailable,
	}
}

// IngestItem writes a freshly-scanned item together with its media
// streams and chapters in a SINGLE transaction.
//
// The scanner previously issued Create + ReplaceForItem + Replace as
// three independently auto-committed statements, so each new file cost
// ~3 WAL fsyncs and three separate acquisitions of SQLite's single write
// lock — the dominant cost of indexing a large library. Folding them into
// one transaction drops that to a single fsync / lock-hold per file and
// makes the write atomic: a malformed stream/chapter row can no longer
// leave an item half-populated (previously the item landed and the
// stream rows were silently dropped with a warning).
//
// `streams` and `chapters` may be empty. Intended for NEW items (no
// pre-existing children to clear); the update path keeps using the
// per-table Replace methods.
func (r *ItemRepository) IngestItem(ctx context.Context, item *librarymodel.Item, streams []*librarymodel.MediaStream, chapters []librarymodel.Chapter) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if r.useSQLite() {
		qtx := r.sq.WithTx(tx)
		if err := qtx.CreateItem(ctx, sqliteCreateItemParams(item)); err != nil {
			return fmt.Errorf("create item: %w", err)
		}
		for _, s := range streams {
			if err := qtx.InsertMediaStream(ctx, mediaStreamToSqliteInsertParams(s)); err != nil {
				return fmt.Errorf("insert stream %d: %w", s.StreamIndex, err)
			}
		}
		for _, c := range chapters {
			if err := qtx.InsertChapter(ctx, sqlc.InsertChapterParams{
				ItemID:     item.ID,
				StartTicks: c.StartTicks,
				EndTicks:   c.EndTicks,
				Title:      nullableString(c.Title),
				ImagePath:  nullableString(c.ImagePath),
			}); err != nil {
				return fmt.Errorf("insert chapter %d: %w", c.StartTicks, err)
			}
		}
		return tx.Commit()
	}

	qtx := r.pq.WithTx(tx)
	if err := qtx.CreateItem(ctx, pgCreateItemParams(item)); err != nil {
		return fmt.Errorf("create item: %w", err)
	}
	for _, s := range streams {
		if err := qtx.InsertMediaStream(ctx, mediaStreamToPgInsertParams(s)); err != nil {
			return fmt.Errorf("insert stream %d: %w", s.StreamIndex, err)
		}
	}
	for _, c := range chapters {
		if err := qtx.InsertChapter(ctx, sqlc_pg.InsertChapterParams{
			ItemID:     item.ID,
			StartTicks: c.StartTicks,
			EndTicks:   c.EndTicks,
			Title:      nullableString(c.Title),
			ImagePath:  nullableString(c.ImagePath),
		}); err != nil {
			return fmt.Errorf("insert chapter %d: %w", c.StartTicks, err)
		}
	}
	return tx.Commit()
}
