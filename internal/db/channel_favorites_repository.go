package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
)

// ChannelFavorite is a single (user, channel, when) tuple.
type ChannelFavorite struct {
	UserID    string
	ChannelID string
	CreatedAt time.Time
}

// ChannelFavoritesRepository wraps sqlc-generated queries for the
// `user_channel_favorites` table. Thin adapter, same pattern as the other
// repositories in this package.
//
// Note: sqlc generates `AddChannelFavoriteParams`, `IsChannelFavorite`, etc.
// from `internal/db/queries/channel_favorites.sql`. If that generated file
// is missing, run `make sqlc` to regenerate.
type ChannelFavoritesRepository struct {
	q *sqlc.Queries
}

func NewChannelFavoritesRepository(database *sql.DB) *ChannelFavoritesRepository {
	return &ChannelFavoritesRepository{q: sqlc.New(database)}
}

// Add marks a channel as favorited by a user. Idempotent: the underlying
// query uses `ON CONFLICT DO NOTHING`, so calling twice is safe.
func (r *ChannelFavoritesRepository) Add(ctx context.Context, userID, channelID string) error {
	err := r.q.AddChannelFavorite(ctx, sqlc.AddChannelFavoriteParams{
		UserID:    userID,
		ChannelID: channelID,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("add channel favorite: %w", err)
	}
	return nil
}

// Remove unmarks a channel as favorited. No error if the row didn't exist —
// callers can treat the operation as idempotent.
func (r *ChannelFavoritesRepository) Remove(ctx context.Context, userID, channelID string) error {
	err := r.q.RemoveChannelFavorite(ctx, sqlc.RemoveChannelFavoriteParams{
		UserID:    userID,
		ChannelID: channelID,
	})
	if err != nil {
		return fmt.Errorf("remove channel favorite: %w", err)
	}
	return nil
}

// Contains reports whether the given user has the channel favorited.
// A fast path for single-channel checks (e.g. the detail view).
func (r *ChannelFavoritesRepository) Contains(ctx context.Context, userID, channelID string) (bool, error) {
	_, err := r.q.IsChannelFavorite(ctx, sqlc.IsChannelFavoriteParams{
		UserID:    userID,
		ChannelID: channelID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("is channel favorite: %w", err)
	}
	return true, nil
}

// ListIDs returns the user's favorite channel IDs in most-recent-first order.
// Used on page load to hydrate the frontend's local favorite set without
// having to join channels — the client already has the channel list.
func (r *ChannelFavoritesRepository) ListIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.q.ListChannelFavorites(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list channel favorites: %w", err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ChannelID)
	}
	return out, nil
}

// ListChannels returns the user's favorite channels joined with the
// `channels` table, filtered to active channels only. Inactive or deleted
// channels are silently skipped — favoriting a channel that later goes
// inactive shouldn't show up as a dead card.
func (r *ChannelFavoritesRepository) ListChannels(ctx context.Context, userID string) ([]*Channel, error) {
	rows, err := r.q.ListChannelFavoritesWithChannel(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list channel favorites with channel: %w", err)
	}
	out := make([]*Channel, 0, len(rows))
	for _, row := range rows {
		out = append(out, &Channel{
			ID:        row.ID,
			LibraryID: row.LibraryID,
			Name:      row.Name,
			Number:    int(row.Number),
			GroupName: row.GroupName,
			LogoURL:   row.LogoURL,
			StreamURL: row.StreamURL,
			TvgID:     row.TvgID,
			Language:  row.Language,
			Country:   row.Country,
			IsActive:  row.IsActive,
			AddedAt:   row.AddedAt,
		})
	}
	return out, nil
}
