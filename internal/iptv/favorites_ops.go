package iptv

// FavoritesOps aísla el surface de "channel favorites" del olor CC
// del audit 2026-05-14 (iptv.Service god-service de 45 métodos en
// 11 sub-features). Stateless excepto por el repo: 5 thin pass-
// throughs sobre `db.ChannelFavoritesRepository`.
//
// Mismo patrón embedding que cerró QQ (auth) y Z (library): el
// Service facade embed `*FavoritesOps` y los métodos se promueven
// vía method promotion intra-paquete — los handlers HTTP siguen
// llamando `svc.AddFavorite(...)` sin saber que vive en un
// sub-service.

import (
	"context"

	"hubplay/internal/db"
	iptvmodel "hubplay/internal/iptv/model"
)

// FavoritesOps gestiona los favoritos per-user sobre el catálogo
// de canales (library-scoped). Todos los métodos asumen que el
// caller ya autenticó al user y verificó el ACL del canal — el
// handler layer hace eso antes de llegar aquí.
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

// ListFavoriteIDs devuelve los IDs de canales favoritos del user
// (most-recent first). Barato: una sola query con índice, sin JOIN.
// Usado cuando el cliente ya tiene la lista de canales y sólo
// necesita togglear el estado ♥.
func (f *FavoritesOps) ListFavoriteIDs(ctx context.Context, userID string) ([]string, error) {
	return f.favorites.ListIDs(ctx, userID)
}

// ListFavoriteChannels devuelve los favoritos del user con la data
// completa del canal. Filtra los inactivos — un canal favoriteado
// que más tarde se apagó NO debería aparecer como dead card.
func (f *FavoritesOps) ListFavoriteChannels(ctx context.Context, userID string) ([]*iptvmodel.Channel, error) {
	return f.favorites.ListChannels(ctx, userID)
}
