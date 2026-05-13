package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
)

// ItemValueRepository is the gateway to the normalized tag store
// (`item_values` + `item_value_map`). Used today for genres; designed
// to host other tag-like facets (studios, content tags, mood, etc.)
// without further migrations.
//
// The synthetic `id` is "<type>:<clean_value>" so callers don't need
// uuid generation and INSERT-OR-IGNORE is naturally idempotent across
// items sharing the same value.
//
// Dual-dialect: Pattern A for the three sqlc-backed mutations plus one
// raw-SQL holdout (ListGenres — sqlc 1.31.1 truncates the trailing
// ORDER BY).
type ItemValueRepository struct {
	db *sql.DB
	sq *sqlc.Queries
	pq *sqlc_pg.Queries

	listGenresSQL string
}

func NewItemValueRepository(driver string, database *sql.DB) *ItemValueRepository {
	r := &ItemValueRepository{
		db: database,
		listGenresSQL: rewritePlaceholders(driver, `
			SELECT iv.value, COUNT(*) AS cnt
			FROM item_values iv
			JOIN item_value_map ivm ON ivm.value_id = iv.id
			JOIN items i ON i.id = ivm.item_id
			WHERE iv.type = ?
			  AND (? = '' OR i.type = ?)
			GROUP BY iv.id, iv.value, iv.clean_value
			ORDER BY cnt DESC, iv.clean_value ASC`),
	}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *ItemValueRepository) useSQLite() bool { return r.sq != nil }

// ItemValueTypeGenre is the canonical type tag for movie/series genres.
const ItemValueTypeGenre = "genre"

// SetGenres replaces the genre tag set for an item atomically. Empty
// input clears all genres for the item — useful when a TMDb refresh
// returns no genres so the UI doesn't keep stale chips around.
//
// Genre names are trimmed; clean_value is the lowercased trim used for
// case-insensitive lookups by the filter query.
func (r *ItemValueRepository) SetGenres(ctx context.Context, itemID string, genres []string) error {
	if itemID == "" {
		return fmt.Errorf("set genres: empty item id")
	}
	if r.useSQLite() {
		if err := r.sq.ClearItemValuesForItem(ctx, sqlc.ClearItemValuesForItemParams{
			ItemID: itemID,
			Type:   ItemValueTypeGenre,
		}); err != nil {
			return fmt.Errorf("clear genres for item %s: %w", itemID, err)
		}
		for _, raw := range genres {
			value := strings.TrimSpace(raw)
			if value == "" {
				continue
			}
			clean := strings.ToLower(value)
			id := ItemValueTypeGenre + ":" + clean
			if err := r.sq.UpsertItemValue(ctx, sqlc.UpsertItemValueParams{
				ID:         id,
				Type:       ItemValueTypeGenre,
				Value:      value,
				CleanValue: clean,
			}); err != nil {
				return fmt.Errorf("upsert genre %q: %w", value, err)
			}
			if err := r.sq.LinkItemValue(ctx, sqlc.LinkItemValueParams{
				ItemID:  itemID,
				ValueID: id,
			}); err != nil {
				return fmt.Errorf("link genre %q to item %s: %w", value, itemID, err)
			}
		}
		return nil
	}

	if err := r.pq.ClearItemValuesForItem(ctx, sqlc_pg.ClearItemValuesForItemParams{
		ItemID: itemID,
		Type:   ItemValueTypeGenre,
	}); err != nil {
		return fmt.Errorf("clear genres for item %s: %w", itemID, err)
	}
	for _, raw := range genres {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		clean := strings.ToLower(value)
		id := ItemValueTypeGenre + ":" + clean
		if err := r.pq.UpsertItemValue(ctx, sqlc_pg.UpsertItemValueParams{
			ID:         id,
			Type:       ItemValueTypeGenre,
			Value:      value,
			CleanValue: clean,
		}); err != nil {
			return fmt.Errorf("upsert genre %q: %w", value, err)
		}
		if err := r.pq.LinkItemValue(ctx, sqlc_pg.LinkItemValueParams{
			ItemID:  itemID,
			ValueID: id,
		}); err != nil {
			return fmt.Errorf("link genre %q to item %s: %w", value, itemID, err)
		}
	}
	return nil
}

// GenreValueID returns the synthesized id for a genre name. The filter
// query uses this to look up matches with a single equality check
// instead of a JOIN against item_values per call site.
func GenreValueID(name string) string {
	clean := strings.ToLower(strings.TrimSpace(name))
	if clean == "" {
		return ""
	}
	return ItemValueTypeGenre + ":" + clean
}

// GenreCount is the {name, count} pair the filter panel renders as a
// chip. Sorted by count desc on the way out so the panel doesn't have
// to re-sort.
type GenreCount struct {
	Name  string
	Count int64
}

// ListGenres returns the genre vocabulary across the catalogue,
// optionally scoped to an item type ("movie", "series"). Empty
// `itemType` returns the union — useful if a future "All" page wants
// the full vocabulary.
//
// Raw SQL because sqlc v1.31.1 truncates the trailing identifier of
// the final query in a file (see item_values.sql for context).
func (r *ItemValueRepository) ListGenres(ctx context.Context, itemType string) ([]GenreCount, error) {
	rows, err := r.db.QueryContext(ctx, r.listGenresSQL, ItemValueTypeGenre, itemType, itemType)
	if err != nil {
		return nil, fmt.Errorf("list genres (type=%q): %w", itemType, err)
	}
	defer rows.Close() //nolint:errcheck
	out := make([]GenreCount, 0)
	for rows.Next() {
		var g GenreCount
		if err := rows.Scan(&g.Name, &g.Count); err != nil {
			return nil, fmt.Errorf("scan genre row: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
