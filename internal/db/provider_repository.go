package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
)

// ProviderConfig represents a provider's persisted configuration.
// Nullable columns (config_json, api_key) surface as plain strings — empty
// means NULL in SQL. The conversion happens at the adapter boundary.
type ProviderConfig struct {
	Name       string
	Type       string // metadata, image, subtitle
	Version    string
	Status     string // active, disabled
	Priority   int    // lower = higher priority
	ConfigJSON string
	APIKey     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ProviderRepository — Pattern A dual-dialect. Priority is INTEGER →
// int64 in SQLite, int32 in Postgres; param cast happens at the call
// site so the domain type stays a plain `int`.
type ProviderRepository struct {
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewProviderRepository(driver string, database *sql.DB) *ProviderRepository {
	r := &ProviderRepository{}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *ProviderRepository) useSQLite() bool { return r.sq != nil }

// Upsert creates or updates a provider configuration. Both created_at and
// updated_at are stamped with `now` on insert; only updated_at is touched on
// conflict (the SQL's DO UPDATE set) — created_at of existing rows is preserved.
func (r *ProviderRepository) Upsert(ctx context.Context, p *ProviderConfig) error {
	now := timeNow()
	var err error
	if r.useSQLite() {
		err = r.sq.UpsertProvider(ctx, sqlc.UpsertProviderParams{
			Name:       p.Name,
			Type:       p.Type,
			Version:    p.Version,
			Status:     p.Status,
			Priority:   int64(p.Priority),
			ConfigJson: nullableString(p.ConfigJSON),
			ApiKey:     nullableString(p.APIKey),
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	} else {
		err = r.pq.UpsertProvider(ctx, sqlc_pg.UpsertProviderParams{
			Name:       p.Name,
			Type:       p.Type,
			Version:    p.Version,
			Status:     p.Status,
			Priority:   int32(p.Priority),
			ConfigJson: nullableString(p.ConfigJSON),
			ApiKey:     nullableString(p.APIKey),
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	if err != nil {
		return fmt.Errorf("upsert provider: %w", err)
	}
	return nil
}

// GetByName returns a provider config by name. Returns (nil, nil) when not
// found — callers treat that as "no config, use defaults". Intentionally
// different from signing_keys/sessions which surface domain.ErrNotFound.
func (r *ProviderRepository) GetByName(ctx context.Context, name string) (*ProviderConfig, error) {
	if r.useSQLite() {
		row, err := r.sq.GetProvider(ctx, name)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get provider: %w", err)
		}
		p := providerFromSqliteRow(row)
		return &p, nil
	}
	row, err := r.pq.GetProvider(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	p := providerFromPgRow(row)
	return &p, nil
}

// ListActive returns all active providers, ordered by priority.
func (r *ProviderRepository) ListActive(ctx context.Context) ([]*ProviderConfig, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListActiveProviders(ctx)
		if err != nil {
			return nil, fmt.Errorf("list active providers: %w", err)
		}
		return providersFromSqliteRows(rows), nil
	}
	rows, err := r.pq.ListActiveProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active providers: %w", err)
	}
	return providersFromPgRows(rows), nil
}

// ListAll returns all providers, ordered by priority.
func (r *ProviderRepository) ListAll(ctx context.Context) ([]*ProviderConfig, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListProviders(ctx)
		if err != nil {
			return nil, fmt.Errorf("list providers: %w", err)
		}
		return providersFromSqliteRows(rows), nil
	}
	rows, err := r.pq.ListProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	return providersFromPgRows(rows), nil
}

// ListByType returns active providers of a specific type.
func (r *ProviderRepository) ListByType(ctx context.Context, providerType string) ([]*ProviderConfig, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListProvidersByType(ctx, providerType)
		if err != nil {
			return nil, fmt.Errorf("list providers by type: %w", err)
		}
		return providersFromSqliteRows(rows), nil
	}
	rows, err := r.pq.ListProvidersByType(ctx, providerType)
	if err != nil {
		return nil, fmt.Errorf("list providers by type: %w", err)
	}
	return providersFromPgRows(rows), nil
}

// SetStatus enables or disables a provider. Returns an error if the provider
// does not exist (unchanged from the previous hand-written behaviour).
func (r *ProviderRepository) SetStatus(ctx context.Context, name, status string) error {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.SetProviderStatus(ctx, sqlc.SetProviderStatusParams{
			Status:    status,
			UpdatedAt: timeNow(),
			Name:      name,
		})
	} else {
		n, err = r.pq.SetProviderStatus(ctx, sqlc_pg.SetProviderStatusParams{
			Status:    status,
			UpdatedAt: timeNow(),
			Name:      name,
		})
	}
	if err != nil {
		return fmt.Errorf("set provider status: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("provider %q not found", name)
	}
	return nil
}

// Delete removes a provider config. No-op if the name doesn't exist
// (unchanged from the previous hand-written behaviour).
func (r *ProviderRepository) Delete(ctx context.Context, name string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteProvider(ctx, name)
	} else {
		err = r.pq.DeleteProvider(ctx, name)
	}
	if err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	return nil
}

// providerFromSqliteRow maps the sqlc-generated row to the domain shape.
// Priority widens from int64 to int (always fits — we set it from an int
// in the first place).
func providerFromSqliteRow(r sqlc.Provider) ProviderConfig {
	return ProviderConfig{
		Name:       r.Name,
		Type:       r.Type,
		Version:    r.Version,
		Status:     r.Status,
		Priority:   int(r.Priority),
		ConfigJSON: r.ConfigJson.String,
		APIKey:     r.ApiKey.String,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
}

func providerFromPgRow(r sqlc_pg.Provider) ProviderConfig {
	return ProviderConfig{
		Name:       r.Name,
		Type:       r.Type,
		Version:    r.Version,
		Status:     r.Status,
		Priority:   int(r.Priority),
		ConfigJSON: r.ConfigJson.String,
		APIKey:     r.ApiKey.String,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
}

func providersFromSqliteRows(rows []sqlc.Provider) []*ProviderConfig {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*ProviderConfig, len(rows))
	for i, row := range rows {
		p := providerFromSqliteRow(row)
		out[i] = &p
	}
	return out
}

func providersFromPgRows(rows []sqlc_pg.Provider) []*ProviderConfig {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*ProviderConfig, len(rows))
	for i, row := range rows {
		p := providerFromPgRow(row)
		out[i] = &p
	}
	return out
}
