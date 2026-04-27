package db

import (
	"context"
	"database/sql"
	"fmt"

	"hubplay/internal/db/sqlc"
)

// Chapter is one named segment within an item's playback timeline.
// Chapters power skip-intro / skip-credits affordances and the markers
// rendered on the seek bar; they're written by the scanner from
// `ffprobe -show_chapters` and by the IPTV/scheduled-jobs flow if it
// ever needs structured timeline data.
//
// Title and ImagePath are optional in the schema; the repository
// layer surfaces them as plain strings (empty when absent) so handlers
// don't have to guard nil pointers everywhere they iterate.
type Chapter struct {
	ItemID     string
	StartTicks int64
	EndTicks   int64
	Title      string
	ImagePath  string
}

type ChapterRepository struct {
	db *sql.DB
	q  *sqlc.Queries
}

func NewChapterRepository(database *sql.DB) *ChapterRepository {
	return &ChapterRepository{db: database, q: sqlc.New(database)}
}

// Replace clears any chapters previously stored for the item and
// inserts the new set in order. The clear-then-insert pattern keeps
// the chapter set authoritative per scan: a re-encode that shifted
// markers replaces them cleanly without leaving phantom rows from
// the old timing.
//
// All inserts share a transaction so a partial write (e.g. caller
// passes 80 chapters and the 50th violates the PK) leaves the prior
// row set intact instead of half-overwritten.
func (r *ChapterRepository) Replace(ctx context.Context, itemID string, chapters []Chapter) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	q := r.q.WithTx(tx)
	if err := q.DeleteChaptersByItem(ctx, itemID); err != nil {
		return fmt.Errorf("delete chapters: %w", err)
	}
	for _, c := range chapters {
		if err := q.InsertChapter(ctx, sqlc.InsertChapterParams{
			ItemID:     itemID,
			StartTicks: c.StartTicks,
			EndTicks:   c.EndTicks,
			Title:      sql.NullString{String: c.Title, Valid: c.Title != ""},
			ImagePath:  sql.NullString{String: c.ImagePath, Valid: c.ImagePath != ""},
		}); err != nil {
			return fmt.Errorf("insert chapter %d: %w", c.StartTicks, err)
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
func (r *ChapterRepository) ListByItem(ctx context.Context, itemID string) ([]*Chapter, error) {
	rows, err := r.q.ListChaptersByItem(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("list chapters: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]*Chapter, len(rows))
	for i, row := range rows {
		out[i] = &Chapter{
			ItemID:     row.ItemID,
			StartTicks: row.StartTicks,
			EndTicks:   row.EndTicks,
			Title:      row.Title,
			ImagePath:  row.ImagePath,
		}
	}
	return out, nil
}
