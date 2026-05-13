package db

import (
	"context"
	"database/sql"
	"fmt"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
)

// EpisodeSegmentKind enumerates the recognised segment types.
// Mirrored as a CHECK constraint at the DB layer (migration 037)
// so unknown values never make it past Replace().
type EpisodeSegmentKind string

const (
	EpisodeSegmentIntro EpisodeSegmentKind = "intro"
	EpisodeSegmentOutro EpisodeSegmentKind = "outro"
	EpisodeSegmentRecap EpisodeSegmentKind = "recap"
)

// EpisodeSegmentSource is the detector that produced the segment.
// 'chapter' is the only one wired today; 'fingerprint' is reserved
// for the audio-fingerprint detector and 'manual' for an admin
// override path that doesn't exist yet but will share this storage.
type EpisodeSegmentSource string

const (
	EpisodeSegmentSourceChapter     EpisodeSegmentSource = "chapter"
	EpisodeSegmentSourceFingerprint EpisodeSegmentSource = "fingerprint"
	EpisodeSegmentSourceManual      EpisodeSegmentSource = "manual"
)

// EpisodeSegment is one detected intro / outro / recap range.
//
// StartTicks and EndTicks use the same 10M-ticks-per-second encoding
// the rest of the schema speaks (chapters, items.duration_ticks).
// Confidence is 0..1 — chapter-title matches use 0.95 because a
// chapter literally titled "Intro" is essentially ground truth, but
// detectors that infer from waveform similarity will surface lower
// numbers and the player can decide whether to auto-show or hide
// the skip button accordingly.
type EpisodeSegment struct {
	ItemID     string
	Kind       EpisodeSegmentKind
	Source     EpisodeSegmentSource
	StartTicks int64
	EndTicks   int64
	Confidence float64
	DetectedAt int64
}

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
	source EpisodeSegmentSource,
	segments []EpisodeSegment,
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
func (r *EpisodeSegmentRepository) ListByItem(ctx context.Context, itemID string) ([]EpisodeSegment, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListEpisodeSegmentsByItem(ctx, itemID)
		if err != nil {
			return nil, fmt.Errorf("list segments: %w", err)
		}
		if len(rows) == 0 {
			return nil, nil
		}
		out := make([]EpisodeSegment, len(rows))
		for i, row := range rows {
			out[i] = EpisodeSegment{
				ItemID:     row.ItemID,
				Kind:       EpisodeSegmentKind(row.Kind),
				Source:     EpisodeSegmentSource(row.Source),
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
	out := make([]EpisodeSegment, len(rows))
	for i, row := range rows {
		out[i] = EpisodeSegment{
			ItemID:     row.ItemID,
			Kind:       EpisodeSegmentKind(row.Kind),
			Source:     EpisodeSegmentSource(row.Source),
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
