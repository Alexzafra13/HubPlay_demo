package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/domain"
)

// SigningKey is the domain shape exposed to internal/auth. Used to
// live as an alias to sqlc.JwtSigningKey; now a proper struct so the
// dual-dialect repo can return the same type regardless of which
// generated package produced the row.
type SigningKey struct {
	ID        string
	Secret    string
	CreatedAt time.Time
	RetiredAt sql.NullTime
}

// SigningKeyRepository — Pattern A dual-dialect. The rotation policy
// + caching lives in internal/auth/keystore.go; this file only
// adapts the sqlc-generated queries to the narrow keystore interface.
type SigningKeyRepository struct {
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewSigningKeyRepository(driver string, database *sql.DB) *SigningKeyRepository {
	r := &SigningKeyRepository{}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *SigningKeyRepository) useSQLite() bool { return r.sq != nil }

func (r *SigningKeyRepository) Insert(ctx context.Context, k *SigningKey) error {
	if r.useSQLite() {
		if err := r.sq.CreateSigningKey(ctx, sqlc.CreateSigningKeyParams{
			ID:        k.ID,
			Secret:    k.Secret,
			CreatedAt: k.CreatedAt,
			RetiredAt: k.RetiredAt,
		}); err != nil {
			return fmt.Errorf("insert signing key: %w", err)
		}
		return nil
	}
	if err := r.pq.CreateSigningKey(ctx, sqlc_pg.CreateSigningKeyParams{
		ID:        k.ID,
		Secret:    k.Secret,
		CreatedAt: k.CreatedAt,
		RetiredAt: k.RetiredAt,
	}); err != nil {
		return fmt.Errorf("insert signing key: %w", err)
	}
	return nil
}

func (r *SigningKeyRepository) GetByID(ctx context.Context, id string) (*SigningKey, error) {
	if r.useSQLite() {
		row, err := r.sq.GetSigningKey(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("signing key %s: %w", id, domain.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get signing key: %w", err)
		}
		k := signingKeyFromSqlite(row)
		return &k, nil
	}
	row, err := r.pq.GetSigningKey(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("signing key %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get signing key: %w", err)
	}
	k := signingKeyFromPg(row)
	return &k, nil
}

func (r *SigningKeyRepository) ListActive(ctx context.Context) ([]*SigningKey, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListActiveSigningKeys(ctx)
		if err != nil {
			return nil, fmt.Errorf("list active signing keys: %w", err)
		}
		return mapSigningKeysFromSqlite(rows), nil
	}
	rows, err := r.pq.ListActiveSigningKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active signing keys: %w", err)
	}
	return mapSigningKeysFromPg(rows), nil
}

func (r *SigningKeyRepository) ListAll(ctx context.Context) ([]*SigningKey, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListSigningKeys(ctx)
		if err != nil {
			return nil, fmt.Errorf("list signing keys: %w", err)
		}
		return mapSigningKeysFromSqlite(rows), nil
	}
	rows, err := r.pq.ListSigningKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("list signing keys: %w", err)
	}
	return mapSigningKeysFromPg(rows), nil
}

func (r *SigningKeyRepository) SetRetiredAt(ctx context.Context, id string, retiredAt time.Time) error {
	ra := sql.NullTime{}
	if !retiredAt.IsZero() {
		ra = sql.NullTime{Time: retiredAt, Valid: true}
	}
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.SetSigningKeyRetiredAt(ctx, sqlc.SetSigningKeyRetiredAtParams{
			RetiredAt: ra, ID: id,
		})
	} else {
		n, err = r.pq.SetSigningKeyRetiredAt(ctx, sqlc_pg.SetSigningKeyRetiredAtParams{
			RetiredAt: ra, ID: id,
		})
	}
	if err != nil {
		return fmt.Errorf("retire signing key: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("signing key %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

func (r *SigningKeyRepository) DeleteRetiredBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	cut := sql.NullTime{Time: cutoff, Valid: true}
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.DeleteRetiredSigningKeysBefore(ctx, cut)
	} else {
		n, err = r.pq.DeleteRetiredSigningKeysBefore(ctx, cut)
	}
	if err != nil {
		return 0, fmt.Errorf("prune signing keys: %w", err)
	}
	return n, nil
}

// ── row mapping helpers ─────────────────────────────────────────────────

func signingKeyFromSqlite(r sqlc.JwtSigningKey) SigningKey {
	return SigningKey{ID: r.ID, Secret: r.Secret, CreatedAt: r.CreatedAt, RetiredAt: r.RetiredAt}
}

func signingKeyFromPg(r sqlc_pg.JwtSigningKey) SigningKey {
	return SigningKey{ID: r.ID, Secret: r.Secret, CreatedAt: r.CreatedAt, RetiredAt: r.RetiredAt}
}

func mapSigningKeysFromSqlite(rows []sqlc.JwtSigningKey) []*SigningKey {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*SigningKey, len(rows))
	for i, row := range rows {
		k := signingKeyFromSqlite(row)
		out[i] = &k
	}
	return out
}

func mapSigningKeysFromPg(rows []sqlc_pg.JwtSigningKey) []*SigningKey {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*SigningKey, len(rows))
	for i, row := range rows {
		k := signingKeyFromPg(row)
		out[i] = &k
	}
	return out
}
