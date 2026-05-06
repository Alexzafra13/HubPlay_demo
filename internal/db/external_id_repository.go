package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"hubplay/internal/db/sqlc"
)

// ExternalID links an item to an external provider ID (tmdb, imdb, tvdb).
type ExternalID struct {
	ItemID     string
	Provider   string
	ExternalID string
}

type ExternalIDRepository struct {
	q *sqlc.Queries
}

func NewExternalIDRepository(database *sql.DB) *ExternalIDRepository {
	return &ExternalIDRepository{q: sqlc.New(database)}
}

func (r *ExternalIDRepository) Upsert(ctx context.Context, e *ExternalID) error {
	err := r.q.UpsertExternalID(ctx, sqlc.UpsertExternalIDParams{
		ItemID:     e.ItemID,
		Provider:   e.Provider,
		ExternalID: e.ExternalID,
	})
	if err != nil {
		return fmt.Errorf("upsert external id: %w", err)
	}
	return nil
}

func (r *ExternalIDRepository) ListByItem(ctx context.Context, itemID string) ([]*ExternalID, error) {
	rows, err := r.q.ListExternalIDsByItem(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("list external ids: %w", err)
	}
	return externalIDsFromRows(rows), nil
}

func (r *ExternalIDRepository) GetByProvider(ctx context.Context, itemID, prov string) (*ExternalID, error) {
	row, err := r.q.GetExternalIDByProvider(ctx, sqlc.GetExternalIDByProviderParams{
		ItemID:   itemID,
		Provider: prov,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get external id: %w", err)
	}
	e := externalIDFromRow(row)
	return &e, nil
}

func (r *ExternalIDRepository) HasExternalID(ctx context.Context, itemID string) (bool, error) {
	cnt, err := r.q.CountExternalIDsByItem(ctx, itemID)
	if err != nil {
		return false, fmt.Errorf("count external ids: %w", err)
	}
	return cnt > 0, nil
}

// GetItemIDByExternalID does the reverse lookup of GetByProvider:
// given (provider, external_id) returns the local item id that
// carries that mapping, or empty string if none. Used by the
// recommendations endpoint to cross-reference TMDb candidates
// against the user's library so each suggestion can be marked
// "in library" with a deep link or "external" with a TMDb link.
func (r *ExternalIDRepository) GetItemIDByExternalID(ctx context.Context, provider, externalID string) (string, error) {
	id, err := r.q.GetItemIDByExternalID(ctx, sqlc.GetItemIDByExternalIDParams{
		Provider:   provider,
		ExternalID: externalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get item by external id: %w", err)
	}
	return id, nil
}

func externalIDFromRow(r sqlc.ExternalID) ExternalID {
	return ExternalID{
		ItemID:     r.ItemID,
		Provider:   r.Provider,
		ExternalID: r.ExternalID,
	}
}

func externalIDsFromRows(rows []sqlc.ExternalID) []*ExternalID {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*ExternalID, len(rows))
	for i, row := range rows {
		e := externalIDFromRow(row)
		out[i] = &e
	}
	return out
}
