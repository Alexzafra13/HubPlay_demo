package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
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

type ProviderRepository struct {
	q *sqlc.Queries
}

func NewProviderRepository(database *sql.DB) *ProviderRepository {
	return &ProviderRepository{q: sqlc.New(database)}
}

// Upsert creates or updates a provider configuration. Both created_at and
// updated_at are stamped with `now` on insert; only updated_at is touched on
// conflict (the SQL's DO UPDATE set) — created_at of existing rows is preserved.
func (r *ProviderRepository) Upsert(ctx context.Context, p *ProviderConfig) error {
	now := time.Now()
	err := r.q.UpsertProvider(ctx, sqlc.UpsertProviderParams{
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
	if err != nil {
		return fmt.Errorf("upsert provider: %w", err)
	}
	return nil
}

// GetByName returns a provider config by name. Returns (nil, nil) when not
// found — callers treat that as "no config, use defaults". Intentionally
// different from signing_keys/sessions which surface domain.ErrNotFound.
func (r *ProviderRepository) GetByName(ctx context.Context, name string) (*ProviderConfig, error) {
	row, err := r.q.GetProvider(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	p := providerFromRow(row)
	return &p, nil
}

// ListActive returns all active providers, ordered by priority.
func (r *ProviderRepository) ListActive(ctx context.Context) ([]*ProviderConfig, error) {
	rows, err := r.q.ListActiveProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active providers: %w", err)
	}
	return providersFromRows(rows), nil
}

// ListAll returns all providers, ordered by priority.
func (r *ProviderRepository) ListAll(ctx context.Context) ([]*ProviderConfig, error) {
	rows, err := r.q.ListProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	return providersFromRows(rows), nil
}

// ListByType returns active providers of a specific type.
func (r *ProviderRepository) ListByType(ctx context.Context, providerType string) ([]*ProviderConfig, error) {
	rows, err := r.q.ListProvidersByType(ctx, providerType)
	if err != nil {
		return nil, fmt.Errorf("list providers by type: %w", err)
	}
	return providersFromRows(rows), nil
}

// SetStatus enables or disables a provider. Returns an error if the provider
// does not exist (unchanged from the previous hand-written behaviour).
func (r *ProviderRepository) SetStatus(ctx context.Context, name, status string) error {
	n, err := r.q.SetProviderStatus(ctx, sqlc.SetProviderStatusParams{
		Status:    status,
		UpdatedAt: time.Now(),
		Name:      name,
	})
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
	if err := r.q.DeleteProvider(ctx, name); err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	return nil
}

// providerFromRow maps sqlc.Provider (nullable config_json / api_key,
// int64 priority) to the domain ProviderConfig (plain strings, int priority).
func providerFromRow(r sqlc.Provider) ProviderConfig {
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

func providersFromRows(rows []sqlc.Provider) []*ProviderConfig {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*ProviderConfig, len(rows))
	for i, row := range rows {
		p := providerFromRow(row)
		out[i] = &p
	}
	return out
}
