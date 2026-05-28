package db

import (
	"context"
	"database/sql"

	librarymodel "hubplay/internal/library/model"
	"fmt"
)

// UserPreference is a single (user, key) → value tuple.

// UserPreferenceRepository persists per-user UI preferences. Generic
// key/value on purpose — frontend hooks encode whatever shape they
// want in the value column, and the backend doesn't interpret it.
//
// Dual-dialect strategy: raw SQL with `?` placeholders pre-rewritten
// to `$N` at construction time when the postgres driver is selected.
// No sqlc surface — the four queries are trivial enough that a typed
// generated layer would add machinery without gain, same call as
// SettingsRepository (the template Pattern B repo).
type UserPreferenceRepository struct {
	db *sql.DB

	listSQL   string
	upsertSQL string
	deleteSQL string
}

func NewUserPreferenceRepository(driver string, database *sql.DB) *UserPreferenceRepository {
	return &UserPreferenceRepository{
		db: database,
		listSQL: rewritePlaceholders(driver,
			`SELECT user_id, key, value, updated_at
			 FROM user_preferences WHERE user_id = ?`),
		upsertSQL: rewritePlaceholders(driver,
			`INSERT INTO user_preferences (user_id, key, value, updated_at)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(user_id, key) DO UPDATE SET
			    value      = excluded.value,
			    updated_at = excluded.updated_at`),
		deleteSQL: rewritePlaceholders(driver,
			`DELETE FROM user_preferences WHERE user_id = ? AND key = ?`),
	}
}

// ListByUser returns every preference row for one user. Empty slice
// (not nil) when the user has no preferences yet — keeps the JSON
// serialisation on the API side clean.
func (r *UserPreferenceRepository) ListByUser(ctx context.Context, userID string) ([]librarymodel.UserPreference, error) {
	rows, err := r.db.QueryContext(ctx, r.listSQL, userID)
	if err != nil {
		return nil, fmt.Errorf("list preferences: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := make([]librarymodel.UserPreference, 0)
	for rows.Next() {
		var p librarymodel.UserPreference
		var updatedRaw any
		if err := rows.Scan(&p.UserID, &p.Key, &p.Value, &updatedRaw); err != nil {
			return nil, fmt.Errorf("scan preference: %w", err)
		}
		p.UpdatedAt, err = coerceSQLiteTime(updatedRaw)
		if err != nil {
			return nil, fmt.Errorf("parse updated_at: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Set upserts one key. Returns the persisted row so handlers can echo
// it back to the client without an extra read.
func (r *UserPreferenceRepository) Set(ctx context.Context, userID, key, value string) (*librarymodel.UserPreference, error) {
	now := timeNow().UTC()
	if _, err := r.db.ExecContext(ctx, r.upsertSQL, userID, key, value, now); err != nil {
		return nil, fmt.Errorf("set preference: %w", err)
	}
	return &librarymodel.UserPreference{
		UserID: userID, Key: key, Value: value, UpdatedAt: now,
	}, nil
}

// Delete removes one key. Idempotent — no error if the row was
// already absent.
func (r *UserPreferenceRepository) Delete(ctx context.Context, userID, key string) error {
	if _, err := r.db.ExecContext(ctx, r.deleteSQL, userID, key); err != nil {
		return fmt.Errorf("delete preference: %w", err)
	}
	return nil
}
