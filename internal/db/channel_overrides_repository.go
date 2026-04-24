package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
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
// operator intent intact.
type ChannelOverrideRepository struct {
	db *sql.DB
}

func NewChannelOverrideRepository(database *sql.DB) *ChannelOverrideRepository {
	return &ChannelOverrideRepository{db: database}
}

// Upsert records an override. Idempotent: re-running with the same
// fields just bumps updated_at.
func (r *ChannelOverrideRepository) Upsert(ctx context.Context, o *ChannelOverride) error {
	now := time.Now().UTC()
	if o.CreatedAt.IsZero() {
		o.CreatedAt = now
	}
	o.UpdatedAt = now
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channel_overrides (library_id, stream_url, tvg_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(library_id, stream_url) DO UPDATE SET
		    tvg_id     = excluded.tvg_id,
		    updated_at = excluded.updated_at`,
		o.LibraryID, o.StreamURL, o.TvgID, o.CreatedAt, o.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert channel override: %w", err)
	}
	return nil
}

// Delete clears an override by its PK.
func (r *ChannelOverrideRepository) Delete(ctx context.Context, libraryID, streamURL string) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM channel_overrides WHERE library_id = ? AND stream_url = ?`,
		libraryID, streamURL); err != nil {
		return fmt.Errorf("delete channel override: %w", err)
	}
	return nil
}

// Get returns one override if present. Returns (nil, nil) when the
// row doesn't exist so callers can pattern-match that without having
// to sniff for sql.ErrNoRows.
func (r *ChannelOverrideRepository) Get(ctx context.Context, libraryID, streamURL string) (*ChannelOverride, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT library_id, stream_url, tvg_id, created_at, updated_at
		 FROM channel_overrides
		 WHERE library_id = ? AND stream_url = ?`,
		libraryID, streamURL)
	o := &ChannelOverride{}
	err := row.Scan(&o.LibraryID, &o.StreamURL, &o.TvgID, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get channel override: %w", err)
	}
	return o, nil
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

	rows, err := tx.QueryContext(ctx,
		`SELECT stream_url, tvg_id FROM channel_overrides WHERE library_id = ?`,
		libraryID)
	if err != nil {
		return 0, fmt.Errorf("read overrides: %w", err)
	}
	type pair struct {
		streamURL string
		tvgID     string
	}
	var overrides []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.streamURL, &p.tvgID); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan override: %w", err)
		}
		overrides = append(overrides, p)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate overrides: %w", err)
	}

	applied := 0
	for _, p := range overrides {
		res, err := tx.ExecContext(ctx,
			`UPDATE channels SET tvg_id = ?
			 WHERE library_id = ? AND stream_url = ?`,
			p.tvgID, libraryID, p.streamURL)
		if err != nil {
			return 0, fmt.Errorf("apply override: %w", err)
		}
		n, _ := res.RowsAffected()
		applied += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit overrides: %w", err)
	}
	return applied, nil
}
