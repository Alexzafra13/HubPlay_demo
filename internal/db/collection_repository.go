package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"hubplay/internal/db/sqlc"
)

// Collection is a movie saga (X-Men, MCU, Toy Story, …) surfaced as a
// first-class entity. Backed by TMDb's `belongs_to_collection` record;
// TMDbID is the upstream id we dedupe on so the same saga collapses
// across every member movie.
type Collection struct {
	ID          string
	TMDBID      int64
	Name        string
	Overview    string
	PosterURL   string
	BackdropURL string
}

// CollectionListEntry is the {collection + member count} pair the
// /collections browse page renders. Sorted by member count desc on
// the way out.
type CollectionListEntry struct {
	ID          string
	Name        string
	PosterURL   string
	BackdropURL string
	ItemCount   int64
}

type CollectionRepository struct {
	db *sql.DB // ListItemsForCollection uses raw SQL (see queries/collections.sql)
	q  *sqlc.Queries
}

func NewCollectionRepository(database *sql.DB) *CollectionRepository {
	return &CollectionRepository{db: database, q: sqlc.New(database)}
}

// CollectionID builds the canonical row id from a TMDb collection id.
// Exposed so the scanner can build the id without re-implementing
// the recipe — the migration's INSERT and Go's UpsertCollection both
// use `collection:<tmdb_id>` so the row key is predictable from the
// provider data alone.
func CollectionID(tmdbID int64) string {
	if tmdbID <= 0 {
		return ""
	}
	return "collection:" + strconv.FormatInt(tmdbID, 10)
}

// EnsureCollection upserts a collection row and returns its id. Empty
// name → ("", nil): caller treats absence as "no collection for this
// item, leave metadata.collection_id NULL".
//
// Raw SQL because the ON CONFLICT clause uses CASE expressions and
// sqlc v1.31.1 truncates the trailing `END` of the final query in a
// file. The CASE clauses preserve non-empty artwork on re-scan: a
// re-fetch that comes back without a backdrop won't wipe the one we
// already had.
func (r *CollectionRepository) EnsureCollection(ctx context.Context, tmdbID int64, name, overview, posterURL, backdropURL string) (string, error) {
	if tmdbID <= 0 || name == "" {
		return "", nil
	}
	id := CollectionID(tmdbID)
	const query = `
		INSERT INTO collections (id, tmdb_id, name, overview, poster_url, backdrop_url)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(tmdb_id) DO UPDATE SET
		    name         = excluded.name,
		    overview     = CASE WHEN excluded.overview <> '' THEN excluded.overview ELSE collections.overview END,
		    poster_url   = CASE WHEN excluded.poster_url <> '' THEN excluded.poster_url ELSE collections.poster_url END,
		    backdrop_url = CASE WHEN excluded.backdrop_url <> '' THEN excluded.backdrop_url ELSE collections.backdrop_url END`
	if _, err := r.db.ExecContext(ctx, query, id, tmdbID, name, overview, posterURL, backdropURL); err != nil {
		return "", fmt.Errorf("upsert collection (tmdb=%d): %w", tmdbID, err)
	}
	return id, nil
}

// GetByID fetches the canonical row for /collections/{id} rendering.
// Returns (nil, nil) when no collection matches — handler converts
// to 404 so a stale link reads as "not found" instead of crashing.
func (r *CollectionRepository) GetByID(ctx context.Context, id string) (*Collection, error) {
	row, err := r.q.GetCollectionByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get collection by id: %w", err)
	}
	c := collectionFromRow(row)
	return &c, nil
}

// List returns every collection that has at least one member movie
// in the catalogue, sorted by member count desc.
//
// Raw SQL because the trailing ORDER BY ASC hits the sqlc v1.31.1
// parser truncation we work around in three other places already.
func (r *CollectionRepository) List(ctx context.Context) ([]*CollectionListEntry, error) {
	const query = `
		SELECT
		    c.id,
		    c.name,
		    c.poster_url,
		    c.backdrop_url,
		    (SELECT COUNT(*) FROM metadata m WHERE m.collection_id = c.id) AS item_count
		FROM collections c
		WHERE EXISTS (SELECT 1 FROM metadata m WHERE m.collection_id = c.id)
		ORDER BY item_count DESC, c.name ASC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	out := make([]*CollectionListEntry, 0)
	for rows.Next() {
		var e CollectionListEntry
		if err := rows.Scan(&e.ID, &e.Name, &e.PosterURL, &e.BackdropURL, &e.ItemCount); err != nil {
			return nil, fmt.Errorf("scan collection list row: %w", err)
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// CollectionItem mirrors StudioItem — same {id, type, title, year,
// poster_url} grid shape so the same Tile component can render
// either surface.
type CollectionItem struct {
	ID             string
	Type           string
	Title          string
	Year           int
	PrimaryImageID string
}

// ListItemsForCollection returns the catalogue's movies linked to
// this collection id, sorted year-asc (sagas read in release order
// the way you'd watch them — Star Wars 1977 first, then 1980, etc).
// Raw SQL because the trailing ORDER BY hits the sqlc parser
// truncation we work around in three other places already.
func (r *CollectionRepository) ListItemsForCollection(ctx context.Context, collectionID string) ([]*CollectionItem, error) {
	const query = `
		SELECT
		    i.id,
		    i.type,
		    i.title,
		    COALESCE(i.year, 0) AS year,
		    COALESCE(img.id, '') AS primary_image_id
		FROM metadata m
		JOIN items i ON i.id = m.item_id
		LEFT JOIN images img
		    ON img.item_id = i.id AND img.type = 'primary' AND img.is_primary = 1
		WHERE m.collection_id = ?
		  AND i.is_available = 1
		  AND i.type = 'movie'
		ORDER BY COALESCE(i.year, 0) ASC, i.title ASC`
	rows, err := r.db.QueryContext(ctx, query, collectionID)
	if err != nil {
		return nil, fmt.Errorf("list items for collection %s: %w", collectionID, err)
	}
	defer rows.Close() //nolint:errcheck
	out := make([]*CollectionItem, 0)
	for rows.Next() {
		var it CollectionItem
		if err := rows.Scan(&it.ID, &it.Type, &it.Title, &it.Year, &it.PrimaryImageID); err != nil {
			return nil, fmt.Errorf("scan collection item: %w", err)
		}
		out = append(out, &it)
	}
	return out, rows.Err()
}

// SetItemCollection links an item's metadata row to a collection.
// Empty collectionID clears the link (a metadata refresh that no
// longer returns a belongs_to_collection drops the link).
func (r *CollectionRepository) SetItemCollection(ctx context.Context, itemID, collectionID string) error {
	const query = `UPDATE metadata SET collection_id = NULLIF(?, '') WHERE item_id = ?`
	if _, err := r.db.ExecContext(ctx, query, collectionID, itemID); err != nil {
		return fmt.Errorf("set item collection: %w", err)
	}
	return nil
}

func collectionFromRow(row sqlc.Collection) Collection {
	return Collection{
		ID:          row.ID,
		TMDBID:      row.TmdbID,
		Name:        row.Name,
		Overview:    row.Overview,
		PosterURL:   row.PosterUrl,
		BackdropURL: row.BackdropUrl,
	}
}
