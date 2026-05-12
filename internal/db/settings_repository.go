package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"hubplay/internal/domain"
)

// SettingsRepository persists admin-editable runtime settings (the
// app_settings table from migration 019). Thin key-value layer; the
// whitelist + serialisation lives one layer up at the admin handler.
//
// Dual-dialect strategy: raw SQL with `?` placeholders pre-rewritten
// to `$N` at construction time when the postgres driver is selected.
// The pre-rewrite means per-query cost is zero — the SQL strings are
// already in the right shape by the time any method runs. This is the
// pattern shared with the other ~7 raw-SQL repos in the project (most
// of which bypass sqlc due to the 1.31.1 parser bugs documented in
// docs/memory/architecture-decisions.md).
//
// Why raw SQL and not sqlc here: settings is 4 trivial queries.
// sqlc's typed-struct generation gains nothing over four string
// constants and a single helper. The simpler shape is also bug-proof
// against the sqlc 1.31.1 parser issues that bite when CURRENT_TIMESTAMP
// is combined with ON CONFLICT in upsert queries (verified empirically
// during the dual-dialect refactor).
//
// Nil-safe: a typed-nil *SettingsRepository wrapped in an interface
// is the trap that catches every caller's `if r == nil` guard.
// Returning ErrNotFound from Get / no-op from Delete lets the
// GetOr-default-fallback path work transparently for partial wiring.
type SettingsRepository struct {
	db *sql.DB

	// Pre-rewritten SQL — built once in the constructor per driver.
	// `?` on SQLite, `$1`/`$2`/... on Postgres. Per-call cost is a
	// single string read.
	getSQL    string
	upsertSQL string
	deleteSQL string
	listSQL   string
}

// NewSettingsRepository wires the repo against the chosen backend.
// `driver` accepts "postgres" or anything-else-meaning-sqlite — the
// default branch keeps fresh-install + tests on SQLite without
// callers having to pass an explicit driver string.
func NewSettingsRepository(driver string, database *sql.DB) *SettingsRepository {
	return &SettingsRepository{
		db: database,
		getSQL: rewritePlaceholders(driver,
			`SELECT value FROM app_settings WHERE key = ?`),
		upsertSQL: rewritePlaceholders(driver,
			`INSERT INTO app_settings (key, value, updated_at)
			 VALUES (?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(key) DO UPDATE SET
			    value      = excluded.value,
			    updated_at = excluded.updated_at`),
		deleteSQL: rewritePlaceholders(driver,
			`DELETE FROM app_settings WHERE key = ?`),
		listSQL: `SELECT key, value FROM app_settings`,
	}
}

// Get returns the stored value for key. domain.ErrNotFound when the
// row is absent — callers should layer their YAML / env default on
// top via GetOr below rather than treating absence as an error.
func (r *SettingsRepository) Get(ctx context.Context, key string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("setting %q: %w", key, domain.ErrNotFound)
	}
	var value string
	err := r.db.QueryRowContext(ctx, r.getSQL, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("setting %q: %w", key, domain.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("get setting %q: %w", key, err)
	}
	return value, nil
}

// GetOr returns the stored value for key, falling back to def when
// nothing is stored. The fallback is the path through which YAML /
// env defaults reach runtime: callers pass cfg.Whatever as def and
// the DB row (if any) overrides it.
func (r *SettingsRepository) GetOr(ctx context.Context, key, def string) (string, error) {
	value, err := r.Get(ctx, key)
	if errors.Is(err, domain.ErrNotFound) {
		return def, nil
	}
	if err != nil {
		return def, err
	}
	return value, nil
}

// Set upserts the value for key. Writing to a nil receiver is a
// programming error (the admin endpoint should have refused the
// request before reaching here) so we surface a clear error rather
// than silently dropping the write.
func (r *SettingsRepository) Set(ctx context.Context, key, value string) error {
	if r == nil {
		return fmt.Errorf("set setting %q: settings repository not initialised", key)
	}
	if _, err := r.db.ExecContext(ctx, r.upsertSQL, key, value); err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}

// Delete removes a setting so the next Get falls back to the YAML
// default. Used by the admin "reset to default" affordance.
func (r *SettingsRepository) Delete(ctx context.Context, key string) error {
	if r == nil {
		return fmt.Errorf("delete setting %q: settings repository not initialised", key)
	}
	if _, err := r.db.ExecContext(ctx, r.deleteSQL, key); err != nil {
		return fmt.Errorf("delete setting %q: %w", key, err)
	}
	return nil
}

// All returns every stored setting. Used by the admin GET endpoint
// to hydrate the UI on first load. Nil receiver returns an empty
// map so a partial wiring renders defaults.
func (r *SettingsRepository) All(ctx context.Context) (map[string]string, error) {
	if r == nil {
		return map[string]string{}, nil
	}
	rows, err := r.db.QueryContext(ctx, r.listSQL)
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		out[k] = v
	}
	return out, rows.Err()
}
