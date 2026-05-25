package iptv

// FavoritesOps gestiona favoritos de canales per-user. Stateless
// excepto por el repo. Embebido en Service vía method promotion.

import (
	"context"

	"hubplay/internal/db"
	iptvmodel "hubplay/internal/iptv/model"
)

// FavoritesOps — los métodos asumen que el caller ya autenticó
// y verificó ACL. El handler lo hace antes de llegar aquí.
type FavoritesOps struct {
	favorites *db.ChannelFavoritesRepository
}

func newFavoritesOps(favorites *db.ChannelFavoritesRepository) *FavoritesOps {
	return &FavoritesOps{favorites: favorites}
}

// AddFavorite marca un canal como favorito del user. Idempotente.
func (f *FavoritesOps) AddFavorite(ctx context.Context, userID, channelID string) error {
	return f.favorites.Add(ctx, userID, channelID)
}

// RemoveFavorite desmarca un canal como favorito del user. Idempotente.
func (f *FavoritesOps) RemoveFavorite(ctx context.Context, userID, channelID string) error {
	return f.favorites.Remove(ctx, userID, channelID)
}

// IsFavorite reporta si el canal está actualmente favoriteado por
// el user.
func (f *FavoritesOps) IsFavorite(ctx context.Context, userID, channelID string) (bool, error) {
	return f.favorites.Contains(ctx, userID, channelID)
}

// ListFavoriteIDs devuelve los IDs de canales favoritos (most-recent first).
// Una sola query con índice, sin JOIN.
func (f *FavoritesOps) ListFavoriteIDs(ctx context.Context, userID string) ([]string, error) {
	return f.favorites.ListIDs(ctx, userID)
}

// ListFavoriteChannels devuelve los favoritos con data completa.
// Filtra inactivos para no mostrar dead cards.
func (f *FavoritesOps) ListFavoriteChannels(ctx context.Context, userID string) ([]*iptvmodel.Channel, error) {
	return f.favorites.ListChannels(ctx, userID)
}
