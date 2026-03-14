package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ProviderConfig represents a provider's persisted configuration.
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
	db *sql.DB
}

func NewProviderRepository(database *sql.DB) *ProviderRepository {
	return &ProviderRepository{db: database}
}

// Upsert creates or updates a provider configuration.
func (r *ProviderRepository) Upsert(ctx context.Context, p *ProviderConfig) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO providers (name, type, version, status, priority, config_json, api_key, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   type = excluded.type,
		   version = excluded.version,
		   status = excluded.status,
		   priority = excluded.priority,
		   config_json = excluded.config_json,
		   api_key = excluded.api_key,
		   updated_at = excluded.updated_at`,
		p.Name, p.Type, p.Version, p.Status, p.Priority, p.ConfigJSON, p.APIKey, now, now,
	)
	if err != nil {
		return fmt.Errorf("upsert provider: %w", err)
	}
	return nil
}

// GetByName returns a provider config by name.
func (r *ProviderRepository) GetByName(ctx context.Context, name string) (*ProviderConfig, error) {
	p := &ProviderConfig{}
	err := r.db.QueryRowContext(ctx,
		`SELECT name, type, version, status, priority,
		        COALESCE(config_json, ''), COALESCE(api_key, ''),
		        created_at, updated_at
		 FROM providers WHERE name = ?`, name,
	).Scan(&p.Name, &p.Type, &p.Version, &p.Status, &p.Priority,
		&p.ConfigJSON, &p.APIKey, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	return p, nil
}

// ListActive returns all active providers, ordered by priority.
func (r *ProviderRepository) ListActive(ctx context.Context) ([]*ProviderConfig, error) {
	return r.list(ctx, `SELECT name, type, version, status, priority,
		COALESCE(config_json, ''), COALESCE(api_key, ''), created_at, updated_at
		FROM providers WHERE status = 'active' ORDER BY priority, name`)
}

// ListAll returns all providers, ordered by priority.
func (r *ProviderRepository) ListAll(ctx context.Context) ([]*ProviderConfig, error) {
	return r.list(ctx, `SELECT name, type, version, status, priority,
		COALESCE(config_json, ''), COALESCE(api_key, ''), created_at, updated_at
		FROM providers ORDER BY priority, name`)
}

// ListByType returns providers of a specific type.
func (r *ProviderRepository) ListByType(ctx context.Context, providerType string) ([]*ProviderConfig, error) {
	return r.list(ctx, `SELECT name, type, version, status, priority,
		COALESCE(config_json, ''), COALESCE(api_key, ''), created_at, updated_at
		FROM providers WHERE type = ? AND status = 'active' ORDER BY priority, name`, providerType)
}

// SetStatus enables or disables a provider.
func (r *ProviderRepository) SetStatus(ctx context.Context, name, status string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE providers SET status = ?, updated_at = ? WHERE name = ?`,
		status, time.Now(), name,
	)
	if err != nil {
		return fmt.Errorf("set provider status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("provider %q not found", name)
	}
	return nil
}

// Delete removes a provider config.
func (r *ProviderRepository) Delete(ctx context.Context, name string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM providers WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	return nil
}

func (r *ProviderRepository) list(ctx context.Context, query string, args ...any) ([]*ProviderConfig, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var providers []*ProviderConfig
	for rows.Next() {
		p := &ProviderConfig{}
		if err := rows.Scan(&p.Name, &p.Type, &p.Version, &p.Status, &p.Priority,
			&p.ConfigJSON, &p.APIKey, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}
