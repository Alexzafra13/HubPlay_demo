package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// UserPreference is a single (user, key) → value tuple.
type UserPreference struct {
	UserID    string
	Key       string
	Value     string
	UpdatedAt time.Time
}

// UserPreferenceRepository persists per-user UI preferences. Generic
// key/value on purpose — frontend hooks encode whatever shape they
// want in the value column, and the backend doesn't interpret it.
type UserPreferenceRepository struct {
	db *sql.DB
}

func NewUserPreferenceRepository(database *sql.DB) *UserPreferenceRepository {
	return &UserPreferenceRepository{db: database}
}

// ListByUser returns every preference row for one user. Empty slice
// (not nil) when the user has no preferences yet — keeps the JSON
// serialisation on the API side clean.
func (r *UserPreferenceRepository) ListByUser(ctx context.Context, userID string) ([]UserPreference, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT user_id, key, value, updated_at
		 FROM user_preferences WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("list preferences: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := make([]UserPreference, 0)
	for rows.Next() {
		var p UserPreference
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
func (r *UserPreferenceRepository) Set(ctx context.Context, userID, key, value string) (*UserPreference, error) {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO user_preferences (user_id, key, value, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(user_id, key) DO UPDATE SET
		    value      = excluded.value,
		    updated_at = excluded.updated_at`,
		userID, key, value, now)
	if err != nil {
		return nil, fmt.Errorf("set preference: %w", err)
	}
	return &UserPreference{
		UserID: userID, Key: key, Value: value, UpdatedAt: now,
	}, nil
}

// Delete removes one key. Idempotent — no error if the row was
// already absent.
func (r *UserPreferenceRepository) Delete(ctx context.Context, userID, key string) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM user_preferences WHERE user_id = ? AND key = ?`,
		userID, key); err != nil {
		return fmt.Errorf("delete preference: %w", err)
	}
	return nil
}
