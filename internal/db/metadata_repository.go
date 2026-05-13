package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
)

// Metadata holds extended metadata for an item (overview, tagline, genres, etc.).
type Metadata struct {
	ItemID     string
	Overview   string
	Tagline    string
	Studio     string
	GenresJSON string
	TagsJSON   string
	// TrailerKey is the platform-specific id of the best-matched
	// trailer/teaser at scan time (typically a YouTube key from TMDb).
	// Empty when no trailer was returned for the item — the
	// SeriesHero treats absence as "no preview, just show the
	// backdrop". TrailerSite is the platform name ("YouTube",
	// "Vimeo") so the frontend picks the right embed URL.
	TrailerKey  string
	TrailerSite string
	// StudioLogoURL is the absolute image URL of the headline
	// production company / network logo (Lucasfilm, HBO, Disney+, …).
	// Built from TMDb's `production_companies[0].logo_path` at scan
	// time using the configured image base, so the frontend renders
	// it with a single `<img src>` and falls back to the studio text
	// when empty (older studios with no TMDb logo, or failed match).
	StudioLogoURL string
	// CollectionID is the FK to the saga (TMDb belongs_to_collection)
	// this movie belongs to — populated at scan time when the
	// provider returned one. Empty / NULL means "no saga"; the
	// detail page renders the optional "Part of: X" link only when
	// non-empty.
	CollectionID string
}

// MetadataRepository — Pattern A dual-dialect plus two raw-SQL
// batch readers (GetOverviewBatch, GetMetadataBatch) that need the
// dynamic IN() clause sqlc doesn't support.
type MetadataRepository struct {
	db *sql.DB
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewMetadataRepository(driver string, database *sql.DB) *MetadataRepository {
	r := &MetadataRepository{db: database}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *MetadataRepository) useSQLite() bool { return r.sq != nil }

func (r *MetadataRepository) driver() string {
	if r.useSQLite() {
		return DriverSQLite
	}
	return DriverPostgres
}

func (r *MetadataRepository) Upsert(ctx context.Context, m *Metadata) error {
	var err error
	if r.useSQLite() {
		err = r.sq.UpsertMetadata(ctx, sqlc.UpsertMetadataParams{
			ItemID:        m.ItemID,
			Overview:      nullableString(m.Overview),
			Tagline:       nullableString(m.Tagline),
			Studio:        nullableString(m.Studio),
			GenresJson:    nullableString(m.GenresJSON),
			TagsJson:      nullableString(m.TagsJSON),
			TrailerKey:    m.TrailerKey,
			TrailerSite:   m.TrailerSite,
			StudioLogoUrl: m.StudioLogoURL,
		})
	} else {
		err = r.pq.UpsertMetadata(ctx, sqlc_pg.UpsertMetadataParams{
			ItemID:        m.ItemID,
			Overview:      nullableString(m.Overview),
			Tagline:       nullableString(m.Tagline),
			Studio:        nullableString(m.Studio),
			GenresJson:    nullableString(m.GenresJSON),
			TagsJson:      nullableString(m.TagsJSON),
			TrailerKey:    m.TrailerKey,
			TrailerSite:   m.TrailerSite,
			StudioLogoUrl: m.StudioLogoURL,
		})
	}
	if err != nil {
		return fmt.Errorf("upsert metadata: %w", err)
	}
	return nil
}

func (r *MetadataRepository) GetByItemID(ctx context.Context, itemID string) (*Metadata, error) {
	if r.useSQLite() {
		row, err := r.sq.GetMetadataByItemID(ctx, itemID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get metadata: %w", err)
		}
		return &Metadata{
			ItemID:        row.ItemID,
			Overview:      row.Overview,
			Tagline:       row.Tagline,
			Studio:        row.Studio,
			GenresJSON:    row.GenresJson,
			TagsJSON:      row.TagsJson,
			TrailerKey:    row.TrailerKey,
			TrailerSite:   row.TrailerSite,
			StudioLogoURL: row.StudioLogoUrl,
			CollectionID:  row.CollectionID,
		}, nil
	}
	row, err := r.pq.GetMetadataByItemID(ctx, itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get metadata: %w", err)
	}
	return &Metadata{
		ItemID:        row.ItemID,
		Overview:      row.Overview,
		Tagline:       row.Tagline,
		Studio:        row.Studio,
		GenresJSON:    row.GenresJson,
		TagsJSON:      row.TagsJson,
		TrailerKey:    row.TrailerKey,
		TrailerSite:   row.TrailerSite,
		StudioLogoURL: row.StudioLogoUrl,
		CollectionID:  row.CollectionID,
	}, nil
}

func (r *MetadataRepository) Delete(ctx context.Context, itemID string) error {
	if r.useSQLite() {
		return r.sq.DeleteMetadata(ctx, itemID)
	}
	return r.pq.DeleteMetadata(ctx, itemID)
}

// GetOverviewBatch returns overview text for a batch of item IDs.
// Uses raw SQL because sqlc doesn't support dynamic IN() on either dialect.
func (r *MetadataRepository) GetOverviewBatch(ctx context.Context, itemIDs []string) (map[string]string, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(itemIDs))
	args := make([]any, len(itemIDs))
	for i, id := range itemIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := rewritePlaceholders(r.driver(), fmt.Sprintf(
		`SELECT item_id, COALESCE(overview,'') FROM metadata WHERE item_id IN (%s)`,
		joinStrings(placeholders, ","),
	))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get overview batch: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	result := make(map[string]string)
	for rows.Next() {
		var itemID, overview string
		if err := rows.Scan(&itemID, &overview); err != nil {
			return nil, fmt.Errorf("scan overview: %w", err)
		}
		if overview != "" {
			result[itemID] = overview
		}
	}
	return result, rows.Err()
}

// GetMetadataBatch returns metadata for a batch of item IDs.
// Uses raw SQL because sqlc doesn't support dynamic IN() on either dialect.
func (r *MetadataRepository) GetMetadataBatch(ctx context.Context, itemIDs []string) (map[string]*Metadata, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(itemIDs))
	args := make([]any, len(itemIDs))
	for i, id := range itemIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := rewritePlaceholders(r.driver(), fmt.Sprintf(
		`SELECT item_id, COALESCE(overview,''), COALESCE(tagline,''),
		        COALESCE(studio,''), COALESCE(genres_json,''), COALESCE(tags_json,''),
		        COALESCE(trailer_key,''), COALESCE(trailer_site,''),
		        COALESCE(studio_logo_url,'')
		 FROM metadata WHERE item_id IN (%s)`,
		joinStrings(placeholders, ","),
	))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get metadata batch: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	result := make(map[string]*Metadata)
	for rows.Next() {
		m := &Metadata{}
		if err := rows.Scan(&m.ItemID, &m.Overview, &m.Tagline, &m.Studio, &m.GenresJSON, &m.TagsJSON, &m.TrailerKey, &m.TrailerSite, &m.StudioLogoURL); err != nil {
			return nil, fmt.Errorf("scan metadata: %w", err)
		}
		result[m.ItemID] = m
	}
	return result, rows.Err()
}
