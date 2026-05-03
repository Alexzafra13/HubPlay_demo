package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// HomeRepository serves the cross-cutting queries the configurable
// home page needs: per-library "latest", server-wide "trending", and
// the "live now" mini-rail that joins channels with their current EPG
// program.
//
// Lives in its own file (not bolted onto Items / UserData / Channels)
// because each query joins across multiple tables AND must respect
// per-user library access, and forcing those concerns into the
// existing repos would make their interfaces noisy. Raw SQL, not
// sqlc, since each query is a one-shot with joins + aggregations
// that sqlc's positional-binding model handles awkwardly (same
// rationale as the existing NextUp / UserHasAccess raw queries).
type HomeRepository struct {
	db *sql.DB
}

func NewHomeRepository(database *sql.DB) *HomeRepository {
	return &HomeRepository{db: database}
}

// HomeTrendingItem is one entry in the trending rail.
type HomeTrendingItem struct {
	ID              string
	Type            string
	Title           string
	Year            sql.NullInt64
	CommunityRating sql.NullFloat64
	LibraryID       string
	PlayCount       int64
	LastPlayedAt    time.Time
}

// Trending returns the top `limit` items played across ALL users in
// the last `windowDays`, scoped to libraries the caller can see.
// Items the user can't access (private library) are filtered out at
// the SQL level via the same library_access EXISTS pattern the rest
// of the codebase uses.
//
// Counts plays as "user_data rows touched in the window" rather than
// "play events" — HubPlay doesn't keep a play-event log; user_data
// last_played_at is updated on every progress write, which is the
// closest signal we have. A user that resumes a movie three times in
// a week counts as one trending vote, not three. That's a feature:
// it prevents one obsessive viewer from skewing the ranking.
//
// Movies and individual episodes count, but episodes are folded back
// to their series so the rail surfaces "Game of Thrones is hot",
// not "S04E09 is hot". Series ranking aggregates plays of all its
// episodes via the parent_id climb (one CTE).
func (r *HomeRepository) Trending(ctx context.Context, userID string, windowDays, limit int) ([]HomeTrendingItem, error) {
	if windowDays <= 0 {
		windowDays = 7
	}
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	cutoff := time.Now().UTC().Add(-time.Duration(windowDays) * 24 * time.Hour)

	// The CTE picks the "rollup" item id for every user_data row:
	//   episode → its series id (climb parent_id twice: episode→season→series)
	//   movie / series root → itself
	// Then we group by rollup id, count distinct (user_id, rollup_id)
	// votes (one user resuming N times still counts once), and order
	// by votes DESC.
	//
	// The library_access guard runs as an EXISTS sub-query rather
	// than a JOIN so the "no rows means everyone has access" rule
	// the rest of the codebase enforces stays consistent (see
	// LibraryRepository.UserHasAccess).
	const query = `
		WITH plays AS (
			SELECT
				ud.user_id,
				CASE
					WHEN i.type = 'episode' AND i.parent_id IS NOT NULL
						THEN COALESCE(
							(SELECT s.parent_id FROM items s WHERE s.id = i.parent_id),
							i.parent_id
						)
					ELSE i.id
				END AS rollup_id,
				ud.last_played_at
			FROM user_data ud
			JOIN items i ON i.id = ud.item_id
			WHERE ud.last_played_at >= ?
			  AND i.is_available = 1
		)
		SELECT
			i.id, i.type, i.title, i.year, i.community_rating, i.library_id,
			COUNT(DISTINCT p.user_id) AS votes,
			MAX(p.last_played_at)     AS last_played_at
		FROM plays p
		JOIN items i ON i.id = p.rollup_id
		WHERE i.is_available = 1
		  AND (
			EXISTS (SELECT 1 FROM library_access la WHERE la.library_id = i.library_id AND la.user_id = ?)
			OR NOT EXISTS (SELECT 1 FROM library_access la WHERE la.library_id = i.library_id)
		  )
		GROUP BY i.id
		ORDER BY votes DESC, last_played_at DESC
		LIMIT ?`

	rows, err := r.db.QueryContext(ctx, query, cutoff, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("trending: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := make([]HomeTrendingItem, 0, limit)
	for rows.Next() {
		var it HomeTrendingItem
		var lastPlayedRaw any
		if err := rows.Scan(&it.ID, &it.Type, &it.Title, &it.Year, &it.CommunityRating,
			&it.LibraryID, &it.PlayCount, &lastPlayedRaw); err != nil {
			return nil, fmt.Errorf("scan trending row: %w", err)
		}
		it.LastPlayedAt, err = coerceSQLiteTime(lastPlayedRaw)
		if err != nil {
			return nil, fmt.Errorf("parse last_played_at: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

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
// (is_active = 0) are skipped — they're disabled at the source.
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

	const query = `
		SELECT
			c.id, c.name, c.logo_url, c.library_id, l.name AS library_name,
			ep.title, ep.start_time, ep.end_time, ep.icon_url,
			CASE WHEN cf.channel_id IS NOT NULL THEN 1 ELSE 0 END AS is_fav,
			CASE WHEN ep.id IS NOT NULL          THEN 1 ELSE 0 END AS has_now
		FROM channels c
		JOIN libraries l ON l.id = c.library_id
		LEFT JOIN epg_programs ep
			ON ep.channel_id = c.id
			AND ep.start_time <= ?
			AND ep.end_time   > ?
		LEFT JOIN user_channel_favorites cf
			ON cf.channel_id = c.id AND cf.user_id = ?
		WHERE c.is_active = 1
		  AND c.consecutive_failures < ?
		  AND (
			EXISTS (SELECT 1 FROM library_access la WHERE la.library_id = c.library_id AND la.user_id = ?)
			OR NOT EXISTS (SELECT 1 FROM library_access la WHERE la.library_id = c.library_id)
		  )
		ORDER BY is_fav DESC, has_now DESC, c.name ASC
		LIMIT ?`

	rows, err := r.db.QueryContext(ctx, query, now, now, userID, UnhealthyThreshold, userID, limit)
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

// IDsFromTrending pulls just the ID column out of trending results,
// used by the home handler to batch-load full Item records and
// images for response enrichment.
func IDsFromTrending(items []HomeTrendingItem) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}
