package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
)

// ChannelOverride captures a hand-edited field that must survive an
// M3U refresh. Keyed by (library_id, stream_url) because channel IDs
// are regenerated on every refresh — see migration 009 for the
// rationale.
type ChannelOverride struct {
	LibraryID string
	StreamURL string
	TvgID     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChannelOverrideRepository — Pattern A dual-dialect. ApplyToLibrary
// still owns the multi-row transaction; the tx pattern branches per
// backend (same shape as ImageRepository.SetPrimary).
type ChannelOverrideRepository struct {
	db *sql.DB
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewChannelOverrideRepository(driver string, database *sql.DB) *ChannelOverrideRepository {
	r := &ChannelOverrideRepository{db: database}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *ChannelOverrideRepository) useSQLite() bool { return r.sq != nil }

// Upsert records an override. Idempotent: re-running with the same
// fields just bumps updated_at.
func (r *ChannelOverrideRepository) Upsert(ctx context.Context, o *ChannelOverride) error {
	now := time.Now().UTC()
	if o.CreatedAt.IsZero() {
		o.CreatedAt = now
	}
	o.UpdatedAt = now
	var err error
	if r.useSQLite() {
		err = r.sq.UpsertChannelOverride(ctx, sqlc.UpsertChannelOverrideParams{
			LibraryID: o.LibraryID,
			StreamUrl: o.StreamURL,
			TvgID:     o.TvgID,
			CreatedAt: o.CreatedAt,
			UpdatedAt: o.UpdatedAt,
		})
	} else {
		err = r.pq.UpsertChannelOverride(ctx, sqlc_pg.UpsertChannelOverrideParams{
			LibraryID: o.LibraryID,
			StreamUrl: o.StreamURL,
			TvgID:     o.TvgID,
			CreatedAt: o.CreatedAt,
			UpdatedAt: o.UpdatedAt,
		})
	}
	if err != nil {
		return fmt.Errorf("upsert channel override: %w", err)
	}
	return nil
}

// Delete clears an override by its PK. Idempotent.
func (r *ChannelOverrideRepository) Delete(ctx context.Context, libraryID, streamURL string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteChannelOverride(ctx, sqlc.DeleteChannelOverrideParams{
			LibraryID: libraryID, StreamUrl: streamURL,
		})
	} else {
		err = r.pq.DeleteChannelOverride(ctx, sqlc_pg.DeleteChannelOverrideParams{
			LibraryID: libraryID, StreamUrl: streamURL,
		})
	}
	if err != nil {
		return fmt.Errorf("delete channel override: %w", err)
	}
	return nil
}

// Get returns one override if present. Returns (nil, nil) when the
// row doesn't exist so callers can pattern-match that without having
// to sniff for sql.ErrNoRows.
func (r *ChannelOverrideRepository) Get(ctx context.Context, libraryID, streamURL string) (*ChannelOverride, error) {
	if r.useSQLite() {
		row, err := r.sq.GetChannelOverride(ctx, sqlc.GetChannelOverrideParams{
			LibraryID: libraryID, StreamUrl: streamURL,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get channel override: %w", err)
		}
		return &ChannelOverride{
			LibraryID: row.LibraryID,
			StreamURL: row.StreamUrl,
			TvgID:     row.TvgID,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		}, nil
	}
	row, err := r.pq.GetChannelOverride(ctx, sqlc_pg.GetChannelOverrideParams{
		LibraryID: libraryID, StreamUrl: streamURL,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get channel override: %w", err)
	}
	return &ChannelOverride{
		LibraryID: row.LibraryID,
		StreamURL: row.StreamUrl,
		TvgID:     row.TvgID,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

// ApplyToLibrary is the post-import hook: for every override tied to
// this library, find the channel with the matching stream_url and
// update its tvg_id in place. A single transaction so a crash mid-
// apply doesn't leave the library half-overridden.
//
// Returns the number of channels actually updated. Orphaned overrides
// (URL no longer in the playlist) don't fail — they stay in the table
// waiting for their stream to reappear.
func (r *ChannelOverrideRepository) ApplyToLibrary(ctx context.Context, libraryID string) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	applied := 0
	if r.useSQLite() {
		qtx := r.sq.WithTx(tx)
		overrides, err := qtx.ListChannelOverridesByLibrary(ctx, libraryID)
		if err != nil {
			return 0, fmt.Errorf("read overrides: %w", err)
		}
		for _, ov := range overrides {
			n, err := qtx.ApplyChannelOverride(ctx, sqlc.ApplyChannelOverrideParams{
				TvgID:     sql.NullString{String: ov.TvgID, Valid: true},
				LibraryID: libraryID,
				StreamUrl: ov.StreamUrl,
			})
			if err != nil {
				return 0, fmt.Errorf("apply override: %w", err)
			}
			applied += int(n)
		}
	} else {
		qtx := r.pq.WithTx(tx)
		overrides, err := qtx.ListChannelOverridesByLibrary(ctx, libraryID)
		if err != nil {
			return 0, fmt.Errorf("read overrides: %w", err)
		}
		for _, ov := range overrides {
			n, err := qtx.ApplyChannelOverride(ctx, sqlc_pg.ApplyChannelOverrideParams{
				TvgID:     sql.NullString{String: ov.TvgID, Valid: true},
				LibraryID: libraryID,
				StreamUrl: ov.StreamUrl,
			})
			if err != nil {
				return 0, fmt.Errorf("apply override: %w", err)
			}
			applied += int(n)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit overrides: %w", err)
	}
	return applied, nil
}
