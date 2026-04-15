package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/domain"
)

// SigningKey is an alias for the sqlc-generated row type. The alias keeps the
// name stable for callers in internal/auth/ (keystore, jwt, service) while the
// underlying shape is owned by sqlc from the jwt_signing_keys table defined in
// migrations/sqlite/004_jwt_signing_keys.sql.
type SigningKey = sqlc.JwtSigningKey

// SigningKeyRepository persists JWT signing keys.
//
// The repository is intentionally minimal: the keystore layer owns rotation
// policy (when to retire, how long to overlap) and caches reads. This file
// only adapts the sqlc-generated queries to the narrow interface the keystore
// consumes (see internal/auth/keystore.go:signingKeyRepo).
type SigningKeyRepository struct {
	q *sqlc.Queries
}

func NewSigningKeyRepository(database *sql.DB) *SigningKeyRepository {
	return &SigningKeyRepository{q: sqlc.New(database)}
}

// Insert adds a new signing key. The caller generates the id and secret.
func (r *SigningKeyRepository) Insert(ctx context.Context, k *SigningKey) error {
	err := r.q.CreateSigningKey(ctx, sqlc.CreateSigningKeyParams{
		ID:        k.ID,
		Secret:    k.Secret,
		CreatedAt: k.CreatedAt,
		RetiredAt: k.RetiredAt,
	})
	if err != nil {
		return fmt.Errorf("insert signing key: %w", err)
	}
	return nil
}

// GetByID fetches a single key by id. Returns domain.ErrNotFound for a
// missing kid so handlers can map it to a 401.
func (r *SigningKeyRepository) GetByID(ctx context.Context, id string) (*SigningKey, error) {
	k, err := r.q.GetSigningKey(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("signing key %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get signing key: %w", err)
	}
	return &k, nil
}

// ListActive returns every non-retired key, newest first. The newest is
// treated as the primary signer; any other active key is in its overlap
// window and only validates in-flight tokens.
func (r *SigningKeyRepository) ListActive(ctx context.Context) ([]*SigningKey, error) {
	rows, err := r.q.ListActiveSigningKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active signing keys: %w", err)
	}
	return rowsToPtrs(rows), nil
}

// ListAll returns every key, active and retired, newest first. Used by the
// admin UI and by the pruner to identify retirable keys.
func (r *SigningKeyRepository) ListAll(ctx context.Context) ([]*SigningKey, error) {
	rows, err := r.q.ListSigningKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("list signing keys: %w", err)
	}
	return rowsToPtrs(rows), nil
}

// SetRetiredAt marks a key as retired (or clears it if retiredAt is zero).
// The zero-time branch exists so tests and admins can "unretire" a key
// without recreating it; production typically just passes a concrete time.
func (r *SigningKeyRepository) SetRetiredAt(ctx context.Context, id string, retiredAt time.Time) error {
	ra := sql.NullTime{}
	if !retiredAt.IsZero() {
		ra = sql.NullTime{Time: retiredAt, Valid: true}
	}
	n, err := r.q.SetSigningKeyRetiredAt(ctx, sqlc.SetSigningKeyRetiredAtParams{
		RetiredAt: ra,
		ID:        id,
	})
	if err != nil {
		return fmt.Errorf("retire signing key: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("signing key %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// DeleteRetiredBefore removes every key that was retired before the cutoff.
// Returns how many rows were deleted.
func (r *SigningKeyRepository) DeleteRetiredBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	n, err := r.q.DeleteRetiredSigningKeysBefore(ctx, sql.NullTime{Time: cutoff, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("prune signing keys: %w", err)
	}
	return n, nil
}

// rowsToPtrs adapts sqlc's value slices to the pointer-slice shape the auth
// package consumes. Taking &rows[i] in a loop is safe because rows is the
// local slice owned by this call.
func rowsToPtrs(rows []sqlc.JwtSigningKey) []*SigningKey {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*SigningKey, len(rows))
	for i := range rows {
		out[i] = &rows[i]
	}
	return out
}
