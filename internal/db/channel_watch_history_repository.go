package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
)

// ChannelWatchHistoryRepository persists per-user playback timestamps
// keyed by stream_url. Stream URL is the stable identity of a channel
// across M3U refreshes — the channel.id UUID is regenerated on each
// refresh so a channel_id-keyed history would CASCADE-die daily. See
// migration 012 for the full rationale.
//
// Dual-dialect: sqlc-backed (Pattern A) per call. The Number column
// is INTEGER → NullInt64 on SQLite, NullInt32 on Postgres; both rows
// project to the same domain Channel via per-backend mapping helpers.
type ChannelWatchHistoryRepository struct {
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewChannelWatchHistoryRepository(driver string, database *sql.DB) *ChannelWatchHistoryRepository {
	r := &ChannelWatchHistoryRepository{}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *ChannelWatchHistoryRepository) useSQLite() bool { return r.sq != nil }

// RecordByStreamURL upserts the (user, stream_url) pair with the
// current timestamp. Idempotent — every "playing" event during a
// session just rewrites the same row, so a user who pauses and
// resumes 20 times in 10 minutes produces exactly one history entry.
//
// Returns the timestamp written so handlers can echo it to the client
// without an extra read.
func (r *ChannelWatchHistoryRepository) RecordByStreamURL(ctx context.Context, userID, streamURL string) (time.Time, error) {
	now := time.Now().UTC()
	var err error
	if r.useSQLite() {
		err = r.sq.RecordChannelWatch(ctx, sqlc.RecordChannelWatchParams{
			UserID:        userID,
			StreamUrl:     streamURL,
			LastWatchedAt: now,
		})
	} else {
		err = r.pq.RecordChannelWatch(ctx, sqlc_pg.RecordChannelWatchParams{
			UserID:        userID,
			StreamUrl:     streamURL,
			LastWatchedAt: now,
		})
	}
	if err != nil {
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
	doubled := int64(limit * 2)

	seenURLs := make(map[string]struct{}, limit)
	channels := make([]*Channel, 0, limit)
	watched := make([]time.Time, 0, limit)

	if r.useSQLite() {
		rows, err := r.sq.ListChannelWatchHistoryByUser(ctx, sqlc.ListChannelWatchHistoryByUserParams{
			UserID: userID,
			Limit:  doubled,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("list continue-watching: %w", err)
		}
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

	rows, err := r.pq.ListChannelWatchHistoryByUser(ctx, sqlc_pg.ListChannelWatchHistoryByUserParams{
		UserID: userID,
		Limit:  int32(doubled),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list continue-watching: %w", err)
	}
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
			ch.Number = int(row.Number.Int32)
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
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteChannelWatch(ctx, sqlc.DeleteChannelWatchParams{
			UserID: userID, StreamUrl: streamURL,
		})
	} else {
		err = r.pq.DeleteChannelWatch(ctx, sqlc_pg.DeleteChannelWatchParams{
			UserID: userID, StreamUrl: streamURL,
		})
	}
	if err != nil {
		return fmt.Errorf("delete channel watch: %w", err)
	}
	return nil
}
