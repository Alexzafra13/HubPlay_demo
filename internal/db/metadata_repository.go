package db

import (
	"context"
	"database/sql"
	"fmt"
)

// Metadata holds extended metadata for an item (overview, tagline, genres, etc.).
type Metadata struct {
	ItemID    string
	Overview  string
	Tagline   string
	Studio    string
	GenresJSON string
	TagsJSON  string
}

type MetadataRepository struct {
	db *sql.DB
}

func NewMetadataRepository(database *sql.DB) *MetadataRepository {
	return &MetadataRepository{db: database}
}

func (r *MetadataRepository) Upsert(ctx context.Context, m *Metadata) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO metadata (item_id, overview, tagline, studio, genres_json, tags_json)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(item_id) DO UPDATE SET
		   overview = excluded.overview,
		   tagline = excluded.tagline,
		   studio = excluded.studio,
		   genres_json = excluded.genres_json,
		   tags_json = excluded.tags_json`,
		m.ItemID, m.Overview, m.Tagline, m.Studio, m.GenresJSON, m.TagsJSON,
	)
	if err != nil {
		return fmt.Errorf("upsert metadata: %w", err)
	}
	return nil
}

func (r *MetadataRepository) GetByItemID(ctx context.Context, itemID string) (*Metadata, error) {
	m := &Metadata{}
	err := r.db.QueryRowContext(ctx,
		`SELECT item_id, COALESCE(overview,''), COALESCE(tagline,''),
		        COALESCE(studio,''), COALESCE(genres_json,''), COALESCE(tags_json,'')
		 FROM metadata WHERE item_id = ?`, itemID,
	).Scan(&m.ItemID, &m.Overview, &m.Tagline, &m.Studio, &m.GenresJSON, &m.TagsJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get metadata: %w", err)
	}
	return m, nil
}

// GetOverviewBatch returns overview text for a batch of item IDs.
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
		        COALESCE(studio,''), COALESCE(genres_json,''), COALESCE(tags_json,'')
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
		if err := rows.Scan(&m.ItemID, &m.Overview, &m.Tagline, &m.Studio, &m.GenresJSON, &m.TagsJSON); err != nil {
			return nil, fmt.Errorf("scan metadata: %w", err)
		}
		result[m.ItemID] = m
	}
	return result, rows.Err()
}

func (r *MetadataRepository) Delete(ctx context.Context, itemID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM metadata WHERE item_id = ?`, itemID)
	return err
}
