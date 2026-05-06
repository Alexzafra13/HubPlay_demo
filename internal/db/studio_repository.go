package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/domain"
)

// Studio is a production company / TV network surfaced as a first-class
// entity. The same table backs both because to a viewer they are the
// same brand mark — Marvel Studios for movies, HBO for TV. TMDbID is
// the upstream id when the scanner had a provider match (NULL means
// the row is a backfill from the legacy free-form `metadata.studio`
// text, which carries no provider id).
type Studio struct {
	ID      string
	TMDBID  *int64
	Name    string
	Slug    string
	LogoURL string
}

// StudioListEntry is the {studio + item count} pair the browse page
// renders. Sorted server-side by count desc so the UI doesn't need
// a second pass.
type StudioListEntry struct {
	ID        string
	Name      string
	Slug      string
	LogoURL   string
	ItemCount int64
}

type StudioRepository struct {
	db *sql.DB // ListItemsForStudio uses raw SQL (see queries/studios.sql)
	q  *sqlc.Queries
}

func NewStudioRepository(database *sql.DB) *StudioRepository {
	return &StudioRepository{db: database, q: sqlc.New(database)}
}

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
		err := r.q.UpsertStudio(ctx, sqlc.UpsertStudioParams{
			ID:      id,
			TmdbID:  sql.NullInt64{Int64: *tmdbID, Valid: true},
			Name:    name,
			Slug:    slug,
			LogoUrl: logoURL,
		})
		if err != nil {
			return "", fmt.Errorf("upsert studio (tmdb=%d): %w", *tmdbID, err)
		}
		// The conflict resolution may have kept the existing row; resolve
		// the actual id by tmdb_id rather than trusting the inserted id
		// (different slug → different id, but the tmdb_id wins).
		row, gerr := r.q.GetStudioByTMDBID(ctx, sql.NullInt64{Int64: *tmdbID, Valid: true})
		if gerr == nil {
			return row.ID, nil
		}
		return id, nil
	}
	// No tmdb_id: dedupe by slug (legacy backfill path).
	err := r.q.UpsertStudioBySlug(ctx, sqlc.UpsertStudioBySlugParams{
		ID:      id,
		TmdbID:  sql.NullInt64{Valid: false},
		Name:    name,
		Slug:    slug,
		LogoUrl: logoURL,
	})
	if err != nil {
		return "", fmt.Errorf("upsert studio (slug=%s): %w", slug, err)
	}
	return id, nil
}

// GetBySlug fetches the canonical row for /studios/<slug> rendering.
// Returns (nil, nil) when no studio matches — handler converts to 404.
func (r *StudioRepository) GetBySlug(ctx context.Context, slug string) (*Studio, error) {
	row, err := r.q.GetStudioBySlug(ctx, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get studio by slug: %w", err)
	}
	s := studioFromRow(row)
	return &s, nil
}

// List returns every studio that has at least one item linked to it,
// sorted by item count desc. Drives the /studios browse page.
func (r *StudioRepository) List(ctx context.Context) ([]*StudioListEntry, error) {
	rows, err := r.q.ListStudios(ctx)
	if err != nil {
		return nil, fmt.Errorf("list studios: %w", err)
	}
	out := make([]*StudioListEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, &StudioListEntry{
			ID:        row.ID,
			Name:      row.Name,
			Slug:      row.Slug,
			LogoURL:   row.LogoUrl,
			ItemCount: row.ItemCount,
		})
	}
	return out, nil
}

// StudioItem is the slim row /studios/<slug> renders in its grid:
// just enough to plot a poster + title + year card with a deep-link
// to /items/{id}. The poster image id resolves through
// /api/v1/images/file/{id} same as the recommendations rail.
type StudioItem struct {
	ID             string
	Type           string
	Title          string
	Year           int
	PrimaryImageID string
}

// ListItemsForStudio returns the catalogue's items linked to this
// studio id, sorted year-desc. Raw SQL because the trailing ORDER BY
// hits the sqlc v1.31.1 truncation bug we work around in two other
// places already.
func (r *StudioRepository) ListItemsForStudio(ctx context.Context, studioID string) ([]*StudioItem, error) {
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
		WHERE m.studio_id = ?
		  AND i.is_available = 1
		  AND i.type IN ('movie', 'series')
		ORDER BY COALESCE(i.year, 0) DESC, i.title ASC`
	rows, err := r.db.QueryContext(ctx, query, studioID)
	if err != nil {
		return nil, fmt.Errorf("list items for studio %s: %w", studioID, err)
	}
	defer rows.Close() //nolint:errcheck
	out := make([]*StudioItem, 0)
	for rows.Next() {
		var it StudioItem
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
	const query = `UPDATE metadata SET studio_id = NULLIF(?, '') WHERE item_id = ?`
	if _, err := r.db.ExecContext(ctx, query, studioID, itemID); err != nil {
		return fmt.Errorf("set item studio: %w", err)
	}
	return nil
}

func studioFromRow(row sqlc.Studio) Studio {
	s := Studio{
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

// Compile-time check that domain.ErrNotFound is reachable here so
// future handler changes can return it directly from this repo if
// the contract grows that way.
var _ = domain.ErrNotFound
