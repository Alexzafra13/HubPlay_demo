package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/domain"
)

// StudioRepository — Pattern A for the five sqlc-backed methods plus
// two raw-SQL holdouts (ListItemsForStudio / SetItemStudio) that go
// through `rewritePlaceholders` at construction time. The raw holdout
// exists because sqlc 1.31.1 truncates the trailing identifier of the
// final query in a file (ORDER BY ASC ends up as ORDER BY A).
//
// TMDb id widens between dialects: SQLite serialises INTEGER as int64,
// Postgres declares the column as INTEGER (32-bit), so the sqlc
// surface is `sql.NullInt64` / `sql.NullInt32` respectively. The
// domain stays on `*int64`; the conversion happens at the adapter.
type StudioRepository struct {
	db *sql.DB
	sq *sqlc.Queries
	pq *sqlc_pg.Queries

	listItemsSQL    string
	setItemStudioSQL string
}

func NewStudioRepository(driver string, database *sql.DB) *StudioRepository {
	r := &StudioRepository{
		db: database,
		// BOOLEAN predicates are written without `= 1` so the query
		// stays portable: SQLite reads 0/1 as truthy, Postgres rejects
		// the implicit int → bool cast. Same pattern applied across
		// the dual-dialect refactor.
		listItemsSQL: rewritePlaceholders(driver, `
			SELECT
			    i.id,
			    i.type,
			    i.title,
			    COALESCE(i.year, 0) AS year,
			    COALESCE(img.id, '') AS primary_image_id
			FROM metadata m
			JOIN items i ON i.id = m.item_id
			LEFT JOIN images img
			    ON img.item_id = i.id AND img.type = 'primary' AND img.is_primary
			WHERE m.studio_id = ?
			  AND i.is_available
			  AND i.type IN ('movie', 'series')
			ORDER BY COALESCE(i.year, 0) DESC, i.title ASC`),
		setItemStudioSQL: rewritePlaceholders(driver, `UPDATE metadata SET studio_id = NULLIF(?, '') WHERE item_id = ?`),
	}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *StudioRepository) useSQLite() bool { return r.sq != nil }

// Slugify turns a free-form studio name into the URL-safe slug used
// as the row key on /studios/<slug>. Mirrors the SQL recipe in
// 032_studios.sql so the migration's backfill and the scanner agree
// on the same slug for the same name.
func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}
	// Normalise the same separators the migration handles, in the same
	// order, so a row inserted by Go matches the row inserted by SQL.
	s = strings.ReplaceAll(s, "&", "and")
	s = strings.ReplaceAll(s, "'", "")
	// Collapse anything non-alphanumeric to '-' (catches space, period,
	// comma, slash, parens, etc. in one pass — strictly stricter than
	// the migration but the migration's chain only handles the common
	// separators, so this Go pass produces the same or more-canonical
	// result for any name the scanner sees).
	s = nonAlnumRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	// Collapse runs of dashes left by adjacent separators.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}

var nonAlnumRE = regexp.MustCompile(`[^a-z0-9]+`)

// EnsureStudio upserts a studio row keyed by tmdb_id (when present)
// or slug (legacy / non-TMDb providers), and returns the id of the
// resulting row. The scanner calls this once per item-with-studio.
//
// Empty name → ("", nil): caller treats absence as "no studio for
// this item, leave metadata.studio_id NULL".
func (r *StudioRepository) EnsureStudio(ctx context.Context, name, logoURL string, tmdbID *int64) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	slug := Slugify(name)
	if slug == "" {
		return "", nil
	}
	id := "studio:" + slug
	if tmdbID != nil && *tmdbID > 0 {
		// Upsert by tmdb_id — the canonical dedupe key when the scanner
		// has provider data. The query refreshes logo_url only when
		// non-empty so a re-scan that comes back without a logo (TMDb
		// transient gap) doesn't blank an existing one.
		if r.useSQLite() {
			if err := r.sq.UpsertStudio(ctx, sqlc.UpsertStudioParams{
				ID:      id,
				TmdbID:  sql.NullInt64{Int64: *tmdbID, Valid: true},
				Name:    name,
				Slug:    slug,
				LogoUrl: logoURL,
			}); err != nil {
				return "", fmt.Errorf("upsert studio (tmdb=%d): %w", *tmdbID, err)
			}
			// The conflict resolution may have kept the existing row;
			// resolve the actual id by tmdb_id rather than trusting
			// the inserted id (different slug → different id, but the
			// tmdb_id wins).
			row, gerr := r.sq.GetStudioByTMDBID(ctx, sql.NullInt64{Int64: *tmdbID, Valid: true})
			if gerr == nil {
				return row.ID, nil
			}
			return id, nil
		}
		if err := r.pq.UpsertStudio(ctx, sqlc_pg.UpsertStudioParams{
			ID:      id,
			TmdbID:  sql.NullInt32{Int32: int32(*tmdbID), Valid: true},
			Name:    name,
			Slug:    slug,
			LogoUrl: logoURL,
		}); err != nil {
			return "", fmt.Errorf("upsert studio (tmdb=%d): %w", *tmdbID, err)
		}
		row, gerr := r.pq.GetStudioByTMDBID(ctx, sql.NullInt32{Int32: int32(*tmdbID), Valid: true})
		if gerr == nil {
			return row.ID, nil
		}
		return id, nil
	}
	// No tmdb_id: dedupe by slug (legacy backfill path).
	if r.useSQLite() {
		if err := r.sq.UpsertStudioBySlug(ctx, sqlc.UpsertStudioBySlugParams{
			ID:      id,
			TmdbID:  sql.NullInt64{Valid: false},
			Name:    name,
			Slug:    slug,
			LogoUrl: logoURL,
		}); err != nil {
			return "", fmt.Errorf("upsert studio (slug=%s): %w", slug, err)
		}
		return id, nil
	}
	if err := r.pq.UpsertStudioBySlug(ctx, sqlc_pg.UpsertStudioBySlugParams{
		ID:      id,
		TmdbID:  sql.NullInt32{Valid: false},
		Name:    name,
		Slug:    slug,
		LogoUrl: logoURL,
	}); err != nil {
		return "", fmt.Errorf("upsert studio (slug=%s): %w", slug, err)
	}
	return id, nil
}

// GetBySlug fetches the canonical row for /studios/<slug> rendering.
// Returns (nil, nil) when no studio matches — handler converts to 404.
func (r *StudioRepository) GetBySlug(ctx context.Context, slug string) (*librarymodel.Studio, error) {
	if r.useSQLite() {
		row, err := r.sq.GetStudioBySlug(ctx, slug)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get studio by slug: %w", err)
		}
		s := studioFromSqliteRow(row)
		return &s, nil
	}
	row, err := r.pq.GetStudioBySlug(ctx, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get studio by slug: %w", err)
	}
	s := studioFromPgRow(row)
	return &s, nil
}

// List returns every studio that has at least one item linked to it,
// sorted by item count desc. Drives the /studios browse page.
func (r *StudioRepository) List(ctx context.Context) ([]*librarymodel.StudioListEntry, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListStudios(ctx)
		if err != nil {
			return nil, fmt.Errorf("list studios: %w", err)
		}
		out := make([]*librarymodel.StudioListEntry, 0, len(rows))
		for _, row := range rows {
			out = append(out, &librarymodel.StudioListEntry{
				ID:        row.ID,
				Name:      row.Name,
				Slug:      row.Slug,
				LogoURL:   row.LogoUrl,
				ItemCount: row.ItemCount,
			})
		}
		return out, nil
	}
	rows, err := r.pq.ListStudios(ctx)
	if err != nil {
		return nil, fmt.Errorf("list studios: %w", err)
	}
	out := make([]*librarymodel.StudioListEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, &librarymodel.StudioListEntry{
			ID:        row.ID,
			Name:      row.Name,
			Slug:      row.Slug,
			LogoURL:   row.LogoUrl,
			ItemCount: row.ItemCount,
		})
	}
	return out, nil
}

// ListItemsForStudio returns the catalogue's items linked to this
// studio id, sorted year-desc. Raw SQL because the trailing ORDER BY
// hits the sqlc v1.31.1 truncation bug we work around in two other
// places already.
func (r *StudioRepository) ListItemsForStudio(ctx context.Context, studioID string) ([]*librarymodel.StudioItem, error) {
	rows, err := r.db.QueryContext(ctx, r.listItemsSQL, studioID)
	if err != nil {
		return nil, fmt.Errorf("list items for studio %s: %w", studioID, err)
	}
	defer rows.Close() //nolint:errcheck
	out := make([]*librarymodel.StudioItem, 0)
	for rows.Next() {
		var it librarymodel.StudioItem
		if err := rows.Scan(&it.ID, &it.Type, &it.Title, &it.Year, &it.PrimaryImageID); err != nil {
			return nil, fmt.Errorf("scan studio item: %w", err)
		}
		out = append(out, &it)
	}
	return out, rows.Err()
}

// SetItemStudio links an item's metadata row to a studio id. Empty
// studioID clears the link (used when a metadata refresh now returns
// no studio match for an item that previously had one).
func (r *StudioRepository) SetItemStudio(ctx context.Context, itemID, studioID string) error {
	if _, err := r.db.ExecContext(ctx, r.setItemStudioSQL, studioID, itemID); err != nil {
		return fmt.Errorf("set item studio: %w", err)
	}
	return nil
}

func studioFromSqliteRow(row sqlc.Studio) librarymodel.Studio {
	s := librarymodel.Studio{
		ID:      row.ID,
		Name:    row.Name,
		Slug:    row.Slug,
		LogoURL: row.LogoUrl,
	}
	if row.TmdbID.Valid {
		v := row.TmdbID.Int64
		s.TMDBID = &v
	}
	return s
}

func studioFromPgRow(row sqlc_pg.Studio) librarymodel.Studio {
	s := librarymodel.Studio{
		ID:      row.ID,
		Name:    row.Name,
		Slug:    row.Slug,
		LogoURL: row.LogoUrl,
	}
	if row.TmdbID.Valid {
		v := int64(row.TmdbID.Int32)
		s.TMDBID = &v
	}
	return s
}

// Compile-time check that domain.ErrNotFound is reachable here so
// future handler changes can return it directly from this repo if
// the contract grows that way.
var _ = domain.ErrNotFound
