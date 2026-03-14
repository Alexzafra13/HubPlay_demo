package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"hubplay/internal/domain"
)

type Item struct {
	ID              string
	LibraryID       string
	ParentID        string // empty if root item (movie, series)
	Type            string // movie, series, season, episode, audio, album, artist
	Title           string
	SortTitle       string
	OriginalTitle   string
	Year            int
	Path            string
	Size            int64
	DurationTicks   int64
	Container       string
	Fingerprint     string
	SeasonNumber    *int
	EpisodeNumber   *int
	CommunityRating *float64
	ContentRating   string
	PremiereDate    *time.Time
	AddedAt         time.Time
	UpdatedAt       time.Time
	IsAvailable     bool
}

type ItemFilter struct {
	LibraryID string
	ParentID  string // filter by parent (e.g., episodes of a season)
	Type      string // filter by type
	Query     string // FTS search
	Limit     int
	Offset    int
	SortBy    string // sort_title, added_at, year
	SortOrder string // asc, desc
}

type ItemRepository struct {
	db *sql.DB
}

func NewItemRepository(database *sql.DB) *ItemRepository {
	return &ItemRepository{db: database}
}

func (r *ItemRepository) Create(ctx context.Context, item *Item) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO items (id, library_id, parent_id, type, title, sort_title, original_title,
		 year, path, size, duration_ticks, container, fingerprint, season_number, episode_number,
		 community_rating, content_rating, premiere_date, added_at, updated_at, is_available)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.LibraryID, nullStr(item.ParentID), item.Type, item.Title, item.SortTitle,
		nullStr(item.OriginalTitle), item.Year, nullStr(item.Path), item.Size, item.DurationTicks,
		nullStr(item.Container), nullStr(item.Fingerprint), item.SeasonNumber, item.EpisodeNumber,
		item.CommunityRating, nullStr(item.ContentRating), item.PremiereDate,
		item.AddedAt, item.UpdatedAt, item.IsAvailable,
	)
	if err != nil {
		return fmt.Errorf("create item: %w", err)
	}
	return nil
}

// nullStr returns nil (SQL NULL) for empty strings, keeping the value otherwise.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (r *ItemRepository) GetByID(ctx context.Context, id string) (*Item, error) {
	item := &Item{}
	var n itemNullables

	err := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, parent_id, type, title, sort_title, original_title,
		        year, path, size, duration_ticks, container, fingerprint, season_number,
		        episode_number, community_rating, content_rating, premiere_date,
		        added_at, updated_at, is_available
		 FROM items WHERE id = ?`, id,
	).Scan(fullScanDests(item, &n)...)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("item %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get item %s: %w", id, err)
	}

	n.applyFull(item)
	return item, nil
}

func (r *ItemRepository) GetByPath(ctx context.Context, path string) (*Item, error) {
	item := &Item{}
	var n itemNullables

	err := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, parent_id, type, title, sort_title, original_title,
		        year, path, size, duration_ticks, container, fingerprint, season_number,
		        episode_number, community_rating, content_rating, premiere_date,
		        added_at, updated_at, is_available
		 FROM items WHERE path = ?`, path,
	).Scan(fullScanDests(item, &n)...)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("item path %q: %w", path, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get item by path %q: %w", path, err)
	}

	n.applyFull(item)
	return item, nil
}

func (r *ItemRepository) List(ctx context.Context, filter ItemFilter) ([]*Item, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.SortBy == "" {
		filter.SortBy = "sort_title"
	}
	if filter.SortOrder == "" {
		filter.SortOrder = "asc"
	}

	// Validate sort to prevent injection
	validSorts := map[string]bool{"sort_title": true, "added_at": true, "year": true, "episode_number": true}
	if !validSorts[filter.SortBy] {
		filter.SortBy = "sort_title"
	}
	if filter.SortOrder != "asc" && filter.SortOrder != "desc" {
		filter.SortOrder = "asc"
	}

	where := "WHERE 1=1"
	args := []any{}

	if filter.LibraryID != "" {
		where += " AND library_id = ?"
		args = append(args, filter.LibraryID)
	}
	if filter.ParentID != "" {
		where += " AND parent_id = ?"
		args = append(args, filter.ParentID)
	} else if filter.Type == "" {
		// If no parent filter and no type filter, show root items only
		where += " AND parent_id IS NULL"
	}
	if filter.Type != "" {
		where += " AND type = ?"
		args = append(args, filter.Type)
	}

	// Full-text search via FTS5
	if filter.Query != "" {
		where += " AND rowid IN (SELECT rowid FROM items_fts WHERE items_fts MATCH ?)"
		args = append(args, filter.Query+"*")
	}

	// Count
	var total int
	countSQL := "SELECT COUNT(*) FROM items " + where
	if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count items: %w", err)
	}

	// Query
	querySQL := fmt.Sprintf(
		`SELECT id, library_id, parent_id, type, title, sort_title, original_title,
		        year, path, size, duration_ticks, container, season_number, episode_number,
		        community_rating, added_at, updated_at, is_available
		 FROM items %s ORDER BY %s %s LIMIT ? OFFSET ?`,
		where, filter.SortBy, filter.SortOrder,
	)
	args = append(args, filter.Limit, filter.Offset)

	rows, err := r.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var items []*Item
	for rows.Next() {
		item := &Item{}
		var n itemNullables
		if err := rows.Scan(listScanDests(item, &n)...); err != nil {
			return nil, 0, fmt.Errorf("scan item: %w", err)
		}
		n.applyList(item)
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *ItemRepository) Update(ctx context.Context, item *Item) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE items SET title = ?, sort_title = ?, original_title = ?,
		        year = ?, size = ?, duration_ticks = ?, container = ?,
		        fingerprint = ?, season_number = ?, episode_number = ?,
		        community_rating = ?, content_rating = ?,
		        premiere_date = ?, updated_at = ?, is_available = ?
		 WHERE id = ?`,
		item.Title, item.SortTitle, nullStr(item.OriginalTitle), item.Year, item.Size,
		item.DurationTicks, nullStr(item.Container), nullStr(item.Fingerprint),
		item.SeasonNumber, item.EpisodeNumber, item.CommunityRating,
		nullStr(item.ContentRating), item.PremiereDate, item.UpdatedAt, item.IsAvailable, item.ID,
	)
	if err != nil {
		return fmt.Errorf("update item: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("item %s: %w", item.ID, domain.ErrNotFound)
	}
	return nil
}

func (r *ItemRepository) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("item %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// DeleteByLibrary removes all items in a library.
func (r *ItemRepository) DeleteByLibrary(ctx context.Context, libraryID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM items WHERE library_id = ?`, libraryID)
	if err != nil {
		return fmt.Errorf("delete items by library: %w", err)
	}
	return nil
}

// CountByLibrary returns the number of items in a library.
func (r *ItemRepository) CountByLibrary(ctx context.Context, libraryID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM items WHERE library_id = ?`, libraryID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count items by library: %w", err)
	}
	return count, nil
}

// GetChildren returns direct children of an item.
func (r *ItemRepository) GetChildren(ctx context.Context, parentID string) ([]*Item, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, library_id, parent_id, type, title, sort_title, original_title,
		        year, path, size, duration_ticks, container, season_number, episode_number,
		        community_rating, added_at, updated_at, is_available
		 FROM items WHERE parent_id = ? ORDER BY COALESCE(season_number, 0), COALESCE(episode_number, 0), sort_title`,
		parentID,
	)
	if err != nil {
		return nil, fmt.Errorf("get children: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var items []*Item
	for rows.Next() {
		item := &Item{}
		var n itemNullables
		if err := rows.Scan(listScanDests(item, &n)...); err != nil {
			return nil, fmt.Errorf("scan child item: %w", err)
		}
		n.applyList(item)
		items = append(items, item)
	}
	return items, rows.Err()
}

// LatestItems returns the most recently added items.
func (r *ItemRepository) LatestItems(ctx context.Context, libraryID string, limit int) ([]*Item, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	where := "WHERE is_available = 1"
	args := []any{}
	if libraryID != "" {
		where += " AND library_id = ?"
		args = append(args, libraryID)
	}
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(
			`SELECT id, library_id, parent_id, type, title, sort_title, year, path,
			        duration_ticks, container, added_at, is_available
			 FROM items %s ORDER BY added_at DESC LIMIT ?`, where,
		), args...)
	if err != nil {
		return nil, fmt.Errorf("latest items: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var items []*Item
	for rows.Next() {
		item := &Item{}
		var parentID, path, container sql.NullString
		if err := rows.Scan(&item.ID, &item.LibraryID, &parentID, &item.Type, &item.Title,
			&item.SortTitle, &item.Year, &path, &item.DurationTicks, &container,
			&item.AddedAt, &item.IsAvailable); err != nil {
			return nil, fmt.Errorf("scan latest item: %w", err)
		}
		item.ParentID = parentID.String
		item.Path = path.String
		item.Container = container.String
		items = append(items, item)
	}
	return items, rows.Err()
}

