package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	librarymodel "hubplay/internal/library/model"
)


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
func (r *HomeRepository) Trending(ctx context.Context, userID string, windowDays, limit int) ([]librarymodel.HomeTrendingItem, error) {
	if windowDays <= 0 {
		windowDays = 7
	}
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	cutoff := timeNow().UTC().Add(-time.Duration(windowDays) * 24 * time.Hour)

	rows, err := r.db.QueryContext(ctx, r.trendingSQL, cutoff, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("trending: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := make([]librarymodel.HomeTrendingItem, 0, limit)
	for rows.Next() {
		var it librarymodel.HomeTrendingItem
		var (
			year            sql.NullInt64
			communityRating sql.NullFloat64
			lastPlayedRaw   any
		)
		if err := rows.Scan(&it.ID, &it.Type, &it.Title, &year, &communityRating,
			&it.LibraryID, &it.PlayCount, &lastPlayedRaw, &it.ContentRating); err != nil {
			return nil, fmt.Errorf("scan trending row: %w", err)
		}
		if year.Valid {
			v := int(year.Int64)
			it.Year = &v
		}
		if communityRating.Valid {
			it.CommunityRating = &communityRating.Float64
		}
		it.LastPlayedAt, err = coerceSQLiteTime(lastPlayedRaw)
		if err != nil {
			return nil, fmt.Errorf("parse last_played_at: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
