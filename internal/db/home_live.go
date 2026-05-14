package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// HomeLiveNowChannel is one entry in the "live now" rail.
type HomeLiveNowChannel struct {
	ChannelID    string
	ChannelName  string
	ChannelLogo  sql.NullString
	LibraryID    string
	LibraryName  string
	ProgramTitle sql.NullString
	ProgramStart sql.NullTime
	ProgramEnd   sql.NullTime
	ProgramIcon  sql.NullString
}

// LiveNow returns up to `limit` channels with their current EPG
// program. Order:
//
//   1. User's favourited channels (user_channel_favorites) first
//   2. Then channels with a program currently airing
//   3. Then anything else, by name
//
// Restricted to libraries the user can access. Inactive channels
// (is_active = false) are skipped — they're disabled at the source.
// Unhealthy channels (consecutive_failures >= UnhealthyThreshold) are
// also excluded so the rail and the LiveTV channel list stay in sync —
// otherwise clicking a card here deep-links into LiveTV with a channel
// id that LiveTV's healthy-only fetch doesn't surface, and the player
// never opens.
func (r *HomeRepository) LiveNow(ctx context.Context, userID string, limit int) ([]HomeLiveNowChannel, error) {
	if limit <= 0 || limit > 30 {
		limit = 5
	}
	now := time.Now().UTC()

	rows, err := r.db.QueryContext(ctx, r.liveNowSQL, now, now, userID, UnhealthyThreshold, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("live now: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := make([]HomeLiveNowChannel, 0, limit)
	for rows.Next() {
		var c HomeLiveNowChannel
		var startRaw, endRaw any
		var isFav, hasNow int
		if err := rows.Scan(&c.ChannelID, &c.ChannelName, &c.ChannelLogo,
			&c.LibraryID, &c.LibraryName,
			&c.ProgramTitle, &startRaw, &endRaw, &c.ProgramIcon,
			&isFav, &hasNow); err != nil {
			return nil, fmt.Errorf("scan live-now row: %w", err)
		}
		if startRaw != nil {
			t, err := coerceSQLiteTime(startRaw)
			if err != nil {
				return nil, fmt.Errorf("parse program start: %w", err)
			}
			c.ProgramStart = sql.NullTime{Time: t, Valid: true}
		}
		if endRaw != nil {
			t, err := coerceSQLiteTime(endRaw)
			if err != nil {
				return nil, fmt.Errorf("parse program end: %w", err)
			}
			c.ProgramEnd = sql.NullTime{Time: t, Valid: true}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
