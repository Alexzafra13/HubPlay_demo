package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	iptvmodel "hubplay/internal/iptv/model"
)

// ChannelLogoOverrideRepository envuelve `channel_logo_overrides`
// dual-dialect en Pattern B (raw SQL con RewritePlaceholders), mismo
// patrón que LibraryChannelOrderRepository. Cuatro queries simples —
// no merece la pena pasar por sqlc.
//
// Invariante: cada row tiene `logo_url <> '' OR logo_file <> ''` (la
// migración lo hace CHECK constraint). El "clear" se modela como DELETE
// de la row, no como UPDATE a empties.
type ChannelLogoOverrideRepository struct {
	db     *sql.DB
	driver string
}

func NewChannelLogoOverrideRepository(driver string, database *sql.DB) *ChannelLogoOverrideRepository {
	return &ChannelLogoOverrideRepository{db: database, driver: driver}
}

// UpsertURL guarda un override de URL externa, limpiando logo_file si
// había uno previo (un override es URL O archivo, nunca ambos).
func (r *ChannelLogoOverrideRepository) UpsertURL(ctx context.Context, libraryID, streamURL, logoURL string) error {
	if logoURL == "" {
		return fmt.Errorf("channel_logo_overrides: logo_url required for URL override")
	}
	now := time.Now().UTC()
	query := RewritePlaceholders(r.driver, `
		INSERT INTO channel_logo_overrides (library_id, stream_url, logo_url, logo_file, created_at, updated_at)
		VALUES (?, ?, ?, '', ?, ?)
		ON CONFLICT(library_id, stream_url) DO UPDATE SET
			logo_url   = excluded.logo_url,
			logo_file  = '',
			updated_at = excluded.updated_at`)
	if _, err := r.db.ExecContext(ctx, query, libraryID, streamURL, logoURL, now, now); err != nil {
		return fmt.Errorf("upsert channel_logo_overrides (url): %w", err)
	}
	return nil
}

// UpsertFile guarda un override de archivo subido, limpiando logo_url
// si había una previa. logoFile es el basename (sin path) — la
// resolución a path absoluto la hace el handler con imageDir.
func (r *ChannelLogoOverrideRepository) UpsertFile(ctx context.Context, libraryID, streamURL, logoFile string) error {
	if logoFile == "" {
		return fmt.Errorf("channel_logo_overrides: logo_file required for file override")
	}
	now := time.Now().UTC()
	query := RewritePlaceholders(r.driver, `
		INSERT INTO channel_logo_overrides (library_id, stream_url, logo_url, logo_file, created_at, updated_at)
		VALUES (?, ?, '', ?, ?, ?)
		ON CONFLICT(library_id, stream_url) DO UPDATE SET
			logo_url   = '',
			logo_file  = excluded.logo_file,
			updated_at = excluded.updated_at`)
	if _, err := r.db.ExecContext(ctx, query, libraryID, streamURL, logoFile, now, now); err != nil {
		return fmt.Errorf("upsert channel_logo_overrides (file): %w", err)
	}
	return nil
}

// Delete borra el override por su PK. Idempotente — no error si no
// existía. Cuando se borra una row con logo_file, el handler es
// responsable de borrar también el archivo en disco (no podemos hacerlo
// aquí porque no conocemos imageDir).
func (r *ChannelLogoOverrideRepository) Delete(ctx context.Context, libraryID, streamURL string) error {
	query := RewritePlaceholders(r.driver,
		`DELETE FROM channel_logo_overrides WHERE library_id = ? AND stream_url = ?`)
	if _, err := r.db.ExecContext(ctx, query, libraryID, streamURL); err != nil {
		return fmt.Errorf("delete channel_logo_overrides: %w", err)
	}
	return nil
}

// Get devuelve un override si existe. (nil, nil) cuando no hay row —
// llamantes hacen pattern-match sin sniff de sql.ErrNoRows.
func (r *ChannelLogoOverrideRepository) Get(ctx context.Context, libraryID, streamURL string) (*iptvmodel.ChannelLogoOverride, error) {
	query := RewritePlaceholders(r.driver, `
		SELECT library_id, stream_url, logo_url, logo_file, created_at, updated_at
		FROM channel_logo_overrides
		WHERE library_id = ? AND stream_url = ?`)
	row := r.db.QueryRowContext(ctx, query, libraryID, streamURL)
	var e iptvmodel.ChannelLogoOverride
	if err := row.Scan(&e.LibraryID, &e.StreamURL, &e.LogoURL, &e.LogoFile, &e.CreatedAt, &e.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get channel_logo_overrides: %w", err)
	}
	return &e, nil
}

// ListByLibrary devuelve todos los overrides de una biblioteca. El
// servicio de canales lo carga en bulk para aplicar el overlay sin un
// query por canal — patrón ya usado por applyAdminOverlay con el order.
func (r *ChannelLogoOverrideRepository) ListByLibrary(ctx context.Context, libraryID string) ([]iptvmodel.ChannelLogoOverride, error) {
	query := RewritePlaceholders(r.driver, `
		SELECT library_id, stream_url, logo_url, logo_file, created_at, updated_at
		FROM channel_logo_overrides
		WHERE library_id = ?`)
	rows, err := r.db.QueryContext(ctx, query, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list channel_logo_overrides: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	out := []iptvmodel.ChannelLogoOverride{}
	for rows.Next() {
		var e iptvmodel.ChannelLogoOverride
		if err := rows.Scan(&e.LibraryID, &e.StreamURL, &e.LogoURL, &e.LogoFile, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan channel_logo_overrides: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
