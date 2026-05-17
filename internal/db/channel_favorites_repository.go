package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	iptvmodel "hubplay/internal/iptv/model"
	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
)

// ChannelFavoritesRepository wraps sqlc-generated queries for the
// `user_channel_favorites` table — Pattern A dual-dialect. Number is
// INTEGER → NullInt64 on SQLite, NullInt32 on Postgres, branched
// per backend.
type ChannelFavoritesRepository struct {
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewChannelFavoritesRepository(driver string, database *sql.DB) *ChannelFavoritesRepository {
	r := &ChannelFavoritesRepository{}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *ChannelFavoritesRepository) useSQLite() bool { return r.sq != nil }

// Add marks a channel as favorited by a user. Idempotent: the underlying
// query uses `ON CONFLICT DO NOTHING`, so calling twice is safe.
func (r *ChannelFavoritesRepository) Add(ctx context.Context, userID, channelID string) error {
	now := time.Now().UTC()
	var err error
	if r.useSQLite() {
		err = r.sq.AddChannelFavorite(ctx, sqlc.AddChannelFavoriteParams{
			UserID:    userID,
			ChannelID: channelID,
			CreatedAt: now,
		})
	} else {
		err = r.pq.AddChannelFavorite(ctx, sqlc_pg.AddChannelFavoriteParams{
			UserID:    userID,
			ChannelID: channelID,
			CreatedAt: now,
		})
	}
	if err != nil {
		return fmt.Errorf("add channel favorite: %w", err)
	}
	return nil
}

// Remove unmarks a channel as favorited. No error if the row didn't exist —
// callers can treat the operation as idempotent.
func (r *ChannelFavoritesRepository) Remove(ctx context.Context, userID, channelID string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.RemoveChannelFavorite(ctx, sqlc.RemoveChannelFavoriteParams{
			UserID:    userID,
			ChannelID: channelID,
		})
	} else {
		err = r.pq.RemoveChannelFavorite(ctx, sqlc_pg.RemoveChannelFavoriteParams{
			UserID:    userID,
			ChannelID: channelID,
		})
	}
	if err != nil {
		return fmt.Errorf("remove channel favorite: %w", err)
	}
	return nil
}

// Contains reports whether the given user has the channel favorited.
// A fast path for single-channel checks (e.g. the detail view).
func (r *ChannelFavoritesRepository) Contains(ctx context.Context, userID, channelID string) (bool, error) {
	var err error
	if r.useSQLite() {
		_, err = r.sq.IsChannelFavorite(ctx, sqlc.IsChannelFavoriteParams{
			UserID:    userID,
			ChannelID: channelID,
		})
	} else {
		_, err = r.pq.IsChannelFavorite(ctx, sqlc_pg.IsChannelFavoriteParams{
			UserID:    userID,
			ChannelID: channelID,
		})
	}
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
	if r.useSQLite() {
		rows, err := r.sq.ListChannelFavorites(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("list channel favorites: %w", err)
		}
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, row.ChannelID)
		}
		return out, nil
	}
	rows, err := r.pq.ListChannelFavorites(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list channel favorites: %w", err)
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ChannelID)
	}
	return out, nil
}

// ListChannels returns the user's favorite channels joined with the
// `channels` table, filtered to active channels only. Inactive or deleted
// channels are silently skipped — favoriting a channel that later goes
// inactive shouldn't show up as a dead card.
func (r *ChannelFavoritesRepository) ListChannels(ctx context.Context, userID string) ([]*iptvmodel.Channel, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListChannelFavoritesWithChannel(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("list channel favorites with channel: %w", err)
		}
		out := make([]*iptvmodel.Channel, 0, len(rows))
		for _, row := range rows {
			ch := &iptvmodel.Channel{
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
			out = append(out, ch)
		}
		return out, nil
	}
	rows, err := r.pq.ListChannelFavoritesWithChannel(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list channel favorites with channel: %w", err)
	}
	out := make([]*iptvmodel.Channel, 0, len(rows))
	for _, row := range rows {
		ch := &iptvmodel.Channel{
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
		out = append(out, ch)
	}
	return out, nil
}
