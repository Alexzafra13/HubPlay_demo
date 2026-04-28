package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"hubplay/internal/db/sqlc"
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
}

type MetadataRepository struct {
	db *sql.DB // kept for batch queries with dynamic IN()
	q  *sqlc.Queries
}

func NewMetadataRepository(database *sql.DB) *MetadataRepository {
	return &MetadataRepository{db: database, q: sqlc.New(database)}
}

func (r *MetadataRepository) Upsert(ctx context.Context, m *Metadata) error {
	err := r.q.UpsertMetadata(ctx, sqlc.UpsertMetadataParams{
		ItemID:      m.ItemID,
		Overview:    nullableString(m.Overview),
		Tagline:     nullableString(m.Tagline),
		Studio:      nullableString(m.Studio),
		GenresJson:  nullableString(m.GenresJSON),
		TagsJson:    nullableString(m.TagsJSON),
		TrailerKey:  m.TrailerKey,
		TrailerSite: m.TrailerSite,
	})
	if err != nil {
		return fmt.Errorf("upsert metadata: %w", err)
	}
	return nil
}

func (r *MetadataRepository) GetByItemID(ctx context.Context, itemID string) (*Metadata, error) {
	row, err := r.q.GetMetadataByItemID(ctx, itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get metadata: %w", err)
	}
	m := metadataFromRow(row)
	return &m, nil
}

func (r *MetadataRepository) Delete(ctx context.Context, itemID string) error {
	return r.q.DeleteMetadata(ctx, itemID)
}

// GetOverviewBatch returns overview text for a batch of item IDs.
// Uses raw SQL because sqlc doesn't support dynamic IN() on SQLite.
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

	query := fmt.Sprintf(
		`SELECT item_id, COALESCE(overview,'') FROM metadata WHERE item_id IN (%s)`,
		joinStrings(placeholders, ","),
	)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get overview batch: %w", err)
	}
	defer rows.Close()

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
// Uses raw SQL because sqlc doesn't support dynamic IN() on SQLite.
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

	query := fmt.Sprintf(
		`SELECT item_id, COALESCE(overview,''), COALESCE(tagline,''),
		        COALESCE(studio,''), COALESCE(genres_json,''), COALESCE(tags_json,''),
		        COALESCE(trailer_key,''), COALESCE(trailer_site,'')
		 FROM metadata WHERE item_id IN (%s)`,
		joinStrings(placeholders, ","),
	)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get metadata batch: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*Metadata)
	for rows.Next() {
		m := &Metadata{}
		if err := rows.Scan(&m.ItemID, &m.Overview, &m.Tagline, &m.Studio, &m.GenresJSON, &m.TagsJSON, &m.TrailerKey, &m.TrailerSite); err != nil {
			return nil, fmt.Errorf("scan metadata: %w", err)
		}
		result[m.ItemID] = m
	}
	return result, rows.Err()
}

func metadataFromRow(r sqlc.GetMetadataByItemIDRow) Metadata {
	return Metadata{
		ItemID:      r.ItemID,
		Overview:    r.Overview,
		Tagline:     r.Tagline,
		Studio:      r.Studio,
		GenresJSON:  r.GenresJson,
		TagsJSON:    r.TagsJson,
		TrailerKey:  r.TrailerKey,
		TrailerSite: r.TrailerSite,
	}
}
