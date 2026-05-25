package library

import (
	"context"
	"fmt"
	"log/slog"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
)

// ItemQueries agrupa los métodos read-only sobre items, separados de
// library.Service para reducir el tamaño del facade.
type ItemQueries struct {
	items      *db.ItemRepository
	streams    *db.MediaStreamRepository
	images     *db.ImageRepository
	itemValues *db.ItemValueRepository
	libraries  *db.LibraryRepository
	channels   *db.ChannelRepository
	logger     *slog.Logger
}

func newItemQueries(
	items *db.ItemRepository,
	streams *db.MediaStreamRepository,
	images *db.ImageRepository,
	itemValues *db.ItemValueRepository,
	libraries *db.LibraryRepository,
	channels *db.ChannelRepository,
	logger *slog.Logger,
) *ItemQueries {
	return &ItemQueries{
		items:      items,
		streams:    streams,
		images:     images,
		itemValues: itemValues,
		libraries:  libraries,
		channels:   channels,
		logger:     logger,
	}
}

// ListItems delega al repo con filters. Limit clampeado a [1, 100].
func (q *ItemQueries) ListItems(ctx context.Context, filter librarymodel.ItemFilter) ([]*librarymodel.Item, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	return q.items.List(ctx, filter)
}

// ListGenres delega al store normalizado de tags.
// itemType opcional scopea el vocabulario por tipo de contenido.
func (q *ItemQueries) ListGenres(ctx context.Context, itemType string) ([]librarymodel.GenreCount, error) {
	if q.itemValues == nil {
		return nil, nil
	}
	return q.itemValues.ListGenres(ctx, itemType)
}

func (q *ItemQueries) GetItem(ctx context.Context, id string) (*librarymodel.Item, error) {
	return q.items.GetByID(ctx, id)
}

func (q *ItemQueries) GetItemChildren(ctx context.Context, id string) ([]*librarymodel.Item, error) {
	return q.items.GetChildren(ctx, id)
}

func (q *ItemQueries) GetItemChildCounts(ctx context.Context, parentIDs []string) (map[string]int, error) {
	return q.items.ChildCountsByParents(ctx, parentIDs)
}

func (q *ItemQueries) GetItemStreams(ctx context.Context, itemID string) ([]*librarymodel.MediaStream, error) {
	return q.streams.ListByItem(ctx, itemID)
}

func (q *ItemQueries) GetItemImages(ctx context.Context, itemID string) ([]*librarymodel.Image, error) {
	return q.images.ListByItem(ctx, itemID)
}

// LatestItems devuelve los items más recientes de una library (o global si
// libraryID == ""). capRating no-vacío filtra ratings; "" desactiva el filtro.
func (q *ItemQueries) LatestItems(ctx context.Context, libraryID string, itemType string, limit int, capRating string) ([]*librarymodel.Item, error) {
	allowed := AllowedRatingsAtMost(capRating)
	return q.items.LatestItems(ctx, libraryID, itemType, limit, allowed...)
}

// LatestSeriesByActivity: query dedicada para el rail de shows.
// Devuelve timestamp de actividad + count de episodios nuevos por serie.
func (q *ItemQueries) LatestSeriesByActivity(ctx context.Context, libraryID string, limit int) ([]*librarymodel.LatestSeriesActivity, error) {
	return q.items.LatestSeriesByActivity(ctx, libraryID, limit)
}

// ItemCount devuelve el total de items en una library. Para livetv
// despacha al channels repo (livetv no popula la tabla items).
func (q *ItemQueries) ItemCount(ctx context.Context, libraryID string) (int, error) {
	lib, err := q.libraries.GetByID(ctx, libraryID)
	if err == nil && lib != nil && lib.ContentType == "livetv" && q.channels != nil {
		chs, err := q.channels.ListByLibrary(ctx, libraryID, false)
		if err != nil {
			return 0, fmt.Errorf("count channels: %w", err)
		}
		return len(chs), nil
	}
	return q.items.CountByLibrary(ctx, libraryID)
}
