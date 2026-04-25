package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
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

// ChannelOverrideRepository persists manual channel edits separately
// from the `channels` table so a DELETE+INSERT import pass leaves
// operator intent intact. Sqlc-generated queries for everything except
// ApplyToLibrary, which still owns the multi-row transaction (sqlc
// generates per-row primitives; the txn boundary is the repo's job).
type ChannelOverrideRepository struct {
	db *sql.DB
	q  *sqlc.Queries
}

func NewChannelOverrideRepository(database *sql.DB) *ChannelOverrideRepository {
	return &ChannelOverrideRepository{db: database, q: sqlc.New(database)}
}

// Upsert records an override. Idempotent: re-running with the same
// fields just bumps updated_at.
func (r *ChannelOverrideRepository) Upsert(ctx context.Context, o *ChannelOverride) error {
	now := time.Now().UTC()
	if o.CreatedAt.IsZero() {
		o.CreatedAt = now
	}
	o.UpdatedAt = now
	if err := r.q.UpsertChannelOverride(ctx, sqlc.UpsertChannelOverrideParams{
		LibraryID: o.LibraryID,
		StreamUrl: o.StreamURL,
		TvgID:     o.TvgID,
		CreatedAt: o.CreatedAt,
		UpdatedAt: o.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("upsert channel override: %w", err)
	}
	return nil
}

// Delete clears an override by its PK. Idempotent.
func (r *ChannelOverrideRepository) Delete(ctx context.Context, libraryID, streamURL string) error {
	if err := r.q.DeleteChannelOverride(ctx, sqlc.DeleteChannelOverrideParams{
		LibraryID: libraryID, StreamUrl: streamURL,
	}); err != nil {
		return fmt.Errorf("delete channel override: %w", err)
	}
	return nil
}

// Get returns one override if present. Returns (nil, nil) when the
// row doesn't exist so callers can pattern-match that without having
// to sniff for sql.ErrNoRows.
func (r *ChannelOverrideRepository) Get(ctx context.Context, libraryID, streamURL string) (*ChannelOverride, error) {
	row, err := r.q.GetChannelOverride(ctx, sqlc.GetChannelOverrideParams{
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
	qtx := r.q.WithTx(tx)

	overrides, err := qtx.ListChannelOverridesByLibrary(ctx, libraryID)
	if err != nil {
		return 0, fmt.Errorf("read overrides: %w", err)
	}
	applied := 0
	for _, ov := range overrides {
		// channels.tvg_id is nullable in the schema; sqlc renders the
		// param as sql.NullString. We always have a value here (the
		// upsert side normalises empty → string) so just wrap.
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
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit overrides: %w", err)
	}
	return applied, nil
}
