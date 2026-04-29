package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"hubplay/internal/domain"
)

// SettingsRepository persists admin-editable runtime settings (the
// app_settings table from migration 019). It is intentionally a thin
// key-value layer with no schema for the values themselves — the
// whitelist + serialisation lives one layer up at the admin handler,
// because that is where "this setting is a bool / a URL / a duration"
// is actually known. Keeping types out of the repo means a new setting
// joins the whitelist with one line; the repo never grows.
type SettingsRepository struct {
	db *sql.DB
}

func NewSettingsRepository(database *sql.DB) *SettingsRepository {
	return &SettingsRepository{db: database}
}

// Get returns the stored value for key. domain.ErrNotFound when the
// row is absent — callers should layer their YAML / env default on
// top via GetOr below rather than treating absence as an error.
//
// Nil-safe receiver: a nil *SettingsRepository wrapped in a
// SettingsReader interface is the typed-nil-in-interface trap that
// catches every caller's `if r == nil` guard. Returning ErrNotFound
// here lets the GetOr default-fallback path work transparently for
// a wiring that didn't get a real repo (test rigs, deployments
// where settings construction failed).
func (r *SettingsRepository) Get(ctx context.Context, key string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("setting %q: %w", key, domain.ErrNotFound)
	}
	var value string
	err := r.db.QueryRowContext(ctx,
		`SELECT value FROM app_settings WHERE key = ?`, key,
	).Scan(&value)
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
//
// Errors other than "not found" are surfaced as a string equal to def
// — the design choice here is that a sql layer hiccup should not
// silently flip a setting; logging happens at the call site so the
// caller knows the read failed and used the default.
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

// Set upserts the value for key. Updating updated_at on every write
// gives the system panel a "last edited" hint without a separate
// audit log — sufficient for self-hosted single-tenant. Writing to
// a nil receiver is a programming error (the admin endpoint should
// have refused the request before reaching here) so we surface a
// clear error rather than silently dropping the write.
func (r *SettingsRepository) Set(ctx context.Context, key, value string) error {
	if r == nil {
		return fmt.Errorf("set setting %q: settings repository not initialised", key)
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO app_settings (key, value, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET
		    value      = excluded.value,
		    updated_at = excluded.updated_at`,
		key, value)
	if err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}

// Delete removes a setting so the next Get falls back to the YAML
// default. Used by the admin "reset to default" affordance — without
// it the operator could only pin a value, never explicitly clear an
// override.
func (r *SettingsRepository) Delete(ctx context.Context, key string) error {
	if r == nil {
		return fmt.Errorf("delete setting %q: settings repository not initialised", key)
	}
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM app_settings WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("delete setting %q: %w", key, err)
	}
	return nil
}

// All returns every stored setting. Used by the admin GET endpoint to
// hydrate the UI on first load — the page wants to show "current
// effective values" with both the override (if any) and the YAML
// default; the handler combines both. Nil receiver returns an empty
// map so a partial wiring (handler without repo) renders defaults.
func (r *SettingsRepository) All(ctx context.Context) (map[string]string, error) {
	if r == nil {
		return map[string]string{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT key, value FROM app_settings`)
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
