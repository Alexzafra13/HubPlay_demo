package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// UserData holds a user's interaction data for a specific item.
type UserData struct {
	UserID             string
	ItemID             string
	PositionTicks      int64
	PlayCount          int
	Completed          bool
	IsFavorite         bool
	Liked              *bool // nil = no opinion
	AudioStreamIndex   *int
	SubtitleStreamIndex *int
	LastPlayedAt       *time.Time
	UpdatedAt          time.Time
}

type UserDataRepository struct {
	db *sql.DB
}

func NewUserDataRepository(database *sql.DB) *UserDataRepository {
	return &UserDataRepository{db: database}
}

// Upsert creates or updates user data for an item.
func (r *UserDataRepository) Upsert(ctx context.Context, ud *UserData) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO user_data (user_id, item_id, position_ticks, play_count, completed,
		 is_favorite, liked, audio_stream_index, subtitle_stream_index, last_played_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, item_id) DO UPDATE SET
		   position_ticks = excluded.position_ticks,
		   play_count = excluded.play_count,
		   completed = excluded.completed,
		   is_favorite = excluded.is_favorite,
		   liked = excluded.liked,
		   audio_stream_index = excluded.audio_stream_index,
		   subtitle_stream_index = excluded.subtitle_stream_index,
		   last_played_at = excluded.last_played_at,
		   updated_at = excluded.updated_at`,
		ud.UserID, ud.ItemID, ud.PositionTicks, ud.PlayCount, ud.Completed,
		ud.IsFavorite, ud.Liked, ud.AudioStreamIndex, ud.SubtitleStreamIndex,
		ud.LastPlayedAt, ud.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert user data: %w", err)
	}
	return nil
}

// Get returns user data for a specific user+item pair.
func (r *UserDataRepository) Get(ctx context.Context, userID, itemID string) (*UserData, error) {
	ud := &UserData{}
	err := r.db.QueryRowContext(ctx,
		`SELECT user_id, item_id, position_ticks, play_count, completed,
		        is_favorite, liked, audio_stream_index, subtitle_stream_index,
		        last_played_at, updated_at
		 FROM user_data WHERE user_id = ? AND item_id = ?`, userID, itemID,
	).Scan(&ud.UserID, &ud.ItemID, &ud.PositionTicks, &ud.PlayCount, &ud.Completed,
		&ud.IsFavorite, &ud.Liked, &ud.AudioStreamIndex, &ud.SubtitleStreamIndex,
		&ud.LastPlayedAt, &ud.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // No data yet — not an error
	}
	if err != nil {
		return nil, fmt.Errorf("get user data: %w", err)
	}
	return ud, nil
}

// UpdateProgress updates just the playback position and timestamps.
func (r *UserDataRepository) UpdateProgress(ctx context.Context, userID, itemID string, positionTicks int64, completed bool) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO user_data (user_id, item_id, position_ticks, completed, last_played_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, item_id) DO UPDATE SET
		   position_ticks = excluded.position_ticks,
		   completed = excluded.completed,
		   last_played_at = excluded.last_played_at,
		   updated_at = excluded.updated_at`,
		userID, itemID, positionTicks, completed, now, now,
	)
	if err != nil {
		return fmt.Errorf("update progress: %w", err)
	}
	return nil
}

// MarkPlayed increments play count and marks completed.
func (r *UserDataRepository) MarkPlayed(ctx context.Context, userID, itemID string) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO user_data (user_id, item_id, play_count, completed, last_played_at, updated_at)
		 VALUES (?, ?, 1, 1, ?, ?)
		 ON CONFLICT(user_id, item_id) DO UPDATE SET
		   play_count = user_data.play_count + 1,
		   completed = 1,
		   position_ticks = 0,
		   last_played_at = excluded.last_played_at,
		   updated_at = excluded.updated_at`,
		userID, itemID, now, now,
	)
	if err != nil {
		return fmt.Errorf("mark played: %w", err)
	}
	return nil
}

// SetFavorite sets or unsets favorite for an item.
func (r *UserDataRepository) SetFavorite(ctx context.Context, userID, itemID string, favorite bool) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO user_data (user_id, item_id, is_favorite, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(user_id, item_id) DO UPDATE SET
		   is_favorite = excluded.is_favorite,
		   updated_at = excluded.updated_at`,
		userID, itemID, favorite, now,
	)
	if err != nil {
		return fmt.Errorf("set favorite: %w", err)
	}
	return nil
}

// ContinueWatching returns items that the user started but hasn't completed, ordered by last played.
func (r *UserDataRepository) ContinueWatching(ctx context.Context, userID string, limit int) ([]*ContinueWatchingItem, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT ud.item_id, ud.position_ticks, ud.last_played_at,
		        i.title, i.type, i.duration_ticks, COALESCE(i.parent_id, ''),
		        COALESCE(i.container, '')
		 FROM user_data ud
		 JOIN items i ON i.id = ud.item_id
		 WHERE ud.user_id = ? AND ud.completed = 0 AND ud.position_ticks > 0
		   AND i.is_available = 1
		 ORDER BY ud.last_played_at DESC
		 LIMIT ?`, userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("continue watching: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var items []*ContinueWatchingItem
	for rows.Next() {
		item := &ContinueWatchingItem{}
		if err := rows.Scan(&item.ItemID, &item.PositionTicks, &item.LastPlayedAt,
			&item.Title, &item.Type, &item.DurationTicks, &item.ParentID,
			&item.Container); err != nil {
			return nil, fmt.Errorf("scan continue watching: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
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
}

// Favorites returns items marked as favorite by the user.
func (r *UserDataRepository) Favorites(ctx context.Context, userID string, limit, offset int) ([]*FavoriteItem, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT ud.item_id, ud.updated_at,
		        i.title, i.type, i.year, i.duration_ticks
		 FROM user_data ud
		 JOIN items i ON i.id = ud.item_id
		 WHERE ud.user_id = ? AND ud.is_favorite = 1
		   AND i.is_available = 1
		 ORDER BY ud.updated_at DESC
		 LIMIT ? OFFSET ?`, userID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("favorites: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var items []*FavoriteItem
	for rows.Next() {
		item := &FavoriteItem{}
		if err := rows.Scan(&item.ItemID, &item.FavoritedAt,
			&item.Title, &item.Type, &item.Year, &item.DurationTicks); err != nil {
			return nil, fmt.Errorf("scan favorite: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
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
func (r *UserDataRepository) NextUp(ctx context.Context, userID string, limit int) ([]*NextUpItem, error) {
	if limit <= 0 {
		limit = 20
	}
	// Find next unwatched episode per series:
	// 1. Get series where user has watched at least one episode
	// 2. Find next episode after the last watched one
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
func (r *UserDataRepository) Delete(ctx context.Context, userID, itemID string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM user_data WHERE user_id = ? AND item_id = ?`, userID, itemID,
	)
	if err != nil {
		return fmt.Errorf("delete user data: %w", err)
	}
	return nil
}
