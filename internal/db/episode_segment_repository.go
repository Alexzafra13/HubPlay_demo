package db

import (
	"context"
	"database/sql"
	"fmt"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
)

// EpisodeSegmentRepository — Pattern A dual-dialect. All ticks /
// detected_at columns are BIGINT in both schemas so the per-backend
// params are structurally identical; the only branch needed is which
// generated package to invoke.
type EpisodeSegmentRepository struct {
	db *sql.DB
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewEpisodeSegmentRepository(driver string, database *sql.DB) *EpisodeSegmentRepository {
	r := &EpisodeSegmentRepository{db: database}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *EpisodeSegmentRepository) useSQLite() bool { return r.sq != nil }

// Replace clears every segment previously written by `source` for
// the item and inserts the new set in one transaction. Other sources'
// rows for the same item are not touched — that's the whole point of
// scoping the delete to (item_id, source) rather than item_id alone.
//
// Empty `segments` is a valid input: the previous source's rows go
// away and nothing replaces them. Useful for re-runs that newly
// decide there's no intro on this episode after all.
func (r *EpisodeSegmentRepository) Replace(
	ctx context.Context,
	itemID string,
	source librarymodel.EpisodeSegmentSource,
	segments []librarymodel.EpisodeSegment,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if r.useSQLite() {
		qtx := r.sq.WithTx(tx)
		if err := qtx.DeleteEpisodeSegmentsByItemAndSource(ctx, sqlc.DeleteEpisodeSegmentsByItemAndSourceParams{
			ItemID: itemID,
			Source: string(source),
		}); err != nil {
			return fmt.Errorf("delete prior segments: %w", err)
		}
		for _, s := range segments {
			if err := qtx.InsertEpisodeSegment(ctx, sqlc.InsertEpisodeSegmentParams{
				ItemID:     itemID,
				Kind:       string(s.Kind),
				Source:     string(source),
				StartTicks: s.StartTicks,
				EndTicks:   s.EndTicks,
				Confidence: s.Confidence,
				DetectedAt: s.DetectedAt,
			}); err != nil {
				return fmt.Errorf("insert segment %s/%s: %w", s.Kind, source, err)
			}
		}
	} else {
		qtx := r.pq.WithTx(tx)
		if err := qtx.DeleteEpisodeSegmentsByItemAndSource(ctx, sqlc_pg.DeleteEpisodeSegmentsByItemAndSourceParams{
			ItemID: itemID,
			Source: string(source),
		}); err != nil {
			return fmt.Errorf("delete prior segments: %w", err)
		}
		for _, s := range segments {
			if err := qtx.InsertEpisodeSegment(ctx, sqlc_pg.InsertEpisodeSegmentParams{
				ItemID:     itemID,
				Kind:       string(s.Kind),
				Source:     string(source),
				StartTicks: s.StartTicks,
				EndTicks:   s.EndTicks,
				Confidence: s.Confidence,
				DetectedAt: s.DetectedAt,
			}); err != nil {
				return fmt.Errorf("insert segment %s/%s: %w", s.Kind, source, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// ListByItem returns every segment recorded for the item in
// (kind, source) order. Empty result is (nil, nil), matching the
// shape the rest of the repo layer returns.
//
// When two sources have produced a segment of the same kind (e.g.
// chapter-derived and fingerprint-derived intros), both rows are
// returned. Resolution to a single "best" segment is the caller's
// job — the API handler picks the highest-confidence row per kind
// before serialising.
func (r *EpisodeSegmentRepository) ListByItem(ctx context.Context, itemID string) ([]librarymodel.EpisodeSegment, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListEpisodeSegmentsByItem(ctx, itemID)
		if err != nil {
			return nil, fmt.Errorf("list segments: %w", err)
		}
		if len(rows) == 0 {
			return nil, nil
		}
		out := make([]librarymodel.EpisodeSegment, len(rows))
		for i, row := range rows {
			out[i] = librarymodel.EpisodeSegment{
				ItemID:     row.ItemID,
				Kind:       librarymodel.EpisodeSegmentKind(row.Kind),
				Source:     librarymodel.EpisodeSegmentSource(row.Source),
				StartTicks: row.StartTicks,
				EndTicks:   row.EndTicks,
				Confidence: row.Confidence,
				DetectedAt: row.DetectedAt,
			}
		}
		return out, nil
	}
	rows, err := r.pq.ListEpisodeSegmentsByItem(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("list segments: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]librarymodel.EpisodeSegment, len(rows))
	for i, row := range rows {
		out[i] = librarymodel.EpisodeSegment{
			ItemID:     row.ItemID,
			Kind:       librarymodel.EpisodeSegmentKind(row.Kind),
			Source:     librarymodel.EpisodeSegmentSource(row.Source),
			StartTicks: row.StartTicks,
			EndTicks:   row.EndTicks,
			Confidence: row.Confidence,
			DetectedAt: row.DetectedAt,
		}
	}
	return out, nil
}

// DeleteByItem removes every segment for the item across all sources.
// Useful when the item is being deleted manually outside the FK
// cascade (the FK already wipes these rows when the items row goes
// away, but explicit cleanup matters for re-detect flows that want
// a clean slate).
func (r *EpisodeSegmentRepository) DeleteByItem(ctx context.Context, itemID string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteEpisodeSegmentsByItem(ctx, itemID)
	} else {
		err = r.pq.DeleteEpisodeSegmentsByItem(ctx, itemID)
	}
	if err != nil {
		return fmt.Errorf("delete segments: %w", err)
	}
	return nil
}
