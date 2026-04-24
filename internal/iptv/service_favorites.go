package iptv

// Channel favorites — per-user overlay on top of the (library-scoped)
// channel catalog. All methods require the caller to have already
// authenticated the user and to have verified the channel belongs to a
// library the user can access — the handler layer does that before
// reaching here.

import (
	"context"

	"hubplay/internal/db"
)

// AddFavorite marks a channel as favorited by a user. Idempotent.
func (s *Service) AddFavorite(ctx context.Context, userID, channelID string) error {
	return s.favorites.Add(ctx, userID, channelID)
}

// RemoveFavorite unmarks a channel as favorited by a user. Idempotent.
func (s *Service) RemoveFavorite(ctx context.Context, userID, channelID string) error {
	return s.favorites.Remove(ctx, userID, channelID)
}

// IsFavorite reports whether a channel is currently favorited by a user.
func (s *Service) IsFavorite(ctx context.Context, userID, channelID string) (bool, error) {
	return s.favorites.Contains(ctx, userID, channelID)
}

// ListFavoriteIDs returns the user's favorite channel IDs (most-recent first).
// Cheap: one indexed query, no JOIN. Use when the client already has the
// channel list and just needs to toggle ♥ state.
func (s *Service) ListFavoriteIDs(ctx context.Context, userID string) ([]string, error) {
	return s.favorites.ListIDs(ctx, userID)
}

// ListFavoriteChannels returns the user's favorite channels with full channel
// data. Filters out inactive channels — a favorited channel that later went
// dark shouldn't surface as a dead card.
func (s *Service) ListFavoriteChannels(ctx context.Context, userID string) ([]*db.Channel, error) {
	return s.favorites.ListChannels(ctx, userID)
}
