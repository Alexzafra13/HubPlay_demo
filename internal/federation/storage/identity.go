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

// GetIdentity devuelve la fila singleton o (nil, nil) si no existe.
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
		ServerUUID:      row.ServerUuid,
		Name:            row.Name,
		PrivateKey:      row.PrivateKey,
		PublicKey:       row.PublicKey,
		CreatedAt:       row.CreatedAt,
		AvatarColor:     row.AvatarColor,
		AvatarImagePath: row.AvatarImagePath,
	}
	if row.RotatedAt.Valid {
		t := row.RotatedAt.Time
		id.RotatedAt = &t
	}
	return id
}

func identityFromPgRow(row sqlc_pg.GetServerIdentityRow) *federation.Identity {
	id := &federation.Identity{
		ServerUUID:      row.ServerUuid,
		Name:            row.Name,
		PrivateKey:      row.PrivateKey,
		PublicKey:       row.PublicKey,
		CreatedAt:       row.CreatedAt,
		AvatarColor:     row.AvatarColor,
		AvatarImagePath: row.AvatarImagePath,
	}
	if row.RotatedAt.Valid {
		t := row.RotatedAt.Time
		id.RotatedAt = &t
	}
	return id
}

// InsertIdentity persiste el singleton. Error en segunda llamada.
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

// UpdateIdentityProfile actualiza nombre visible + color hex.
func (r *Repository) UpdateIdentityProfile(ctx context.Context, name, avatarColor string) error {
	if r.useSQLite() {
		if err := r.sq.UpdateServerIdentityProfile(ctx, sqlc.UpdateServerIdentityProfileParams{
			Name:        name,
			AvatarColor: avatarColor,
		}); err != nil {
			return fmt.Errorf("update server identity profile: %w", err)
		}
		return nil
	}
	if err := r.pq.UpdateServerIdentityProfile(ctx, sqlc_pg.UpdateServerIdentityProfileParams{
		Name:        name,
		AvatarColor: avatarColor,
	}); err != nil {
		return fmt.Errorf("update server identity profile: %w", err)
	}
	return nil
}

// SetAvatarPath registra el nombre del fichero del avatar del servidor
// (relativo a avatarsDir). Cadena vacía limpia el campo.
func (r *Repository) SetAvatarPath(ctx context.Context, path string) error {
	if r.useSQLite() {
		if err := r.sq.SetServerAvatarPath(ctx, path); err != nil {
			return fmt.Errorf("set server avatar path: %w", err)
		}
		return nil
	}
	if err := r.pq.SetServerAvatarPath(ctx, path); err != nil {
		return fmt.Errorf("set server avatar path: %w", err)
	}
	return nil
}
