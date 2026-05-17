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

// ExternalIDRepository — Pattern A dual-dialect plus one raw-SQL
// holdout (GetItemIDByExternalID — sqlc 1.31.1 truncates the trailing
// `LIMIT 1`).
type ExternalIDRepository struct {
	db *sql.DB
	sq *sqlc.Queries
	pq *sqlc_pg.Queries

	getItemIDSQL string
}

func NewExternalIDRepository(driver string, database *sql.DB) *ExternalIDRepository {
	r := &ExternalIDRepository{
		db: database,
		getItemIDSQL: rewritePlaceholders(driver,
			`SELECT item_id FROM external_ids WHERE provider = ? AND external_id = ? LIMIT 1`),
	}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *ExternalIDRepository) useSQLite() bool { return r.sq != nil }

func (r *ExternalIDRepository) Upsert(ctx context.Context, e *librarymodel.ExternalID) error {
	var err error
	if r.useSQLite() {
		err = r.sq.UpsertExternalID(ctx, sqlc.UpsertExternalIDParams{
			ItemID:     e.ItemID,
			Provider:   e.Provider,
			ExternalID: e.ExternalID,
		})
	} else {
		err = r.pq.UpsertExternalID(ctx, sqlc_pg.UpsertExternalIDParams{
			ItemID:     e.ItemID,
			Provider:   e.Provider,
			ExternalID: e.ExternalID,
		})
	}
	if err != nil {
		return fmt.Errorf("upsert external id: %w", err)
	}
	return nil
}

func (r *ExternalIDRepository) ListByItem(ctx context.Context, itemID string) ([]*librarymodel.ExternalID, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListExternalIDsByItem(ctx, itemID)
		if err != nil {
			return nil, fmt.Errorf("list external ids: %w", err)
		}
		return externalIDsFromSqliteRows(rows), nil
	}
	rows, err := r.pq.ListExternalIDsByItem(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("list external ids: %w", err)
	}
	return externalIDsFromPgRows(rows), nil
}

func (r *ExternalIDRepository) GetByProvider(ctx context.Context, itemID, prov string) (*librarymodel.ExternalID, error) {
	if r.useSQLite() {
		row, err := r.sq.GetExternalIDByProvider(ctx, sqlc.GetExternalIDByProviderParams{
			ItemID:   itemID,
			Provider: prov,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get external id: %w", err)
		}
		e := librarymodel.ExternalID{ItemID: row.ItemID, Provider: row.Provider, ExternalID: row.ExternalID}
		return &e, nil
	}
	row, err := r.pq.GetExternalIDByProvider(ctx, sqlc_pg.GetExternalIDByProviderParams{
		ItemID:   itemID,
		Provider: prov,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get external id: %w", err)
	}
	e := librarymodel.ExternalID{ItemID: row.ItemID, Provider: row.Provider, ExternalID: row.ExternalID}
	return &e, nil
}

func (r *ExternalIDRepository) HasExternalID(ctx context.Context, itemID string) (bool, error) {
	var (
		cnt int64
		err error
	)
	if r.useSQLite() {
		cnt, err = r.sq.CountExternalIDsByItem(ctx, itemID)
	} else {
		cnt, err = r.pq.CountExternalIDsByItem(ctx, itemID)
	}
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
//
// Raw SQL because sqlc v1.31.1 truncates the trailing identifier of
// the final query in a file: `LIMIT 1` becomes `LIMIT`, producing
// invalid SQL that fails at runtime. Same workaround as
// item_value_repository.go::ListGenres.
func (r *ExternalIDRepository) GetItemIDByExternalID(ctx context.Context, provider, externalID string) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx, r.getItemIDSQL, provider, externalID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get item by external id: %w", err)
	}
	return id, nil
}

func externalIDsFromSqliteRows(rows []sqlc.ExternalID) []*librarymodel.ExternalID {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*librarymodel.ExternalID, len(rows))
	for i, row := range rows {
		out[i] = &librarymodel.ExternalID{ItemID: row.ItemID, Provider: row.Provider, ExternalID: row.ExternalID}
	}
	return out
}

func externalIDsFromPgRows(rows []sqlc_pg.ExternalID) []*librarymodel.ExternalID {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*librarymodel.ExternalID, len(rows))
	for i, row := range rows {
		out[i] = &librarymodel.ExternalID{ItemID: row.ItemID, Provider: row.Provider, ExternalID: row.ExternalID}
	}
	return out
}
