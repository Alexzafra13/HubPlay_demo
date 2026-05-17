package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
)

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

func (r *MetadataRepository) Upsert(ctx context.Context, m *librarymodel.Metadata) error {
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

func (r *MetadataRepository) GetByItemID(ctx context.Context, itemID string) (*librarymodel.Metadata, error) {
	if r.useSQLite() {
		row, err := r.sq.GetMetadataByItemID(ctx, itemID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get metadata: %w", err)
		}
		return &librarymodel.Metadata{
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
	return &librarymodel.Metadata{
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
func (r *MetadataRepository) GetMetadataBatch(ctx context.Context, itemIDs []string) (map[string]*librarymodel.Metadata, error) {
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

	result := make(map[string]*librarymodel.Metadata)
	for rows.Next() {
		m := &librarymodel.Metadata{}
		if err := rows.Scan(&m.ItemID, &m.Overview, &m.Tagline, &m.Studio, &m.GenresJSON, &m.TagsJSON, &m.TrailerKey, &m.TrailerSite, &m.StudioLogoURL); err != nil {
			return nil, fmt.Errorf("scan metadata: %w", err)
		}
		result[m.ItemID] = m
	}
	return result, rows.Err()
}
