package db

import (
	"context"
	"database/sql"

	librarymodel "hubplay/internal/library/model"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
)

// UserData holds a user's interaction data for a specific item.

// UserDataRepository — dual-dialect (Pattern A + Pattern B). All sqlc
// methods branch on `useSQLite()`. Two raw-SQL methods stay outside
// sqlc: GetBatch (dynamic IN list) and NextUp (CTE with duplicate
// userID parameter that sqlc 1.31 cannot lower for SQLite).
type UserDataRepository struct {
	db *sql.DB
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewUserDataRepository(driver string, database *sql.DB) *UserDataRepository {
	r := &UserDataRepository{db: database}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *UserDataRepository) useSQLite() bool { return r.sq != nil }

func (r *UserDataRepository) driver() string {
	if r.useSQLite() {
		return DriverSQLite
	}
	return DriverPostgres
}

// Upsert creates or updates user data for an item.
//
// UpdatedAt is normalised to UTC at the binding boundary: see UpdateProgress
// for the modernc.org/sqlite serialisation contract this enforces. LastPlayedAt
// is normalised by nullableTimePtr.
func (r *UserDataRepository) Upsert(ctx context.Context, ud *librarymodel.UserData) error {
	if r.useSQLite() {
		err := r.sq.UpsertUserData(ctx, sqlc.UpsertUserDataParams{
			UserID:              ud.UserID,
			ItemID:              ud.ItemID,
			PositionTicks:       sql.NullInt64{Int64: ud.PositionTicks, Valid: true},
			PlayCount:           sql.NullInt64{Int64: int64(ud.PlayCount), Valid: true},
			Completed:           sql.NullBool{Bool: ud.Completed, Valid: true},
			IsFavorite:          sql.NullBool{Bool: ud.IsFavorite, Valid: true},
			Liked:               nullableBoolPtr(ud.Liked),
			AudioStreamIndex:    nullableIntPtr(ud.AudioStreamIndex),
			SubtitleStreamIndex: nullableIntPtr(ud.SubtitleStreamIndex),
			LastPlayedAt:        nullableTimePtr(ud.LastPlayedAt),
			UpdatedAt:           ud.UpdatedAt.UTC(),
		})
		if err != nil {
			return fmt.Errorf("upsert user data: %w", err)
		}
		return nil
	}
	err := r.pq.UpsertUserData(ctx, sqlc_pg.UpsertUserDataParams{
		UserID:              ud.UserID,
		ItemID:              ud.ItemID,
		PositionTicks:       sql.NullInt64{Int64: ud.PositionTicks, Valid: true},
		PlayCount:           sql.NullInt32{Int32: int32(ud.PlayCount), Valid: true},
		Completed:           sql.NullBool{Bool: ud.Completed, Valid: true},
		IsFavorite:          sql.NullBool{Bool: ud.IsFavorite, Valid: true},
		Liked:               nullableBoolPtr(ud.Liked),
		AudioStreamIndex:    nullableIntPtrInt32(ud.AudioStreamIndex),
		SubtitleStreamIndex: nullableIntPtrInt32(ud.SubtitleStreamIndex),
		LastPlayedAt:        nullableTimePtr(ud.LastPlayedAt),
		UpdatedAt:           ud.UpdatedAt.UTC(),
	})
	if err != nil {
		return fmt.Errorf("upsert user data: %w", err)
	}
	return nil
}

// Get returns user data for a specific user+item pair.
func (r *UserDataRepository) Get(ctx context.Context, userID, itemID string) (*librarymodel.UserData, error) {
	if r.useSQLite() {
		row, err := r.sq.GetUserData(ctx, sqlc.GetUserDataParams{
			UserID: userID,
			ItemID: itemID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get user data: %w", err)
		}
		ud := userDataFromSqliteRow(row)
		return &ud, nil
	}
	row, err := r.pq.GetUserData(ctx, sqlc_pg.GetUserDataParams{
		UserID: userID,
		ItemID: itemID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user data: %w", err)
	}
	ud := userDataFromPgRow(row)
	return &ud, nil
}

// GetBatch returns user data for the given user across a batch of item IDs,
// keyed by item_id. Items with no row are simply absent from the map (the
// caller treats that as "no progress yet"). Uses raw SQL because sqlc
// doesn't support dynamic IN().
func (r *UserDataRepository) GetBatch(ctx context.Context, userID string, itemIDs []string) (map[string]*librarymodel.UserData, error) {
	if userID == "" || len(itemIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(itemIDs))
	args := make([]any, 0, len(itemIDs)+1)
	args = append(args, userID)
	for i, id := range itemIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := rewritePlaceholders(r.driver(), fmt.Sprintf(
		`SELECT user_id, item_id, position_ticks, play_count, completed,
		        is_favorite, liked, audio_stream_index, subtitle_stream_index,
		        last_played_at, updated_at
		 FROM user_data
		 WHERE user_id = ? AND item_id IN (%s)`,
		joinStrings(placeholders, ","),
	))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get user data batch: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	pg := !r.useSQLite()
	out := make(map[string]*librarymodel.UserData, len(itemIDs))
	for rows.Next() {
		// play_count / audio_stream_index / subtitle_stream_index
		// are INTEGER → NullInt64 SQLite, NullInt32 Postgres. Scan
		// into the dialect's native size and project to int.
		var (
			userIDCol, itemIDCol  string
			positionTicks         sql.NullInt64
			completed, isFavorite sql.NullBool
			liked                 sql.NullBool
			lastPlayedAt          sql.NullTime
			updatedAt             time.Time
		)
		ud := &librarymodel.UserData{}
		if pg {
			var playCount sql.NullInt32
			var audio, subtitle sql.NullInt32
			if err := rows.Scan(
				&userIDCol, &itemIDCol, &positionTicks, &playCount, &completed,
				&isFavorite, &liked, &audio, &subtitle,
				&lastPlayedAt, &updatedAt,
			); err != nil {
				return nil, fmt.Errorf("scan user data batch: %w", err)
			}
			if playCount.Valid {
				ud.PlayCount = int(playCount.Int32)
			}
			if audio.Valid {
				v := int(audio.Int32)
				ud.AudioStreamIndex = &v
			}
			if subtitle.Valid {
				v := int(subtitle.Int32)
				ud.SubtitleStreamIndex = &v
			}
		} else {
			var playCount sql.NullInt64
			var audio, subtitle sql.NullInt64
			if err := rows.Scan(
				&userIDCol, &itemIDCol, &positionTicks, &playCount, &completed,
				&isFavorite, &liked, &audio, &subtitle,
				&lastPlayedAt, &updatedAt,
			); err != nil {
				return nil, fmt.Errorf("scan user data batch: %w", err)
			}
			if playCount.Valid {
				ud.PlayCount = int(playCount.Int64)
			}
			if audio.Valid {
				v := int(audio.Int64)
				ud.AudioStreamIndex = &v
			}
			if subtitle.Valid {
				v := int(subtitle.Int64)
				ud.SubtitleStreamIndex = &v
			}
		}
		ud.UserID = userIDCol
		ud.ItemID = itemIDCol
		ud.PositionTicks = positionTicks.Int64
		ud.Completed = completed.Bool
		ud.IsFavorite = isFavorite.Bool
		ud.UpdatedAt = updatedAt
		if liked.Valid {
			v := liked.Bool
			ud.Liked = &v
		}
		if lastPlayedAt.Valid {
			ud.LastPlayedAt = &lastPlayedAt.Time
		}
		out[ud.ItemID] = ud
	}
	return out, rows.Err()
}

// UpdateProgress updates just the playback position and timestamps.
//
// Times are normalised to UTC before binding: modernc.org/sqlite serialises
// a non-UTC time.Time via time.Time.String() — "2026-04-24 12:00:00 +0200 CEST
// m=+0.001..." — whose monotonic-clock suffix the default Scan path cannot
// parse back. UTC times round-trip via RFC3339. Same hard contract the EPG
// repository documents in epg_repository.go.
func (r *UserDataRepository) UpdateProgress(ctx context.Context, userID, itemID string, positionTicks int64, completed bool) error {
	now := timeNow().UTC()
	if r.useSQLite() {
		err := r.sq.UpdateProgress(ctx, sqlc.UpdateProgressParams{
			UserID:        userID,
			ItemID:        itemID,
			PositionTicks: sql.NullInt64{Int64: positionTicks, Valid: true},
			Completed:     sql.NullBool{Bool: completed, Valid: true},
			LastPlayedAt:  sql.NullTime{Time: now, Valid: true},
			UpdatedAt:     now,
		})
		if err != nil {
			return fmt.Errorf("update progress: %w", err)
		}
		return nil
	}
	err := r.pq.UpdateProgress(ctx, sqlc_pg.UpdateProgressParams{
		UserID:        userID,
		ItemID:        itemID,
		PositionTicks: sql.NullInt64{Int64: positionTicks, Valid: true},
		Completed:     sql.NullBool{Bool: completed, Valid: true},
		LastPlayedAt:  sql.NullTime{Time: now, Valid: true},
		UpdatedAt:     now,
	})
	if err != nil {
		return fmt.Errorf("update progress: %w", err)
	}
	return nil
}

// MarkPlayed increments play count and marks completed.
func (r *UserDataRepository) MarkPlayed(ctx context.Context, userID, itemID string) error {
	now := timeNow().UTC()
	if r.useSQLite() {
		err := r.sq.MarkPlayed(ctx, sqlc.MarkPlayedParams{
			UserID:       userID,
			ItemID:       itemID,
			LastPlayedAt: sql.NullTime{Time: now, Valid: true},
			UpdatedAt:    now,
		})
		if err != nil {
			return fmt.Errorf("mark played: %w", err)
		}
		return nil
	}
	err := r.pq.MarkPlayed(ctx, sqlc_pg.MarkPlayedParams{
		UserID:       userID,
		ItemID:       itemID,
		LastPlayedAt: sql.NullTime{Time: now, Valid: true},
		UpdatedAt:    now,
	})
	if err != nil {
		return fmt.Errorf("mark played: %w", err)
	}
	return nil
}

// SetFavorite sets or unsets favorite for an item.
func (r *UserDataRepository) SetFavorite(ctx context.Context, userID, itemID string, favorite bool) error {
	now := timeNow().UTC()
	if r.useSQLite() {
		err := r.sq.SetFavorite(ctx, sqlc.SetFavoriteParams{
			UserID:     userID,
			ItemID:     itemID,
			IsFavorite: sql.NullBool{Bool: favorite, Valid: true},
			UpdatedAt:  now,
		})
		if err != nil {
			return fmt.Errorf("set favorite: %w", err)
		}
		return nil
	}
	err := r.pq.SetFavorite(ctx, sqlc_pg.SetFavoriteParams{
		UserID:     userID,
		ItemID:     itemID,
		IsFavorite: sql.NullBool{Bool: favorite, Valid: true},
		UpdatedAt:  now,
	})
	if err != nil {
		return fmt.Errorf("set favorite: %w", err)
	}
	return nil
}

// AbandonedAfter is the inactivity window past which a partly-watched
// item drops out of "Continue Watching" (when the user got less than
// halfway in). 30 days is the rule of thumb Plex/Jellyfin both lean
// toward; exposed as a package-level var so a future config option or
// per-user setting can override without churning every call site.
var AbandonedAfter = 30 * 24 * time.Hour

// ContinueWatching returns items the user started but hasn't completed,
// ordered by last played. Drops two classes of noise the naive query
// keeps:
//
//   - Near-complete (>=90 % watched). The user almost certainly
//     finished and never explicitly marked played; surfacing it as
//     "in progress" is wrong by both Plex/Jellyfin convention and by
//     reality.
//   - Abandoned (last play older than AbandonedAfter AND <50 %
//     watched). The user moved on; the rail should not keep nagging
//     about the same start-of-S1E1 forever.
//
// Items with unknown duration (`duration_ticks = 0`) are kept — we
// can't reason about progress without it, so leaving them visible is
// the safe default.
func (r *UserDataRepository) ContinueWatching(ctx context.Context, userID string, limit int) ([]*librarymodel.ContinueWatchingItem, error) {
	if limit <= 0 {
		limit = 20
	}
	abandonedThreshold := timeNow().UTC().Add(-AbandonedAfter)
	if r.useSQLite() {
		// LastPlayedAt is sqlc's auto-name for the abandoned-threshold
		// param (it's the column the comparison is against). Same value as
		// before the DeMorgan rewrite — see queries/user_data.sql for why.
		rows, err := r.sq.ContinueWatching(ctx, sqlc.ContinueWatchingParams{
			UserID:       userID,
			LastPlayedAt: sql.NullTime{Time: abandonedThreshold, Valid: true},
			Limit:        int64(limit),
		})
		if err != nil {
			return nil, fmt.Errorf("continue watching: %w", err)
		}
		return continueWatchingFromSqliteRows(rows), nil
	}
	rows, err := r.pq.ContinueWatching(ctx, sqlc_pg.ContinueWatchingParams{
		UserID:       userID,
		LastPlayedAt: sql.NullTime{Time: abandonedThreshold, Valid: true},
		Limit:        int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("continue watching: %w", err)
	}
	return continueWatchingFromPgRows(rows), nil
}

// ContinueWatchingItem is the result for continue watching queries.

// Favorites returns items marked as favorite by the user.
func (r *UserDataRepository) Favorites(ctx context.Context, userID string, limit, offset int) ([]*librarymodel.FavoriteItem, error) {
	if limit <= 0 {
		limit = 50
	}
	if r.useSQLite() {
		rows, err := r.sq.ListFavorites(ctx, sqlc.ListFavoritesParams{
			UserID: userID,
			Limit:  int64(limit),
			Offset: int64(offset),
		})
		if err != nil {
			return nil, fmt.Errorf("favorites: %w", err)
		}
		return favoritesFromSqliteRows(rows), nil
	}
	rows, err := r.pq.ListFavorites(ctx, sqlc_pg.ListFavoritesParams{
		UserID: userID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("favorites: %w", err)
	}
	return favoritesFromPgRows(rows), nil
}

// FavoriteItem is the result for favorite queries.

// NextUp returns the next unwatched episode for each series the user is watching.
// Uses raw SQL because the CTE references the same param twice and sqlc
// (both backends) doesn't handle duplicate named params in subqueries.
//
// BOOLEAN comparison sidestep: `i.is_available` and `ud.completed` (no
// `= 1`) — works in SQLite (BOOLEAN stored as INTEGER, truthy on the
// integer 1) and Postgres (real boolean predicate). Same trick the
// item / channel repos use.
func (r *UserDataRepository) NextUp(ctx context.Context, userID string, limit int) ([]*librarymodel.NextUpItem, error) {
	if limit <= 0 {
		limit = 20
	}
	query := rewritePlaceholders(r.driver(),
		`WITH watched_episodes AS (
		   SELECT i.id, i.parent_id AS season_id,
		          (SELECT parent_id FROM items WHERE id = i.parent_id) AS series_id,
		          i.episode_number, i.season_number,
		          ud.last_played_at
		   FROM user_data ud
		   JOIN items i ON i.id = ud.item_id
		   WHERE ud.user_id = ? AND i.type = 'episode' AND ud.completed
		 ),
		 last_watched AS (
		   SELECT series_id, MAX(last_played_at) AS last_played_at,
		          MAX(season_number * 10000 + episode_number) AS last_order
		   FROM watched_episodes
		   WHERE series_id IS NOT NULL
		   GROUP BY series_id
		 )
		 SELECT e.id, e.title, e.season_number, e.episode_number,
		        e.duration_ticks, s.title AS series_title, lw.series_id
		 FROM last_watched lw
		 JOIN items e ON e.type = 'episode' AND e.is_available
		   AND (SELECT parent_id FROM items WHERE id = e.parent_id) = lw.series_id
		 JOIN items s ON s.id = lw.series_id
		 WHERE (COALESCE(e.season_number, 0) * 10000 + COALESCE(e.episode_number, 0)) > lw.last_order
		   AND NOT EXISTS (
		     SELECT 1 FROM user_data ud2
		     WHERE ud2.user_id = ? AND ud2.item_id = e.id AND ud2.completed
		   )
		 GROUP BY lw.series_id
		 HAVING MIN(COALESCE(e.season_number, 0) * 10000 + COALESCE(e.episode_number, 0))
		 ORDER BY lw.last_played_at DESC
		 LIMIT ?`)

	rows, err := r.db.QueryContext(ctx, query, userID, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("next up: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	pg := !r.useSQLite()
	var items []*librarymodel.NextUpItem
	for rows.Next() {
		item := &librarymodel.NextUpItem{}
		// season_number / episode_number INTEGER → NullInt64 sqlite,
		// NullInt32 pg. Scan into the dialect's type and project to *int.
		var seasonScan, episodeScan sql.NullInt64
		var seasonScan32, episodeScan32 sql.NullInt32
		if pg {
			if err := rows.Scan(&item.EpisodeID, &item.EpisodeTitle,
				&seasonScan32, &episodeScan32,
				&item.DurationTicks, &item.SeriesTitle, &item.SeriesID); err != nil {
				return nil, fmt.Errorf("scan next up: %w", err)
			}
			if seasonScan32.Valid {
				v := int(seasonScan32.Int32)
				item.SeasonNumber = &v
			}
			if episodeScan32.Valid {
				v := int(episodeScan32.Int32)
				item.EpisodeNumber = &v
			}
		} else {
			if err := rows.Scan(&item.EpisodeID, &item.EpisodeTitle,
				&seasonScan, &episodeScan,
				&item.DurationTicks, &item.SeriesTitle, &item.SeriesID); err != nil {
				return nil, fmt.Errorf("scan next up: %w", err)
			}
			if seasonScan.Valid {
				v := int(seasonScan.Int64)
				item.SeasonNumber = &v
			}
			if episodeScan.Valid {
				v := int(episodeScan.Int64)
				item.EpisodeNumber = &v
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// NextUpItem is the result for next-up queries.

// SeriesEpisodeProgress reports total + watched episode counts for a
// single series for one user. Used by the series detail page hero to
// render "Has visto X de Y episodios". Returns (0, 0) for a series
// with no episodes — caller decides whether to render anything.
func (r *UserDataRepository) SeriesEpisodeProgress(ctx context.Context, userID, seriesID string) (total, watched int, err error) {
	if r.useSQLite() {
		row, qerr := r.sq.SeriesEpisodeProgress(ctx, sqlc.SeriesEpisodeProgressParams{
			UserID:   userID,
			ParentID: sql.NullString{String: seriesID, Valid: seriesID != ""},
		})
		if qerr != nil {
			return 0, 0, fmt.Errorf("series episode progress: %w", qerr)
		}
		return int(row.TotalEpisodes), int(row.WatchedEpisodes), nil
	}
	row, qerr := r.pq.SeriesEpisodeProgress(ctx, sqlc_pg.SeriesEpisodeProgressParams{
		UserID:   userID,
		ParentID: sql.NullString{String: seriesID, Valid: seriesID != ""},
	})
	if qerr != nil {
		return 0, 0, fmt.Errorf("series episode progress: %w", qerr)
	}
	return int(row.TotalEpisodes), int(row.WatchedEpisodes), nil
}

// Delete removes user data for a specific user+item pair.
func (r *UserDataRepository) Delete(ctx context.Context, userID, itemID string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteUserData(ctx, sqlc.DeleteUserDataParams{
			UserID: userID,
			ItemID: itemID,
		})
	} else {
		err = r.pq.DeleteUserData(ctx, sqlc_pg.DeleteUserDataParams{
			UserID: userID,
			ItemID: itemID,
		})
	}
	if err != nil {
		return fmt.Errorf("delete user data: %w", err)
	}
	return nil
}

// ClearProgress zeroes position_ticks for an item so it falls off the
// Continue Watching rail. Distinct from Delete (which nukes the whole
// user_data row, losing play_count + favorite + last_played_at) and
// MarkPlayed (which lies about completion). Idempotent — no error
// when the row doesn't exist (the UPDATE simply matches zero rows).
func (r *UserDataRepository) ClearProgress(ctx context.Context, userID, itemID string) error {
	now := timeNow().UTC()
	var err error
	if r.useSQLite() {
		err = r.sq.ClearProgress(ctx, sqlc.ClearProgressParams{
			UpdatedAt: now,
			UserID:    userID,
			ItemID:    itemID,
		})
	} else {
		err = r.pq.ClearProgress(ctx, sqlc_pg.ClearProgressParams{
			UpdatedAt: now,
			UserID:    userID,
			ItemID:    itemID,
		})
	}
	if err != nil {
		return fmt.Errorf("clear progress: %w", err)
	}
	return nil
}

// ── row mapping helpers ─────────────────────────────────────────────────

func nullableBoolPtr(b *bool) sql.NullBool {
	if b == nil {
		return sql.NullBool{}
	}
	return sql.NullBool{Bool: *b, Valid: true}
}

func nullableIntPtr(i *int) sql.NullInt64 {
	if i == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*i), Valid: true}
}

// nullableTimePtr converts an optional time pointer into a sql.NullTime ready
// for binding. Times are forced to UTC: see the UpdateProgress comment for the
// modernc.org/sqlite round-trip contract this enforces.
func nullableTimePtr(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: (*t).UTC(), Valid: true}
}

func userDataFromSqliteRow(r sqlc.UserDatum) librarymodel.UserData {
	ud := librarymodel.UserData{
		UserID:        r.UserID,
		ItemID:        r.ItemID,
		PositionTicks: r.PositionTicks.Int64,
		PlayCount:     int(r.PlayCount.Int64),
		Completed:     r.Completed.Bool,
		IsFavorite:    r.IsFavorite.Bool,
		UpdatedAt:     r.UpdatedAt,
	}
	if r.Liked.Valid {
		v := r.Liked.Bool
		ud.Liked = &v
	}
	if r.AudioStreamIndex.Valid {
		v := int(r.AudioStreamIndex.Int64)
		ud.AudioStreamIndex = &v
	}
	if r.SubtitleStreamIndex.Valid {
		v := int(r.SubtitleStreamIndex.Int64)
		ud.SubtitleStreamIndex = &v
	}
	if r.LastPlayedAt.Valid {
		ud.LastPlayedAt = &r.LastPlayedAt.Time
	}
	return ud
}

func userDataFromPgRow(r sqlc_pg.UserDatum) librarymodel.UserData {
	ud := librarymodel.UserData{
		UserID:        r.UserID,
		ItemID:        r.ItemID,
		PositionTicks: r.PositionTicks.Int64,
		PlayCount:     int(r.PlayCount.Int32),
		Completed:     r.Completed.Bool,
		IsFavorite:    r.IsFavorite.Bool,
		UpdatedAt:     r.UpdatedAt,
	}
	if r.Liked.Valid {
		v := r.Liked.Bool
		ud.Liked = &v
	}
	if r.AudioStreamIndex.Valid {
		v := int(r.AudioStreamIndex.Int32)
		ud.AudioStreamIndex = &v
	}
	if r.SubtitleStreamIndex.Valid {
		v := int(r.SubtitleStreamIndex.Int32)
		ud.SubtitleStreamIndex = &v
	}
	if r.LastPlayedAt.Valid {
		ud.LastPlayedAt = &r.LastPlayedAt.Time
	}
	return ud
}

func continueWatchingFromSqliteRows(rows []sqlc.ContinueWatchingRow) []*librarymodel.ContinueWatchingItem {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*librarymodel.ContinueWatchingItem, len(rows))
	for i, r := range rows {
		item := &librarymodel.ContinueWatchingItem{
			ItemID:        r.ItemID,
			PositionTicks: r.PositionTicks.Int64,
			Title:         r.Title,
			Type:          r.Type,
			DurationTicks: r.DurationTicks.Int64,
			ParentID:      r.ParentID,
			Container:     r.Container,
			SeasonNumber:  ptrInt(int64(r.SeasonNumber)),
			EpisodeNumber: ptrInt(int64(r.EpisodeNumber)),
			SeriesID:      r.SeriesID,
			SeriesTitle:   r.SeriesTitle,
		}
		if r.LastPlayedAt.Valid {
			item.LastPlayedAt = &r.LastPlayedAt.Time
		}
		out[i] = item
	}
	return out
}

func continueWatchingFromPgRows(rows []sqlc_pg.ContinueWatchingRow) []*librarymodel.ContinueWatchingItem {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*librarymodel.ContinueWatchingItem, len(rows))
	for i, r := range rows {
		item := &librarymodel.ContinueWatchingItem{
			ItemID:        r.ItemID,
			PositionTicks: r.PositionTicks.Int64,
			Title:         r.Title,
			Type:          r.Type,
			DurationTicks: r.DurationTicks.Int64,
			ParentID:      r.ParentID,
			Container:     r.Container,
			SeasonNumber:  ptrInt(int64(r.SeasonNumber)),
			EpisodeNumber: ptrInt(int64(r.EpisodeNumber)),
			SeriesID:      r.SeriesID,
			SeriesTitle:   r.SeriesTitle,
		}
		if r.LastPlayedAt.Valid {
			item.LastPlayedAt = &r.LastPlayedAt.Time
		}
		out[i] = item
	}
	return out
}

func favoritesFromSqliteRows(rows []sqlc.ListFavoritesRow) []*librarymodel.FavoriteItem {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*librarymodel.FavoriteItem, len(rows))
	for i, r := range rows {
		out[i] = &librarymodel.FavoriteItem{
			ItemID:        r.ItemID,
			FavoritedAt:   r.UpdatedAt,
			Title:         r.Title,
			Type:          r.Type,
			Year:          int(r.Year.Int64),
			DurationTicks: r.DurationTicks.Int64,
		}
	}
	return out
}

func favoritesFromPgRows(rows []sqlc_pg.ListFavoritesRow) []*librarymodel.FavoriteItem {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*librarymodel.FavoriteItem, len(rows))
	for i, r := range rows {
		out[i] = &librarymodel.FavoriteItem{
			ItemID:        r.ItemID,
			FavoritedAt:   r.UpdatedAt,
			Title:         r.Title,
			Type:          r.Type,
			Year:          int(r.Year.Int32),
			DurationTicks: r.DurationTicks.Int64,
		}
	}
	return out
}

func ptrInt(v int64) *int {
	n := int(v)
	return &n
}
