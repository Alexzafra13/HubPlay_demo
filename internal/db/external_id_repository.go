package db

import (
	"context"
	"database/sql"
	"fmt"
)

// ExternalID links an item to an external provider ID (tmdb, imdb, tvdb).
type ExternalID struct {
	ItemID     string
	Provider   string
	ExternalID string
}

type ExternalIDRepository struct {
	db *sql.DB
}

func NewExternalIDRepository(database *sql.DB) *ExternalIDRepository {
	return &ExternalIDRepository{db: database}
}

func (r *ExternalIDRepository) Upsert(ctx context.Context, e *ExternalID) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO external_ids (item_id, provider, external_id)
		 VALUES (?, ?, ?)
		 ON CONFLICT(item_id, provider) DO UPDATE SET external_id = excluded.external_id`,
		e.ItemID, e.Provider, e.ExternalID,
	)
	if err != nil {
		return fmt.Errorf("upsert external id: %w", err)
	}
	return nil
}

func (r *ExternalIDRepository) ListByItem(ctx context.Context, itemID string) ([]*ExternalID, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT item_id, provider, external_id FROM external_ids WHERE item_id = ?`, itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("list external ids: %w", err)
	}
	defer rows.Close()

	var ids []*ExternalID
	for rows.Next() {
		e := &ExternalID{}
		if err := rows.Scan(&e.ItemID, &e.Provider, &e.ExternalID); err != nil {
			return nil, fmt.Errorf("scan external id: %w", err)
		}
		ids = append(ids, e)
	}
	return ids, rows.Err()
}

func (r *ExternalIDRepository) GetByProvider(ctx context.Context, itemID, prov string) (*ExternalID, error) {
	e := &ExternalID{}
	err := r.db.QueryRowContext(ctx,
		`SELECT item_id, provider, external_id FROM external_ids WHERE item_id = ? AND provider = ?`,
		itemID, prov,
	).Scan(&e.ItemID, &e.Provider, &e.ExternalID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get external id: %w", err)
	}
	return e, nil
}

func (r *ExternalIDRepository) HasExternalID(ctx context.Context, itemID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM external_ids WHERE item_id = ?`, itemID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
