package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ItemMetadataLockRepository envuelve `item_metadata_locks`
// dual-dialect (Pattern B, raw SQL). La tabla tiene una sola columna
// efectiva (item_id PK), así que el repo es pequeño: 4 métodos. La
// presencia de una fila es la señal — no hay payload.
type ItemMetadataLockRepository struct {
	db     *sql.DB
	driver string
}

func NewItemMetadataLockRepository(driver string, database *sql.DB) *ItemMetadataLockRepository {
	return &ItemMetadataLockRepository{db: database, driver: driver}
}

// Lock marca un item como bloqueado contra refreshes automáticos. El
// "Identify" del admin y el editor manual de metadatos llaman aquí
// tras escribir. Idempotente (ON CONFLICT DO NOTHING) — re-lockear
// no es error.
func (r *ItemMetadataLockRepository) Lock(ctx context.Context, itemID string) error {
	query := RewritePlaceholders(r.driver, `
		INSERT INTO item_metadata_locks (item_id) VALUES (?)
		ON CONFLICT(item_id) DO NOTHING`)
	if _, err := r.db.ExecContext(ctx, query, itemID); err != nil {
		return fmt.Errorf("lock item metadata: %w", err)
	}
	return nil
}

// Unlock retira el lock. Idempotente — desbloquear lo no-bloqueado es
// no-op, no error.
func (r *ItemMetadataLockRepository) Unlock(ctx context.Context, itemID string) error {
	query := RewritePlaceholders(r.driver,
		`DELETE FROM item_metadata_locks WHERE item_id = ?`)
	if _, err := r.db.ExecContext(ctx, query, itemID); err != nil {
		return fmt.Errorf("unlock item metadata: %w", err)
	}
	return nil
}

// IsLocked es la consulta caliente: la llama el scanner en
// enrichMetadata para decidir si saltarse el item. Devuelve (false, nil)
// cuando no hay fila — no es error.
func (r *ItemMetadataLockRepository) IsLocked(ctx context.Context, itemID string) (bool, error) {
	query := RewritePlaceholders(r.driver,
		`SELECT 1 FROM item_metadata_locks WHERE item_id = ? LIMIT 1`)
	var one int
	err := r.db.QueryRowContext(ctx, query, itemID).Scan(&one)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("check item metadata lock: %w", err)
	}
	return true, nil
}
