package db

import (
	"context"
	"database/sql"
	"fmt"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
)

// ChapterRepository — Pattern A dual-dialect. start_ticks/end_ticks are
// BIGINT in both schemas so the sqlc surface is structurally identical
// per backend; the only branch needed is which generated package to
// invoke.
type ChapterRepository struct {
	db *sql.DB
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewChapterRepository(driver string, database *sql.DB) *ChapterRepository {
	r := &ChapterRepository{db: database}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *ChapterRepository) useSQLite() bool { return r.sq != nil }

// Replace clears any chapters previously stored for the item and
// inserts the new set in order. The clear-then-insert pattern keeps
// the chapter set authoritative per scan: a re-encode that shifted
// markers replaces them cleanly without leaving phantom rows from
// the old timing.
//
// All inserts share a transaction so a partial write (e.g. caller
// passes 80 chapters and the 50th violates the PK) leaves the prior
// row set intact instead of half-overwritten.
func (r *ChapterRepository) Replace(ctx context.Context, itemID string, chapters []librarymodel.Chapter) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if r.useSQLite() {
		qtx := r.sq.WithTx(tx)
		if err := qtx.DeleteChaptersByItem(ctx, itemID); err != nil {
			return fmt.Errorf("delete chapters: %w", err)
		}
		for _, c := range chapters {
			if err := qtx.InsertChapter(ctx, sqlc.InsertChapterParams{
				ItemID:     itemID,
				StartTicks: c.StartTicks,
				EndTicks:   c.EndTicks,
				Title:      nullableString(c.Title),
				ImagePath:  nullableString(c.ImagePath),
			}); err != nil {
				return fmt.Errorf("insert chapter %d: %w", c.StartTicks, err)
			}
		}
	} else {
		qtx := r.pq.WithTx(tx)
		if err := qtx.DeleteChaptersByItem(ctx, itemID); err != nil {
			return fmt.Errorf("delete chapters: %w", err)
		}
		for _, c := range chapters {
			if err := qtx.InsertChapter(ctx, sqlc_pg.InsertChapterParams{
				ItemID:     itemID,
				StartTicks: c.StartTicks,
				EndTicks:   c.EndTicks,
				Title:      nullableString(c.Title),
				ImagePath:  nullableString(c.ImagePath),
			}); err != nil {
				return fmt.Errorf("insert chapter %d: %w", c.StartTicks, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// ListByItem returns chapters in start-time order. An item with no
// chapters returns a nil slice + nil error, the same shape callers
// already get from ListByItem on every other repo.
func (r *ChapterRepository) ListByItem(ctx context.Context, itemID string) ([]*librarymodel.Chapter, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListChaptersByItem(ctx, itemID)
		if err != nil {
			return nil, fmt.Errorf("list chapters: %w", err)
		}
		if len(rows) == 0 {
			return nil, nil
		}
		out := make([]*librarymodel.Chapter, len(rows))
		for i, row := range rows {
			out[i] = &librarymodel.Chapter{
				ItemID:     row.ItemID,
				StartTicks: row.StartTicks,
				EndTicks:   row.EndTicks,
				Title:      row.Title,
				ImagePath:  row.ImagePath,
			}
		}
		return out, nil
	}
	rows, err := r.pq.ListChaptersByItem(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("list chapters: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]*librarymodel.Chapter, len(rows))
	for i, row := range rows {
		out[i] = &librarymodel.Chapter{
			ItemID:     row.ItemID,
			StartTicks: row.StartTicks,
			EndTicks:   row.EndTicks,
			Title:      row.Title,
			ImagePath:  row.ImagePath,
		}
	}
	return out, nil
}
