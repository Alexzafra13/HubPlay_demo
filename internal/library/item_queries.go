package library

import (
	"context"
	"fmt"
	"log/slog"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
)

// ItemQueries aísla los 10 métodos read-only sobre items del olor Z
// del audit 2026-05-14 (library.Service god-service). Cubre las 3
// sub-responsabilidades 4 (item queries con rating-cap), 5
// (latest/rails) y 6 (telemetría ItemCount) de la lista del audit.
//
// La mayoría son pass-throughs delgados a `db.ItemRepository` con un
// poco de orquestación (ItemCount dispatch livetv → channels, etc.).
// El audit sugería que el handler llamase al repo directamente, pero
// mantener esta interfaz expone `LibraryService` consistente sin que
// el handler tenga que conocer si el query backend es Postgres,
// SQLite o un repo decorator de rating cap.
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

// ListItems delega al items repo con filters. Limit clampado a
// [1, 100] para evitar que un cliente malicioso (o test) pida 10⁶
// filas en una sola query.
func (q *ItemQueries) ListItems(ctx context.Context, filter librarymodel.ItemFilter) ([]*librarymodel.Item, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	return q.items.List(ctx, filter)
}

// ListGenres delega al store normalizado de tags. itemType opcional
// scopea el vocabulary así /movies y /series sólo ven chips
// relevantes.
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

// GetItemChildCounts es un pass-through thin al items repo. Vive en
// el service layer puramente para que el handler dependa de la
// interfaz LibraryService (testable via el mock existente) en lugar
// de meterse en *db.ItemRepository directamente.
func (q *ItemQueries) GetItemChildCounts(ctx context.Context, parentIDs []string) (map[string]int, error) {
	return q.items.ChildCountsByParents(ctx, parentIDs)
}

func (q *ItemQueries) GetItemStreams(ctx context.Context, itemID string) ([]*librarymodel.MediaStream, error) {
	return q.streams.ListByItem(ctx, itemID)
}

func (q *ItemQueries) GetItemImages(ctx context.Context, itemID string) ([]*librarymodel.Image, error) {
	return q.images.ListByItem(ctx, itemID)
}

// LatestItems devuelve los items añadidos más recientemente en una
// library (o globalmente cuando libraryID == ""). Cuando `capRating`
// es non-empty el result set se filtra a ratings at-or-below el cap;
// pasar "" deshabilita el filtro (profile sin restricciones / context
// admin).
func (q *ItemQueries) LatestItems(ctx context.Context, libraryID string, itemType string, limit int, capRating string) ([]*librarymodel.Item, error) {
	allowed := AllowedRatingsAtMost(capRating)
	return q.items.LatestItems(ctx, libraryID, itemType, limit, allowed...)
}

// LatestSeriesByActivity wrappea la query dedicada de shows-library
// rail. Devuelto al API handler para que el wire pueda surface el
// stamp de activity per-series + el count de nuevos episodios sin
// un extra round-trip.
func (q *ItemQueries) LatestSeriesByActivity(ctx context.Context, libraryID string, limit int) ([]*librarymodel.LatestSeriesActivity, error) {
	return q.items.LatestSeriesByActivity(ctx, libraryID, limit)
}

// ItemCount devuelve el número de items en una library. Dispatcha al
// channels repo cuando la library es livetv (esas no populan la
// tabla items; su catálogo vive en channels). Así el admin UI
// muestra un count significativo para cada tipo de library sin
// branching en el handler.
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
