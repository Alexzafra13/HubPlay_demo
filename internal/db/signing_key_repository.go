package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"hubplay/internal/domain"
)

// SigningKey represents an HMAC key used to sign JWT access tokens. A key is
// "active" while retired_at is NULL; a retired key can still validate old
// tokens until pruned.
type SigningKey struct {
	ID        string
	Secret    string
	CreatedAt time.Time
	RetiredAt sql.NullTime
}

// SigningKeyRepository persists JWT signing keys.
//
// The repository is intentionally minimal: the keystore layer owns rotation
// policy (when to retire, how long to overlap) and caches reads. This file
// only speaks SQL.
type SigningKeyRepository struct {
	db *sql.DB
}

func NewSigningKeyRepository(database *sql.DB) *SigningKeyRepository {
	return &SigningKeyRepository{db: database}
}

// Insert adds a new signing key. The caller generates the id and secret.
func (r *SigningKeyRepository) Insert(ctx context.Context, k *SigningKey) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO jwt_signing_keys (id, secret, created_at, retired_at)
		 VALUES (?, ?, ?, ?)`,
		k.ID, k.Secret, k.CreatedAt, k.RetiredAt,
	)
	if err != nil {
		return fmt.Errorf("insert signing key: %w", err)
	}
	return nil
}

// GetByID fetches a single key by id. Returns domain.ErrNotFound for a
// missing kid so handlers can map it to a 401.
func (r *SigningKeyRepository) GetByID(ctx context.Context, id string) (*SigningKey, error) {
	k := &SigningKey{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, secret, created_at, retired_at FROM jwt_signing_keys WHERE id = ?`, id,
	).Scan(&k.ID, &k.Secret, &k.CreatedAt, &k.RetiredAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("signing key %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get signing key: %w", err)
	}
	return k, nil
}

// ListActive returns every non-retired key, newest first. The newest is
// treated as the primary signer; any other active key is in its overlap
// window and only validates in-flight tokens.
func (r *SigningKeyRepository) ListActive(ctx context.Context) ([]*SigningKey, error) {
	return r.query(ctx,
		`SELECT id, secret, created_at, retired_at FROM jwt_signing_keys
		 WHERE retired_at IS NULL ORDER BY created_at DESC`,
	)
}

// ListAll returns every key, active and retired, newest first. Used by the
// admin UI and by the pruner to identify retirable keys.
func (r *SigningKeyRepository) ListAll(ctx context.Context) ([]*SigningKey, error) {
	return r.query(ctx,
		`SELECT id, secret, created_at, retired_at FROM jwt_signing_keys
		 ORDER BY created_at DESC`,
	)
}

// SetRetiredAt marks a key as retired (or clears it if retiredAt is zero).
// The zero-time branch exists so tests and admins can "unretire" a key
// without recreating it; production typically just passes a concrete time.
func (r *SigningKeyRepository) SetRetiredAt(ctx context.Context, id string, retiredAt time.Time) error {
	var arg any
	if retiredAt.IsZero() {
		arg = nil
	} else {
		arg = retiredAt
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE jwt_signing_keys SET retired_at = ? WHERE id = ?`, arg, id,
	)
	if err != nil {
		return fmt.Errorf("retire signing key: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("signing key %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// DeleteRetiredBefore removes every key that was retired before the cutoff.
// Returns how many rows were deleted.
func (r *SigningKeyRepository) DeleteRetiredBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM jwt_signing_keys WHERE retired_at IS NOT NULL AND retired_at < ?`, cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("prune signing keys: %w", err)
	}
	return res.RowsAffected()
}

func (r *SigningKeyRepository) query(ctx context.Context, q string, args ...any) ([]*SigningKey, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list signing keys: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []*SigningKey
	for rows.Next() {
		k := &SigningKey{}
		if err := rows.Scan(&k.ID, &k.Secret, &k.CreatedAt, &k.RetiredAt); err != nil {
			return nil, fmt.Errorf("scan signing key: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
