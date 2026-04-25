package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
)

// ChannelWatchHistoryRepository persists per-user playback timestamps
// keyed by stream_url. Stream URL is the stable identity of a channel
// across M3U refreshes — the channel.id UUID is regenerated on each
// refresh so a channel_id-keyed history would CASCADE-die daily. See
// migration 012 for the full rationale.
//
// Sqlc-generated queries handle the row scan; this thin adapter
// projects the join row into the domain Channel struct that the
// service layer already consumes.
type ChannelWatchHistoryRepository struct {
	q *sqlc.Queries
}

func NewChannelWatchHistoryRepository(database *sql.DB) *ChannelWatchHistoryRepository {
	return &ChannelWatchHistoryRepository{q: sqlc.New(database)}
}

// RecordByStreamURL upserts the (user, stream_url) pair with the
// current timestamp. Idempotent — every "playing" event during a
// session just rewrites the same row, so a user who pauses and
// resumes 20 times in 10 minutes produces exactly one history entry.
//
// Returns the timestamp written so handlers can echo it to the client
// without an extra read.
func (r *ChannelWatchHistoryRepository) RecordByStreamURL(ctx context.Context, userID, streamURL string) (time.Time, error) {
	now := time.Now().UTC()
	if err := r.q.RecordChannelWatch(ctx, sqlc.RecordChannelWatchParams{
		UserID:        userID,
		StreamUrl:     streamURL,
		LastWatchedAt: now,
	}); err != nil {
		return time.Time{}, fmt.Errorf("record channel watch: %w", err)
	}
	return now, nil
}

// ListChannelsByUser returns the Channel rows for the caller's most
// recently watched streams, newest first, up to `limit`. Joins by
// stream_url so rewritten channel UUIDs (post-M3U-refresh) still
// resolve as long as the stream is still in the playlist.
//
// When a single stream_url exists in multiple libraries (rare — same
// playlist attached to two libraries), the first row wins in a
// deterministic order so the result stays stable across calls.
//
// ACL is NOT applied here — the caller is responsible for library
// access filtering.
func (r *ChannelWatchHistoryRepository) ListChannelsByUser(ctx context.Context, userID string, limit int) ([]*Channel, []time.Time, error) {
	if limit <= 0 {
		return nil, nil, nil
	}
	// Double the SQL limit as a cheap guard against stream_url
	// duplicates across libraries — we dedupe in Go below and trim
	// to the requested limit.
	rows, err := r.q.ListChannelWatchHistoryByUser(ctx, sqlc.ListChannelWatchHistoryByUserParams{
		UserID: userID,
		Limit:  int64(limit * 2),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list continue-watching: %w", err)
	}

	seenURLs := make(map[string]struct{}, limit)
	channels := make([]*Channel, 0, limit)
	watched := make([]time.Time, 0, limit)
	for _, row := range rows {
		if _, dup := seenURLs[row.StreamUrl]; dup {
			continue
		}
		seenURLs[row.StreamUrl] = struct{}{}
		ch := &Channel{
			ID:        row.ID,
			LibraryID: row.LibraryID,
			Name:      row.Name,
			GroupName: row.GroupName,
			LogoURL:   row.LogoUrl,
			StreamURL: row.StreamUrl,
			TvgID:     row.TvgID,
			Language:  row.Language,
			Country:   row.Country,
			IsActive:  row.IsActive,
			AddedAt:   row.AddedAt,
		}
		if row.Number.Valid {
			ch.Number = int(row.Number.Int64)
		}
		channels = append(channels, ch)
		watched = append(watched, row.LastWatchedAt)
		if len(channels) >= limit {
			break
		}
	}
	return channels, watched, nil
}

// DeleteByStreamURL removes a single (user, stream_url) entry.
// Idempotent. Not wired to the UI yet; kept available for a future
// "remove from continue watching" affordance.
func (r *ChannelWatchHistoryRepository) DeleteByStreamURL(ctx context.Context, userID, streamURL string) error {
	if err := r.q.DeleteChannelWatch(ctx, sqlc.DeleteChannelWatchParams{
		UserID: userID, StreamUrl: streamURL,
	}); err != nil {
		return fmt.Errorf("delete channel watch: %w", err)
	}
	return nil
}
