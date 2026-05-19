package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	librarymodel "hubplay/internal/library/model"
)

// CollectionImageOverrideRepository envuelve `collection_image_overrides`
// dual-dialect en Pattern B (raw SQL). Cuatro queries simples — no
// merece la pena pasar por sqlc.
//
// Invariante: cada row tiene `url <> '' OR file <> ''` (CHECK en la
// migration). "Clear" se modela como DELETE de la row, no UPDATE a
// empties — los handlers DELETE la row entera al limpiar.
type CollectionImageOverrideRepository struct {
	db     *sql.DB
	driver string
}

func NewCollectionImageOverrideRepository(driver string, database *sql.DB) *CollectionImageOverrideRepository {
	return &CollectionImageOverrideRepository{db: database, driver: driver}
}

// UpsertURL guarda un override de URL externa, limpiando file si había
// uno previo (override es URL O archivo, nunca ambos).
func (r *CollectionImageOverrideRepository) UpsertURL(ctx context.Context, collectionID, imageType, imageURL string) error {
	if imageURL == "" {
		return fmt.Errorf("collection_image_overrides: url required")
	}
	now := time.Now().UTC()
	query := RewritePlaceholders(r.driver, `
		INSERT INTO collection_image_overrides (collection_id, image_type, url, file, created_at, updated_at)
		VALUES (?, ?, ?, '', ?, ?)
		ON CONFLICT(collection_id, image_type) DO UPDATE SET
			url        = excluded.url,
			file       = '',
			updated_at = excluded.updated_at`)
	if _, err := r.db.ExecContext(ctx, query, collectionID, imageType, imageURL, now, now); err != nil {
		return fmt.Errorf("upsert collection_image_overrides (url): %w", err)
	}
	return nil
}

// UpsertFile guarda un override de archivo subido. basename es sólo el
// nombre del fichero (sin path) — la resolución a path absoluto la
// hace el handler con imageDir.
func (r *CollectionImageOverrideRepository) UpsertFile(ctx context.Context, collectionID, imageType, basename string) error {
	if basename == "" {
		return fmt.Errorf("collection_image_overrides: file required")
	}
	now := time.Now().UTC()
	query := RewritePlaceholders(r.driver, `
		INSERT INTO collection_image_overrides (collection_id, image_type, url, file, created_at, updated_at)
		VALUES (?, ?, '', ?, ?, ?)
		ON CONFLICT(collection_id, image_type) DO UPDATE SET
			url        = '',
			file       = excluded.file,
			updated_at = excluded.updated_at`)
	if _, err := r.db.ExecContext(ctx, query, collectionID, imageType, basename, now, now); err != nil {
		return fmt.Errorf("upsert collection_image_overrides (file): %w", err)
	}
	return nil
}

// Delete borra el override (un image_type concreto) por su PK.
// Idempotente. Si la row tenía un file, el handler es responsable de
// borrar el archivo en disco.
func (r *CollectionImageOverrideRepository) Delete(ctx context.Context, collectionID, imageType string) error {
	query := RewritePlaceholders(r.driver,
		`DELETE FROM collection_image_overrides WHERE collection_id = ? AND image_type = ?`)
	if _, err := r.db.ExecContext(ctx, query, collectionID, imageType); err != nil {
		return fmt.Errorf("delete collection_image_overrides: %w", err)
	}
	return nil
}

// Get devuelve un override concreto. (nil, nil) cuando no hay row.
func (r *CollectionImageOverrideRepository) Get(ctx context.Context, collectionID, imageType string) (*librarymodel.CollectionImageOverride, error) {
	query := RewritePlaceholders(r.driver, `
		SELECT collection_id, image_type, url, file, created_at, updated_at
		FROM collection_image_overrides
		WHERE collection_id = ? AND image_type = ?`)
	row := r.db.QueryRowContext(ctx, query, collectionID, imageType)
	var e librarymodel.CollectionImageOverride
	if err := row.Scan(&e.CollectionID, &e.ImageType, &e.URL, &e.File, &e.CreatedAt, &e.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get collection_image_overrides: %w", err)
	}
	return &e, nil
}

// ListByCollection devuelve los dos overrides (poster y/o backdrop) de
// una saga. El handler GET /collections/{id} lo llama una vez por
// petición — coste fijo de 1 query, ~2 rows máx por colección.
func (r *CollectionImageOverrideRepository) ListByCollection(ctx context.Context, collectionID string) ([]librarymodel.CollectionImageOverride, error) {
	query := RewritePlaceholders(r.driver, `
		SELECT collection_id, image_type, url, file, created_at, updated_at
		FROM collection_image_overrides
		WHERE collection_id = ?`)
	rows, err := r.db.QueryContext(ctx, query, collectionID)
	if err != nil {
		return nil, fmt.Errorf("list collection_image_overrides: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	out := []librarymodel.CollectionImageOverride{}
	for rows.Next() {
		var e librarymodel.CollectionImageOverride
		if err := rows.Scan(&e.CollectionID, &e.ImageType, &e.URL, &e.File, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan collection_image_overrides: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
