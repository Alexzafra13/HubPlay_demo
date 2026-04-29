package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
)

// UserData holds a user's interaction data for a specific item.
type UserData struct {
	UserID              string
	ItemID              string
	PositionTicks       int64
	PlayCount           int
	Completed           bool
	IsFavorite          bool
	Liked               *bool // nil = no opinion
	AudioStreamIndex    *int
	SubtitleStreamIndex *int
	LastPlayedAt        *time.Time
	UpdatedAt           time.Time
}

type UserDataRepository struct {
	db *sql.DB // kept for NextUp (complex CTE with duplicate params)
	q  *sqlc.Queries
}

func NewUserDataRepository(database *sql.DB) *UserDataRepository {
	return &UserDataRepository{db: database, q: sqlc.New(database)}
}

// Upsert creates or updates user data for an item.
func (r *UserDataRepository) Upsert(ctx context.Context, ud *UserData) error {
	err := r.q.UpsertUserData(ctx, sqlc.UpsertUserDataParams{
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
		UpdatedAt:           ud.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("upsert user data: %w", err)
	}
	return nil
}

// Get returns user data for a specific user+item pair.
func (r *UserDataRepository) Get(ctx context.Context, userID, itemID string) (*UserData, error) {
	row, err := r.q.GetUserData(ctx, sqlc.GetUserDataParams{
		UserID: userID,
		ItemID: itemID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user data: %w", err)
	}
	ud := userDataFromRow(row)
	return &ud, nil
}

// GetBatch returns user data for the given user across a batch of item IDs,
// keyed by item_id. Items with no row are simply absent from the map (the
// caller treats that as "no progress yet"). Uses raw SQL because sqlc for
// SQLite doesn't support dynamic IN().
func (r *UserDataRepository) GetBatch(ctx context.Context, userID string, itemIDs []string) (map[string]*UserData, error) {
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

	query := fmt.Sprintf(
		`SELECT user_id, item_id, position_ticks, play_count, completed,
		        is_favorite, liked, audio_stream_index, subtitle_stream_index,
		        last_played_at, updated_at
		 FROM user_data
		 WHERE user_id = ? AND item_id IN (%s)`,
		joinStrings(placeholders, ","),
	)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get user data batch: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := make(map[string]*UserData, len(itemIDs))
	for rows.Next() {
		var row sqlc.UserDatum
		if err := rows.Scan(
			&row.UserID, &row.ItemID, &row.PositionTicks, &row.PlayCount, &row.Completed,
			&row.IsFavorite, &row.Liked, &row.AudioStreamIndex, &row.SubtitleStreamIndex,
			&row.LastPlayedAt, &row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan user data batch: %w", err)
		}
		ud := userDataFromRow(row)
		out[row.ItemID] = &ud
	}
	return out, rows.Err()
}

// UpdateProgress updates just the playback position and timestamps.
func (r *UserDataRepository) UpdateProgress(ctx context.Context, userID, itemID string, positionTicks int64, completed bool) error {
	now := time.Now()
	err := r.q.UpdateProgress(ctx, sqlc.UpdateProgressParams{
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
	now := time.Now()
	err := r.q.MarkPlayed(ctx, sqlc.MarkPlayedParams{
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
	now := time.Now()
	err := r.q.SetFavorite(ctx, sqlc.SetFavoriteParams{
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
func (r *UserDataRepository) ContinueWatching(ctx context.Context, userID string, limit int) ([]*ContinueWatchingItem, error) {
	if limit <= 0 {
		limit = 20
	}
	abandonedThreshold := time.Now().Add(-AbandonedAfter)
	rows, err := r.q.ContinueWatching(ctx, sqlc.ContinueWatchingParams{
		UserID:             userID,
		AbandonedThreshold: sql.NullTime{Time: abandonedThreshold, Valid: true},
		Limit:              int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("continue watching: %w", err)
	}
	return continueWatchingFromRows(rows), nil
}

// ContinueWatchingItem is the result for continue watching queries.
type ContinueWatchingItem struct {
	ItemID        string
	PositionTicks int64
	LastPlayedAt  *time.Time
	Title         string
	Type          string
	DurationTicks int64
	ParentID      string
	Container     string
	// SeasonNumber + EpisodeNumber pinpoint the episode inside its
	// show — needed for the "Sigue viendo S01E03" panel label.
	// 0 means "not an episode" or "missing", which the frontend
	// treats as absent (panel shows just the title).
	SeasonNumber  int
	EpisodeNumber int
	// SeriesID is the show this episode belongs to, derived via
	// `episode → season → series` in SQL. Empty when the row is a
	// movie or an orphaned episode without a parent chain. The
	// useResumeTarget hook keys series-scope matching off this field.
	SeriesID string
}

// Favorites returns items marked as favorite by the user.
func (r *UserDataRepository) Favorites(ctx context.Context, userID string, limit, offset int) ([]*FavoriteItem, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.q.ListFavorites(ctx, sqlc.ListFavoritesParams{
		UserID: userID,
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("favorites: %w", err)
	}
	return favoritesFromRows(rows), nil
}

// FavoriteItem is the result for favorite queries.
type FavoriteItem struct {
	ItemID        string
	FavoritedAt   time.Time
	Title         string
	Type          string
	Year          int
	DurationTicks int64
}

// NextUp returns the next unwatched episode for each series the user is watching.
// Uses raw SQL because the CTE references the same param twice and sqlc for
// SQLite doesn't handle duplicate named params in subqueries.
func (r *UserDataRepository) NextUp(ctx context.Context, userID string, limit int) ([]*NextUpItem, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx,
		`WITH watched_episodes AS (
		   SELECT i.id, i.parent_id AS season_id,
		          (SELECT parent_id FROM items WHERE id = i.parent_id) AS series_id,
		          i.episode_number, i.season_number,
		          ud.last_played_at
		   FROM user_data ud
		   JOIN items i ON i.id = ud.item_id
		   WHERE ud.user_id = ? AND i.type = 'episode' AND ud.completed = 1
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
		 JOIN items e ON e.type = 'episode' AND e.is_available = 1
		   AND (SELECT parent_id FROM items WHERE id = e.parent_id) = lw.series_id
		 JOIN items s ON s.id = lw.series_id
		 WHERE (COALESCE(e.season_number, 0) * 10000 + COALESCE(e.episode_number, 0)) > lw.last_order
		   AND NOT EXISTS (
		     SELECT 1 FROM user_data ud2
		     WHERE ud2.user_id = ? AND ud2.item_id = e.id AND ud2.completed = 1
		   )
		 GROUP BY lw.series_id
		 HAVING MIN(COALESCE(e.season_number, 0) * 10000 + COALESCE(e.episode_number, 0))
		 ORDER BY lw.last_played_at DESC
		 LIMIT ?`, userID, userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("next up: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var items []*NextUpItem
	for rows.Next() {
		item := &NextUpItem{}
		if err := rows.Scan(&item.EpisodeID, &item.EpisodeTitle,
			&item.SeasonNumber, &item.EpisodeNumber,
			&item.DurationTicks, &item.SeriesTitle, &item.SeriesID); err != nil {
			return nil, fmt.Errorf("scan next up: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// NextUpItem is the result for next-up queries.
type NextUpItem struct {
	EpisodeID     string
	EpisodeTitle  string
	SeasonNumber  *int
	EpisodeNumber *int
	DurationTicks int64
	SeriesTitle   string
	SeriesID      string
}

// Delete removes user data for a specific user+item pair.
// SeriesEpisodeProgress reports total + watched episode counts for a
// single series for one user. Used by the series detail page hero to
// render "Has visto X de Y episodios". Returns (0, 0) for a series
// with no episodes — caller decides whether to render anything.
func (r *UserDataRepository) SeriesEpisodeProgress(ctx context.Context, userID, seriesID string) (total, watched int, err error) {
	row, qerr := r.q.SeriesEpisodeProgress(ctx, sqlc.SeriesEpisodeProgressParams{
		UserID:   userID,
		SeriesID: seriesID,
	})
	if qerr != nil {
		return 0, 0, fmt.Errorf("series episode progress: %w", qerr)
	}
	return int(row.TotalEpisodes), int(row.WatchedEpisodes), nil
}

func (r *UserDataRepository) Delete(ctx context.Context, userID, itemID string) error {
	err := r.q.DeleteUserData(ctx, sqlc.DeleteUserDataParams{
		UserID: userID,
		ItemID: itemID,
	})
	if err != nil {
		return fmt.Errorf("delete user data: %w", err)
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

func nullableTimePtr(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func userDataFromRow(r sqlc.UserDatum) UserData {
	ud := UserData{
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

func continueWatchingFromRows(rows []sqlc.ContinueWatchingRow) []*ContinueWatchingItem {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*ContinueWatchingItem, len(rows))
	for i, r := range rows {
		item := &ContinueWatchingItem{
			ItemID:        r.ItemID,
			PositionTicks: r.PositionTicks.Int64,
			Title:         r.Title,
			Type:          r.Type,
			DurationTicks: r.DurationTicks.Int64,
			ParentID:      r.ParentID,
			Container:     r.Container,
			SeasonNumber:  int(r.SeasonNumber),
			EpisodeNumber: int(r.EpisodeNumber),
			SeriesID:      r.SeriesID,
		}
		if r.LastPlayedAt.Valid {
			item.LastPlayedAt = &r.LastPlayedAt.Time
		}
		out[i] = item
	}
	return out
}

func favoritesFromRows(rows []sqlc.ListFavoritesRow) []*FavoriteItem {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*FavoriteItem, len(rows))
	for i, r := range rows {
		out[i] = &FavoriteItem{
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
