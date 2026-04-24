package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ChannelWatchHistoryRepository persists per-user playback timestamps
// keyed by stream_url. Stream URL is the stable identity of a channel
// across M3U refreshes — the channel.id UUID is regenerated on each
// refresh so a channel_id-keyed history would CASCADE-die daily. See
// migration 012 for the full rationale.
//
// Raw SQL adapter; the hot paths are a single UPSERT on beacon and a
// single ordered JOIN for the rail.
type ChannelWatchHistoryRepository struct {
	db *sql.DB
}

func NewChannelWatchHistoryRepository(database *sql.DB) *ChannelWatchHistoryRepository {
	return &ChannelWatchHistoryRepository{db: database}
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
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channel_watch_history (user_id, stream_url, last_watched_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(user_id, stream_url) DO UPDATE SET
		    last_watched_at = excluded.last_watched_at`,
		userID, streamURL, now)
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
// Filters applied in SQL so the handler can't accidentally bypass them:
//   * `c.is_active = 1` — deactivated channels drop off the rail.
//   * Orphan history entries (stream_url dropped from every playlist)
//     are joined out naturally; they stay in the table in case the URL
//     returns later.
//
// When a single stream_url exists in multiple libraries (rare — same
// playlist attached to two libraries), the first row wins in a
// deterministic order so the result stays stable across calls.
//
// ACL is NOT applied here — the caller is responsible for library
// access filtering. Pushing it into this SELECT would pull in
// library_access and muddle the single-purpose query.
func (r *ChannelWatchHistoryRepository) ListChannelsByUser(ctx context.Context, userID string, limit int) ([]*Channel, []time.Time, error) {
	if limit <= 0 {
		return nil, nil, nil
	}
	// Double the SQL limit as a cheap guard against stream_url
	// duplicates across libraries — we dedupe in Go below and trim
	// to the requested limit. Worst case is pathological (every
	// history row duplicates across N libraries); the loop still
	// terminates and the output is correct.
	rows, err := r.db.QueryContext(ctx,
		`SELECT c.id, c.library_id, c.name, c.number, COALESCE(c.group_name,''),
		        COALESCE(c.logo_url,''), c.stream_url, COALESCE(c.tvg_id,''),
		        COALESCE(c.language,''), COALESCE(c.country,''),
		        c.is_active, c.added_at,
		        h.last_watched_at
		 FROM   channel_watch_history h
		 JOIN   channels c ON c.stream_url = h.stream_url
		 WHERE  h.user_id = ?
		   AND  c.is_active = 1
		 ORDER BY h.last_watched_at DESC, c.library_id ASC
		 LIMIT  ?`, userID, limit*2)
	if err != nil {
		return nil, nil, fmt.Errorf("list continue-watching: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	seenURLs := make(map[string]struct{}, limit)
	var channels []*Channel
	var watched []time.Time
	for rows.Next() {
		ch := &Channel{}
		var watchedRaw any
		if err := rows.Scan(
			&ch.ID, &ch.LibraryID, &ch.Name, &ch.Number, &ch.GroupName,
			&ch.LogoURL, &ch.StreamURL, &ch.TvgID,
			&ch.Language, &ch.Country,
			&ch.IsActive, &ch.AddedAt,
			&watchedRaw,
		); err != nil {
			return nil, nil, fmt.Errorf("scan continue-watching row: %w", err)
		}
		if _, dup := seenURLs[ch.StreamURL]; dup {
			continue
		}
		seenURLs[ch.StreamURL] = struct{}{}
		w, err := coerceSQLiteTime(watchedRaw)
		if err != nil {
			return nil, nil, fmt.Errorf("parse last_watched_at: %w", err)
		}
		channels = append(channels, ch)
		watched = append(watched, w)
		if len(channels) >= limit {
			break
		}
	}
	return channels, watched, rows.Err()
}

// DeleteByStreamURL removes a single (user, stream_url) entry.
// Idempotent. Not wired to the UI yet; kept available for a future
// "remove from continue watching" affordance.
func (r *ChannelWatchHistoryRepository) DeleteByStreamURL(ctx context.Context, userID, streamURL string) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM channel_watch_history WHERE user_id = ? AND stream_url = ?`,
		userID, streamURL); err != nil {
		return fmt.Errorf("delete channel watch: %w", err)
	}
	return nil
}
