package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
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
	Cursor    string // cursor for keyset pagination (item ID after which to fetch)
}

type ItemRepository struct {
	db *sql.DB // kept for List and LatestItems (dynamic WHERE/FTS/cursor)
	q  *sqlc.Queries
}

func NewItemRepository(database *sql.DB) *ItemRepository {
	return &ItemRepository{db: database, q: sqlc.New(database)}
}

func (r *ItemRepository) Create(ctx context.Context, item *Item) error {
	err := r.q.CreateItem(ctx, sqlc.CreateItemParams{
		ID:              item.ID,
		LibraryID:       item.LibraryID,
		ParentID:        nullableString(item.ParentID),
		Type:            item.Type,
		Title:           item.Title,
		SortTitle:       item.SortTitle,
		OriginalTitle:   nullableString(item.OriginalTitle),
		Year:            sql.NullInt64{Int64: int64(item.Year), Valid: true},
		Path:            nullableString(item.Path),
		Size:            sql.NullInt64{Int64: item.Size, Valid: true},
		DurationTicks:   sql.NullInt64{Int64: item.DurationTicks, Valid: true},
		Container:       nullableString(item.Container),
		Fingerprint:     nullableString(item.Fingerprint),
		SeasonNumber:    nullableIntPtr(item.SeasonNumber),
		EpisodeNumber:   nullableIntPtr(item.EpisodeNumber),
		CommunityRating: nullableFloat64Ptr(item.CommunityRating),
		ContentRating:   nullableString(item.ContentRating),
		PremiereDate:    nullableTimePtr(item.PremiereDate),
		AddedAt:         item.AddedAt,
		UpdatedAt:       item.UpdatedAt,
		IsAvailable:     item.IsAvailable,
	})
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
	row, err := r.q.GetItemByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("item %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get item %s: %w", id, err)
	}
	item := itemFromSqlcModel(row)
	return &item, nil
}

func (r *ItemRepository) GetByPath(ctx context.Context, path string) (*Item, error) {
	row, err := r.q.GetItemByPath(ctx, sql.NullString{String: path, Valid: path != ""})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("item path %q: %w", path, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get item by path %q: %w", path, err)
	}
	item := itemFromSqlcModel(row)
	return &item, nil
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
		where += " AND parent_id IS NULL"
	}
	if filter.Type != "" {
		where += " AND type = ?"
		args = append(args, filter.Type)
	}

	if filter.Query != "" {
		where += " AND rowid IN (SELECT rowid FROM items_fts WHERE items_fts MATCH ?)"
		args = append(args, filter.Query+"*")
	}

	var total int
	if filter.Cursor == "" {
		countSQL := "SELECT COUNT(*) FROM items " + where
		if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count items: %w", err)
		}
	}

	if filter.Cursor != "" {
		var cursorSort string
		var cursorID string
		err := r.db.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT %s, id FROM items WHERE id = ?`, filter.SortBy),
			filter.Cursor,
		).Scan(&cursorSort, &cursorID)
		if err == nil {
			op := ">"
			if filter.SortOrder == "desc" {
				op = "<"
			}
			where += fmt.Sprintf(" AND (%s %s ? OR (%s = ? AND id > ?))",
				filter.SortBy, op, filter.SortBy)
			args = append(args, cursorSort, cursorSort, cursorID)
		}
	}

	querySQL := fmt.Sprintf(
		`SELECT id, library_id, parent_id, type, title, sort_title, original_title,
		        year, path, size, duration_ticks, container, season_number, episode_number,
		        community_rating, added_at, updated_at, is_available
		 FROM items %s ORDER BY %s %s, id ASC LIMIT ? OFFSET ?`,
		where, filter.SortBy, filter.SortOrder,
	)
	offset := filter.Offset
	if filter.Cursor != "" {
		offset = 0
	}
	args = append(args, filter.Limit, offset)

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
	n, err := r.q.UpdateItem(ctx, sqlc.UpdateItemParams{
		Title:           item.Title,
		SortTitle:       item.SortTitle,
		OriginalTitle:   nullableString(item.OriginalTitle),
		Year:            sql.NullInt64{Int64: int64(item.Year), Valid: true},
		Size:            sql.NullInt64{Int64: item.Size, Valid: true},
		DurationTicks:   sql.NullInt64{Int64: item.DurationTicks, Valid: true},
		Container:       nullableString(item.Container),
		Fingerprint:     nullableString(item.Fingerprint),
		SeasonNumber:    nullableIntPtr(item.SeasonNumber),
		EpisodeNumber:   nullableIntPtr(item.EpisodeNumber),
		CommunityRating: nullableFloat64Ptr(item.CommunityRating),
		ContentRating:   nullableString(item.ContentRating),
		PremiereDate:    nullableTimePtr(item.PremiereDate),
		UpdatedAt:       item.UpdatedAt,
		IsAvailable:     item.IsAvailable,
		ID:              item.ID,
	})
	if err != nil {
		return fmt.Errorf("update item: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("item %s: %w", item.ID, domain.ErrNotFound)
	}
	return nil
}

func (r *ItemRepository) Delete(ctx context.Context, id string) error {
	n, err := r.q.DeleteItem(ctx, id)
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("item %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

func (r *ItemRepository) DeleteByLibrary(ctx context.Context, libraryID string) error {
	err := r.q.DeleteItemsByLibrary(ctx, libraryID)
	if err != nil {
		return fmt.Errorf("delete items by library: %w", err)
	}
	return nil
}

func (r *ItemRepository) CountByLibrary(ctx context.Context, libraryID string) (int, error) {
	cnt, err := r.q.CountItemsByLibrary(ctx, libraryID)
	if err != nil {
		return 0, fmt.Errorf("count items by library: %w", err)
	}
	return int(cnt), nil
}

func (r *ItemRepository) GetChildren(ctx context.Context, parentID string) ([]*Item, error) {
	rows, err := r.q.GetItemChildren(ctx, sql.NullString{String: parentID, Valid: parentID != ""})
	if err != nil {
		return nil, fmt.Errorf("get children: %w", err)
	}
	return itemsFromChildrenRows(rows), nil
}

// LatestItems returns the most recently added items.
// Uses raw SQL because of dynamic WHERE clauses.
func (r *ItemRepository) LatestItems(ctx context.Context, libraryID string, itemType string, limit int) ([]*Item, error) {
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
	if itemType != "" {
		where += " AND type = ?"
		args = append(args, itemType)
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

// ── row mapping helpers ─────────────────────────────────────────────────

func nullableFloat64Ptr(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

func itemFromSqlcModel(r sqlc.Item) Item {
	item := Item{
		ID:            r.ID,
		LibraryID:     r.LibraryID,
		ParentID:      r.ParentID.String,
		Type:          r.Type,
		Title:         r.Title,
		SortTitle:     r.SortTitle,
		OriginalTitle: r.OriginalTitle.String,
		Year:          int(r.Year.Int64),
		Path:          r.Path.String,
		Size:          r.Size.Int64,
		DurationTicks: r.DurationTicks.Int64,
		Container:     r.Container.String,
		Fingerprint:   r.Fingerprint.String,
		ContentRating: r.ContentRating.String,
		AddedAt:       r.AddedAt,
		UpdatedAt:     r.UpdatedAt,
		IsAvailable:   r.IsAvailable,
	}
	if r.SeasonNumber.Valid {
		v := int(r.SeasonNumber.Int64)
		item.SeasonNumber = &v
	}
	if r.EpisodeNumber.Valid {
		v := int(r.EpisodeNumber.Int64)
		item.EpisodeNumber = &v
	}
	if r.CommunityRating.Valid {
		item.CommunityRating = &r.CommunityRating.Float64
	}
	if r.PremiereDate.Valid {
		item.PremiereDate = &r.PremiereDate.Time
	}
	return item
}

func itemFromChildrenRow(r sqlc.GetItemChildrenRow) Item {
	item := Item{
		ID:            r.ID,
		LibraryID:     r.LibraryID,
		ParentID:      r.ParentID.String,
		Type:          r.Type,
		Title:         r.Title,
		SortTitle:     r.SortTitle,
		OriginalTitle: r.OriginalTitle.String,
		Year:          int(r.Year.Int64),
		Path:          r.Path.String,
		Size:          r.Size.Int64,
		DurationTicks: r.DurationTicks.Int64,
		Container:     r.Container.String,
		AddedAt:       r.AddedAt,
		UpdatedAt:     r.UpdatedAt,
		IsAvailable:   r.IsAvailable,
	}
	if r.SeasonNumber.Valid {
		v := int(r.SeasonNumber.Int64)
		item.SeasonNumber = &v
	}
	if r.EpisodeNumber.Valid {
		v := int(r.EpisodeNumber.Int64)
		item.EpisodeNumber = &v
	}
	if r.CommunityRating.Valid {
		item.CommunityRating = &r.CommunityRating.Float64
	}
	return item
}

func itemsFromChildrenRows(rows []sqlc.GetItemChildrenRow) []*Item {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*Item, len(rows))
	for i, row := range rows {
		item := itemFromChildrenRow(row)
		out[i] = &item
	}
	return out
}
