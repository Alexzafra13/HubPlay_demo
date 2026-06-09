package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/domain"
)

// ItemRepository — dual-dialect (Pattern A + Pattern B). The sqlc-backed
// methods (Create / GetByID / GetByPath / Update / Delete /
// DeleteByLibrary / CountByLibrary / GetChildren) branch per-call on
// `useSQLite()`. The four dynamic-SQL methods (List, ChildCountsByParents,
// LatestItems, LatestSeriesByActivity) build their query strings on
// the fly and call `r.db` directly with `rewritePlaceholders` to handle
// `?` → `$N` for Postgres.
//
// One last cross-dialect gotcha worth flagging: SQLite stores BOOLEAN
// as INTEGER so `WHERE is_available = 1` works there but Postgres needs
// a real boolean predicate. We sidestep with `WHERE is_available` —
// truthy in both, no rewrite needed.
type ItemRepository struct {
	db *sql.DB
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewItemRepository(driver string, database *sql.DB) *ItemRepository {
	r := &ItemRepository{db: database}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *ItemRepository) useSQLite() bool { return r.sq != nil }

// driver returns the driver string this repo was built with — used
// by raw-SQL methods that need to call rewritePlaceholders.
func (r *ItemRepository) driver() string {
	if r.useSQLite() {
		return DriverSQLite
	}
	return DriverPostgres
}

func (r *ItemRepository) Create(ctx context.Context, item *librarymodel.Item) error {
	if r.useSQLite() {
		err := r.sq.CreateItem(ctx, sqlc.CreateItemParams{
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
	err := r.pq.CreateItem(ctx, sqlc_pg.CreateItemParams{
		ID:              item.ID,
		LibraryID:       item.LibraryID,
		ParentID:        nullableString(item.ParentID),
		Type:            item.Type,
		Title:           item.Title,
		SortTitle:       item.SortTitle,
		OriginalTitle:   nullableString(item.OriginalTitle),
		Year:            sql.NullInt32{Int32: int32(item.Year), Valid: true},
		Path:            nullableString(item.Path),
		Size:            sql.NullInt64{Int64: item.Size, Valid: true},
		DurationTicks:   sql.NullInt64{Int64: item.DurationTicks, Valid: true},
		Container:       nullableString(item.Container),
		Fingerprint:     nullableString(item.Fingerprint),
		SeasonNumber:    nullableIntPtrInt32(item.SeasonNumber),
		EpisodeNumber:   nullableIntPtrInt32(item.EpisodeNumber),
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

func (r *ItemRepository) GetByID(ctx context.Context, id string) (*librarymodel.Item, error) {
	if r.useSQLite() {
		row, err := r.sq.GetItemByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("item %s: %w", id, domain.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get item %s: %w", id, err)
		}
		item := itemFromSqliteModel(row)
		return &item, nil
	}
	row, err := r.pq.GetItemByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("item %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get item %s: %w", id, err)
	}
	item := itemFromPgGetByIDRow(row)
	return &item, nil
}

func (r *ItemRepository) GetByPath(ctx context.Context, path string) (*librarymodel.Item, error) {
	if r.useSQLite() {
		row, err := r.sq.GetItemByPath(ctx, sql.NullString{String: path, Valid: path != ""})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("item path %q: %w", path, domain.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get item by path %q: %w", path, err)
		}
		item := itemFromSqliteModel(row)
		return &item, nil
	}
	row, err := r.pq.GetItemByPath(ctx, sql.NullString{String: path, Valid: path != ""})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("item path %q: %w", path, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get item by path %q: %w", path, err)
	}
	item := itemFromPgByPathRow(row)
	return &item, nil
}

// LibrarySizeRow es el agregado de un solo SELECT — peso total
// (bytes) + numero de ficheros para una library_id. Util para el
// admin Dashboard / Libraries page sin tener que walkear el
// filesystem.

// SumItemSizesByLibrary devuelve un map library_id -> { bytes, files }
// con el peso ya agregado en la DB. Devuelve solo bibliotecas que
// tienen al menos un fichero con size>0 — el caller decide que mostrar
// para una biblioteca vacia (probablemente "0 B" / "—").
//
// Una unica query indexed por idx_items_library; coste ~ms incluso
// con cientos de miles de items. Cero filesystem I/O.
func (r *ItemRepository) SumItemSizesByLibrary(ctx context.Context) (map[string]librarymodel.LibrarySizeRow, error) {
	out := make(map[string]librarymodel.LibrarySizeRow)
	if r.useSQLite() {
		rows, err := r.sq.SumItemSizesByLibrary(ctx)
		if err != nil {
			return nil, fmt.Errorf("sum item sizes by library: %w", err)
		}
		for _, row := range rows {
			out[row.LibraryID] = librarymodel.LibrarySizeRow{
				LibraryID:  row.LibraryID,
				TotalBytes: row.TotalBytes,
				FileCount:  row.FileCount,
			}
		}
		return out, nil
	}
	rows, err := r.pq.SumItemSizesByLibrary(ctx)
	if err != nil {
		return nil, fmt.Errorf("sum item sizes by library: %w", err)
	}
	for _, row := range rows {
		out[row.LibraryID] = librarymodel.LibrarySizeRow{
			LibraryID:  row.LibraryID,
			TotalBytes: row.TotalBytes,
			FileCount:  row.FileCount,
		}
	}
	return out, nil
}

func (r *ItemRepository) List(ctx context.Context, filter librarymodel.ItemFilter) ([]*librarymodel.Item, int, error) {
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

	conds := []string{"1=1"}
	args := []any{}

	if filter.LibraryID != "" {
		conds = append(conds, "library_id = ?")
		args = append(args, filter.LibraryID)
	}
	// Per-caller library allow-list: restricts cross-library list /
	// search to the caller's granted libraries. Empty = no restriction.
	if len(filter.LibraryIDs) > 0 {
		conds = append(conds, "library_id IN ("+sqlPlaceholders(len(filter.LibraryIDs))+")")
		for _, v := range filter.LibraryIDs {
			args = append(args, v)
		}
	}
	if filter.ParentID != "" {
		conds = append(conds, "parent_id = ?")
		args = append(args, filter.ParentID)
	} else if filter.Type == "" {
		conds = append(conds, "parent_id IS NULL")
	}
	if filter.Type != "" {
		conds = append(conds, "type = ?")
		args = append(args, filter.Type)
	}

	// Búsqueda full-text: SQLite usa FTS5, Postgres usa tsvector.
	if filter.Query != "" {
		if r.useSQLite() {
			conds = append(conds, "rowid IN (SELECT rowid FROM items_fts WHERE items_fts MATCH ?)")
			args = append(args, filter.Query+"*")
		} else {
			conds = append(conds, "search_vector @@ to_tsquery('simple', ?)")
			args = append(args, toTSQueryPrefix(filter.Query))
		}
	}

	// Género normalizado via item_value_map.
	if filter.Genre != "" {
		conds = append(conds, "id IN (SELECT item_id FROM item_value_map WHERE value_id = ?)")
		args = append(args, GenreValueID(filter.Genre))
	}
	// Rango de año — items sin year (NULL) no se ocultan.
	if filter.YearFrom > 0 {
		conds = append(conds, "year IS NOT NULL AND year >= ?")
		args = append(args, filter.YearFrom)
	}
	if filter.YearTo > 0 {
		conds = append(conds, "year IS NOT NULL AND year <= ?")
		args = append(args, filter.YearTo)
	}
	if filter.MinRating > 0 {
		conds = append(conds, "community_rating IS NOT NULL AND community_rating >= ?")
		args = append(args, filter.MinRating)
	}
	if len(filter.AllowedContentRatings) > 0 {
		// IN (?,?,?) sobre la allow-list; items sin content_rating
		// se excluyen cuando el caller tiene un cap activo.
		conds = append(conds, "content_rating IS NOT NULL AND content_rating IN ("+sqlPlaceholders(len(filter.AllowedContentRatings))+")")
		for _, v := range filter.AllowedContentRatings {
			args = append(args, v)
		}
	}

	where := "WHERE " + strings.Join(conds, " AND ")
	driver := r.driver()

	var total int
	if filter.Cursor == "" {
		countSQL := rewritePlaceholders(driver, "SELECT COUNT(*) FROM items "+where)
		if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count items: %w", err)
		}
	}

	if filter.Cursor != "" {
		var cursorSort any
		var cursorID string
		cursorSQL := rewritePlaceholders(driver,
			fmt.Sprintf(`SELECT %s, id FROM items WHERE id = ?`, filter.SortBy))
		err := r.db.QueryRowContext(ctx, cursorSQL, filter.Cursor).Scan(&cursorSort, &cursorID)
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

	querySQL := rewritePlaceholders(driver, fmt.Sprintf(
		`SELECT id, library_id, parent_id, type, title, sort_title, original_title,
		        year, path, size, duration_ticks, container, season_number, episode_number,
		        community_rating, added_at, updated_at, is_available
		 FROM items %s ORDER BY %s %s, id ASC LIMIT ? OFFSET ?`,
		where, filter.SortBy, filter.SortOrder,
	))
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

	var items []*librarymodel.Item
	for rows.Next() {
		item := &librarymodel.Item{}
		var n itemNullables
		if err := rows.Scan(listScanDests(item, &n)...); err != nil {
			return nil, 0, fmt.Errorf("scan item: %w", err)
		}
		n.applyList(item)
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *ItemRepository) Update(ctx context.Context, item *librarymodel.Item) error {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.UpdateItem(ctx, sqlc.UpdateItemParams{
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
	} else {
		n, err = r.pq.UpdateItem(ctx, sqlc_pg.UpdateItemParams{
			Title:           item.Title,
			SortTitle:       item.SortTitle,
			OriginalTitle:   nullableString(item.OriginalTitle),
			Year:            sql.NullInt32{Int32: int32(item.Year), Valid: true},
			Size:            sql.NullInt64{Int64: item.Size, Valid: true},
			DurationTicks:   sql.NullInt64{Int64: item.DurationTicks, Valid: true},
			Container:       nullableString(item.Container),
			Fingerprint:     nullableString(item.Fingerprint),
			SeasonNumber:    nullableIntPtrInt32(item.SeasonNumber),
			EpisodeNumber:   nullableIntPtrInt32(item.EpisodeNumber),
			CommunityRating: nullableFloat64Ptr(item.CommunityRating),
			ContentRating:   nullableString(item.ContentRating),
			PremiereDate:    nullableTimePtr(item.PremiereDate),
			UpdatedAt:       item.UpdatedAt,
			IsAvailable:     item.IsAvailable,
			ID:              item.ID,
		})
	}
	if err != nil {
		return fmt.Errorf("update item: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("item %s: %w", item.ID, domain.ErrNotFound)
	}
	return nil
}

func (r *ItemRepository) Delete(ctx context.Context, id string) error {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.DeleteItem(ctx, id)
	} else {
		n, err = r.pq.DeleteItem(ctx, id)
	}
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("item %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

func (r *ItemRepository) DeleteByLibrary(ctx context.Context, libraryID string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteItemsByLibrary(ctx, libraryID)
	} else {
		err = r.pq.DeleteItemsByLibrary(ctx, libraryID)
	}
	if err != nil {
		return fmt.Errorf("delete items by library: %w", err)
	}
	return nil
}

func (r *ItemRepository) CountByLibrary(ctx context.Context, libraryID string) (int, error) {
	var (
		cnt int64
		err error
	)
	if r.useSQLite() {
		cnt, err = r.sq.CountItemsByLibrary(ctx, libraryID)
	} else {
		cnt, err = r.pq.CountItemsByLibrary(ctx, libraryID)
	}
	if err != nil {
		return 0, fmt.Errorf("count items by library: %w", err)
	}
	return int(cnt), nil
}

// ChildCountsByParents returns a `parent_id → direct-child count` map
// in one round-trip. Used by the Children handler to dedupe duplicate
// season rows (same series + season_number) by preferring the one
// that actually has episodes attached. Raw SQL because sqlc has no
// shorthand for `IN (?, ?, ?)` with a dynamic length and we already
// keep `r.db` around for List/LatestItems.
//
// Returns an empty map for an empty input slice — caller doesn't need
// a length guard.
func (r *ItemRepository) ChildCountsByParents(ctx context.Context, parentIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(parentIDs))
	if len(parentIDs) == 0 {
		return out, nil
	}
	placeholders := strings.Repeat("?,", len(parentIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(parentIDs))
	for i, id := range parentIDs {
		args[i] = id
	}
	q := rewritePlaceholders(r.driver(),
		"SELECT parent_id, COUNT(*) FROM items WHERE parent_id IN ("+placeholders+") GROUP BY parent_id")
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("child counts: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var pid sql.NullString
		var n int
		if err := rows.Scan(&pid, &n); err != nil {
			return nil, fmt.Errorf("scan child count: %w", err)
		}
		if pid.Valid {
			out[pid.String] = n
		}
	}
	return out, rows.Err()
}

func (r *ItemRepository) GetChildren(ctx context.Context, parentID string) ([]*librarymodel.Item, error) {
	if r.useSQLite() {
		rows, err := r.sq.GetItemChildren(ctx, sql.NullString{String: parentID, Valid: parentID != ""})
		if err != nil {
			return nil, fmt.Errorf("get children: %w", err)
		}
		return itemsFromSqliteChildrenRows(rows), nil
	}
	rows, err := r.pq.GetItemChildren(ctx, sql.NullString{String: parentID, Valid: parentID != ""})
	if err != nil {
		return nil, fmt.Errorf("get children: %w", err)
	}
	return itemsFromPgChildrenRows(rows), nil
}

// LatestItems returns the most recently added items.
// Uses raw SQL because of dynamic WHERE clauses.
func (r *ItemRepository) LatestItems(ctx context.Context, libraryID string, itemType string, limit int, allowedRatings ...string) ([]*librarymodel.Item, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	// `is_available` (sin `= 1`) — truthy en SQLite (INTEGER) y Postgres (BOOLEAN).
	conds := []string{"is_available"}
	args := []any{}
	if libraryID != "" {
		conds = append(conds, "library_id = ?")
		args = append(args, libraryID)
	}
	if itemType != "" {
		conds = append(conds, "type = ?")
		args = append(args, itemType)
	}
	if len(allowedRatings) > 0 {
		// Mismo gate content_rating que List(); los rails "Reciente
		// en X" respetan el cap de contenido del perfil.
		conds = append(conds, "content_rating IS NOT NULL AND content_rating IN ("+sqlPlaceholders(len(allowedRatings))+")")
		for _, v := range allowedRatings {
			args = append(args, v)
		}
	}
	where := "WHERE " + strings.Join(conds, " AND ")
	args = append(args, limit)

	query := rewritePlaceholders(r.driver(), fmt.Sprintf(
		`SELECT id, library_id, parent_id, type, title, sort_title, year, path,
		        duration_ticks, container, added_at, is_available
		 FROM items %s ORDER BY added_at DESC LIMIT ?`, where,
	))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("latest items: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	pg := !r.useSQLite()
	var items []*librarymodel.Item
	for rows.Next() {
		item := &librarymodel.Item{}
		var parentID, path, container sql.NullString
		// Year column is INTEGER in both schemas — sqlc maps it to
		// NullInt64 on SQLite, NullInt32 on Postgres. We don't go
		// through sqlc here, so scan into the dialect's native size
		// and project to int.
		if pg {
			var year sql.NullInt32
			if err := rows.Scan(&item.ID, &item.LibraryID, &parentID, &item.Type, &item.Title,
				&item.SortTitle, &year, &path, &item.DurationTicks, &container,
				&item.AddedAt, &item.IsAvailable); err != nil {
				return nil, fmt.Errorf("scan latest item: %w", err)
			}
			if year.Valid {
				item.Year = int(year.Int32)
			}
		} else {
			var year sql.NullInt64
			if err := rows.Scan(&item.ID, &item.LibraryID, &parentID, &item.Type, &item.Title,
				&item.SortTitle, &year, &path, &item.DurationTicks, &container,
				&item.AddedAt, &item.IsAvailable); err != nil {
				return nil, fmt.Errorf("scan latest item: %w", err)
			}
			if year.Valid {
				item.Year = int(year.Int64)
			}
		}
		item.ParentID = parentID.String
		item.Path = path.String
		item.Container = container.String
		items = append(items, item)
	}
	return items, rows.Err()
}

// LatestSeriesByActivity is the curated rail used by the home page's
// "Reciente en <library>" tier on shows libraries. It returns series
// rows (no episodes / seasons) ordered by the most recent activity
// across the whole show subtree, alongside the count of episodes
// added in the trailing 14 days so the card can render a "+N nuevos
// episodios" hint when applicable.
//
// The query is structured as:
//
//	WITH activity AS (
//	    SELECT s.id AS series_id,
//	           MAX(s.added_at, MAX(e.added_at)) AS latest_at,
//	           COUNT(e.id) FILTER (added_at >= cutoff) AS new_count
//	    FROM series s LEFT JOIN episodes e via parent->season->series
//	)
//
// SQLite doesn't support FILTER; the equivalent is a CASE/SUM trick.
// The double-deep parent climb (season → series) is encoded with the
// item table's own parent_id column rather than a recursive CTE,
// since shows-only have a fixed two-level depth.
//
// Limit caps at 50 to match `LatestItems`.
func (r *ItemRepository) LatestSeriesByActivity(ctx context.Context, libraryID string, limit int) ([]*librarymodel.LatestSeriesActivity, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	cutoff := timeNow().UTC().Add(-14 * 24 * time.Hour)

	// libraryID == "" significa "todas las bibliotecas" - usado por
	// el strip "Recientemente añadido" del dashboard admin, que
	// agrega series con actividad reciente cross-library. El filtro
	// por library se incluye solo cuando viene un id.
	libraryFilter := ""
	args := []any{cutoff}
	if libraryID != "" {
		libraryFilter = " AND s.library_id = ?"
		args = append(args, libraryID)
	}
	args = append(args, limit)

	query := rewritePlaceholders(r.driver(), fmt.Sprintf(`
		WITH activity AS (
			SELECT
				s.id AS series_id,
				s.added_at AS series_added_at,
				MAX(COALESCE(e.added_at, s.added_at)) AS latest_at,
				SUM(CASE WHEN e.added_at IS NOT NULL AND e.added_at >= ? THEN 1 ELSE 0 END) AS new_count
			FROM items s
			LEFT JOIN items season ON season.parent_id = s.id AND season.type = 'season'
			LEFT JOIN items e ON e.parent_id = season.id
			                 AND e.type = 'episode'
			                 AND e.is_available
			WHERE s.type = 'series'
			  AND s.is_available%s
			GROUP BY s.id
		)
		SELECT s.id, s.library_id, s.parent_id, s.type, s.title, s.sort_title, s.year, s.path,
		       s.duration_ticks, s.container, s.added_at, s.is_available,
		       a.latest_at, a.new_count
		FROM activity a
		JOIN items s ON s.id = a.series_id
		ORDER BY a.latest_at DESC, s.added_at DESC
		LIMIT ?`, libraryFilter))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("latest series by activity: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	pg := !r.useSQLite()
	var out []*librarymodel.LatestSeriesActivity
	for rows.Next() {
		row := &librarymodel.LatestSeriesActivity{}
		var parentID, path, container sql.NullString
		var latestAtRaw any
		var newCount sql.NullInt64
		// Same Year-column dialect split as LatestItems.
		if pg {
			var year sql.NullInt32
			if err := rows.Scan(
				&row.ID, &row.LibraryID, &parentID, &row.Type, &row.Title, &row.SortTitle,
				&year, &path, &row.DurationTicks, &container, &row.AddedAt, &row.IsAvailable,
				&latestAtRaw, &newCount,
			); err != nil {
				return nil, fmt.Errorf("scan latest series activity: %w", err)
			}
			if year.Valid {
				row.Year = int(year.Int32)
			}
		} else {
			var year sql.NullInt64
			if err := rows.Scan(
				&row.ID, &row.LibraryID, &parentID, &row.Type, &row.Title, &row.SortTitle,
				&year, &path, &row.DurationTicks, &container, &row.AddedAt, &row.IsAvailable,
				&latestAtRaw, &newCount,
			); err != nil {
				return nil, fmt.Errorf("scan latest series activity: %w", err)
			}
			if year.Valid {
				row.Year = int(year.Int64)
			}
		}
		row.ParentID = parentID.String
		row.Path = path.String
		row.Container = container.String
		// The MAX() over a heterogeneous column comes back as the
		// driver's any. coerceSQLiteTime handles the prod-legacy
		// "+0200 CEST m=+..." monotonic-clock shape too, same path
		// the trending rail already trusts. Pgx returns time.Time
		// directly — coerceSQLiteTime's `time.Time` case path
		// passes that straight through.
		t, err := coerceSQLiteTime(latestAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse latest_activity_at: %w", err)
		}
		row.LatestActivityAt = t
		row.NewEpisodesCount = int(newCount.Int64)
		out = append(out, row)
	}
	return out, rows.Err()
}

// ── row mapping helpers ─────────────────────────────────────────────────

func nullableFloat64Ptr(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

// nullableIntPtrInt32 is the postgres counterpart to nullableIntPtr —
// season_number / episode_number are INTEGER in postgres, sqlc maps
// that to NullInt32.
func nullableIntPtrInt32(p *int) sql.NullInt32 {
	if p == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(*p), Valid: true}
}

// itemFromSqliteModel maps a sqlc.Item (SQLite, INTEGER → NullInt64)
// into the domain librarymodel.Item. The pg counterpart (itemFromPgGetByIDRow)
// mirrors this with NullInt32 for the int-sized columns.
func itemFromSqliteModel(r sqlc.Item) librarymodel.Item {
	item := librarymodel.Item{
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

// itemFromPgGetByIDRow / itemFromPgByPathRow / itemFromPgChildrenRows —
// pg-side counterparts. INTEGER columns come back as NullInt32, BIGINT
// columns as NullInt64. Boolean is_available is a real BOOLEAN.
func itemFromPgGetByIDRow(r sqlc_pg.GetItemByIDRow) librarymodel.Item {
	item := librarymodel.Item{
		ID:            r.ID,
		LibraryID:     r.LibraryID,
		ParentID:      r.ParentID.String,
		Type:          r.Type,
		Title:         r.Title,
		SortTitle:     r.SortTitle,
		OriginalTitle: r.OriginalTitle.String,
		Year:          int(r.Year.Int32),
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
		v := int(r.SeasonNumber.Int32)
		item.SeasonNumber = &v
	}
	if r.EpisodeNumber.Valid {
		v := int(r.EpisodeNumber.Int32)
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

func itemFromPgByPathRow(r sqlc_pg.GetItemByPathRow) librarymodel.Item {
	// Structural cast — both rows share the same column projection.
	return itemFromPgGetByIDRow(sqlc_pg.GetItemByIDRow(r))
}

func itemFromSqliteChildRow(r sqlc.GetItemChildrenRow) librarymodel.Item {
	item := librarymodel.Item{
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

func itemFromPgChildRow(r sqlc_pg.GetItemChildrenRow) librarymodel.Item {
	item := librarymodel.Item{
		ID:            r.ID,
		LibraryID:     r.LibraryID,
		ParentID:      r.ParentID.String,
		Type:          r.Type,
		Title:         r.Title,
		SortTitle:     r.SortTitle,
		OriginalTitle: r.OriginalTitle.String,
		Year:          int(r.Year.Int32),
		Path:          r.Path.String,
		Size:          r.Size.Int64,
		DurationTicks: r.DurationTicks.Int64,
		Container:     r.Container.String,
		AddedAt:       r.AddedAt,
		UpdatedAt:     r.UpdatedAt,
		IsAvailable:   r.IsAvailable,
	}
	if r.SeasonNumber.Valid {
		v := int(r.SeasonNumber.Int32)
		item.SeasonNumber = &v
	}
	if r.EpisodeNumber.Valid {
		v := int(r.EpisodeNumber.Int32)
		item.EpisodeNumber = &v
	}
	if r.CommunityRating.Valid {
		item.CommunityRating = &r.CommunityRating.Float64
	}
	return item
}

func itemsFromSqliteChildrenRows(rows []sqlc.GetItemChildrenRow) []*librarymodel.Item {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*librarymodel.Item, len(rows))
	for i, row := range rows {
		item := itemFromSqliteChildRow(row)
		out[i] = &item
	}
	return out
}

func itemsFromPgChildrenRows(rows []sqlc_pg.GetItemChildrenRow) []*librarymodel.Item {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*librarymodel.Item, len(rows))
	for i, row := range rows {
		item := itemFromPgChildRow(row)
		out[i] = &item
	}
	return out
}

// toTSQueryPrefix turns a free-text search into a Postgres tsquery
// suitable for `to_tsquery('simple', ?)`. Splits on whitespace, strips
// any character `to_tsquery`'s parser would reject (`& | ! ( ) : * <`
// plus a few more), joins surviving tokens with `&`, and tags the
// final token with `:*` so search-as-you-type matches prefix.
//
// We pre-sanitise rather than trust the parser because raw user input
// like "harry+potter (2001)" would otherwise raise a syntax error. An
// empty / fully-stripped query becomes `""` which `to_tsquery` accepts
// and matches nothing — the right semantic for "the user typed only
// punctuation".
func toTSQueryPrefix(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	parts := strings.Fields(q)
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		clean := strings.Map(func(r rune) rune {
			switch r {
			case '&', '|', '!', '(', ')', ':', '*', '<', '>', '\'', '"', '\\', '/', '%', '?', '$', '@', '#':
				return -1
			}
			return r
		}, p)
		if clean != "" {
			tokens = append(tokens, clean)
		}
	}
	if len(tokens) == 0 {
		return ""
	}
	tokens[len(tokens)-1] += ":*"
	return strings.Join(tokens, " & ")
}
