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

// ContinueWatching returns items that the user started but hasn't completed, ordered by last played.
func (r *UserDataRepository) ContinueWatching(ctx context.Context, userID string, limit int) ([]*ContinueWatchingItem, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.q.ContinueWatching(ctx, sqlc.ContinueWatchingParams{
		UserID: userID,
		Limit:  int64(limit),
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
