package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/federation"
)

// GetIdentity returns the singleton row, or (nil, nil) if none yet.
// nil-without-error is the contract the IdentityStore expects so it
// can decide whether to bootstrap a fresh keypair.
func (r *Repository) GetIdentity(ctx context.Context) (*federation.Identity, error) {
	if r.useSQLite() {
		row, err := r.sq.GetServerIdentity(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get server identity: %w", err)
		}
		return identityFromSqliteRow(row), nil
	}
	row, err := r.pq.GetServerIdentity(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get server identity: %w", err)
	}
	return identityFromPgRow(row), nil
}

func identityFromSqliteRow(row sqlc.GetServerIdentityRow) *federation.Identity {
	id := &federation.Identity{
		ServerUUID: row.ServerUuid,
		Name:       row.Name,
		PrivateKey: row.PrivateKey,
		PublicKey:  row.PublicKey,
		CreatedAt:  row.CreatedAt,
	}
	if row.RotatedAt.Valid {
		t := row.RotatedAt.Time
		id.RotatedAt = &t
	}
	return id
}

func identityFromPgRow(row sqlc_pg.GetServerIdentityRow) *federation.Identity {
	id := &federation.Identity{
		ServerUUID: row.ServerUuid,
		Name:       row.Name,
		PrivateKey: row.PrivateKey,
		PublicKey:  row.PublicKey,
		CreatedAt:  row.CreatedAt,
	}
	if row.RotatedAt.Valid {
		t := row.RotatedAt.Time
		id.RotatedAt = &t
	}
	return id
}

// InsertIdentity persists the singleton. Idempotency guard: errors
// on a second call (CHECK(id=1) + UNIQUE on server_uuid).
func (r *Repository) InsertIdentity(ctx context.Context, id *federation.Identity) error {
	var err error
	if r.useSQLite() {
		err = r.sq.InsertServerIdentity(ctx, sqlc.InsertServerIdentityParams{
			ServerUuid: id.ServerUUID,
			Name:       id.Name,
			PrivateKey: []byte(id.PrivateKey),
			PublicKey:  []byte(id.PublicKey),
			CreatedAt:  id.CreatedAt,
		})
	} else {
		err = r.pq.InsertServerIdentity(ctx, sqlc_pg.InsertServerIdentityParams{
			ServerUuid: id.ServerUUID,
			Name:       id.Name,
			PrivateKey: []byte(id.PrivateKey),
			PublicKey:  []byte(id.PublicKey),
			CreatedAt:  id.CreatedAt,
		})
	}
	if err != nil {
		return fmt.Errorf("insert server identity: %w", err)
	}
	return nil
}
