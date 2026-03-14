package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"hubplay/internal/domain"
)

type Image struct {
	ID        string
	ItemID    string
	Type      string // primary, backdrop, thumb, logo, banner
	Path      string
	Width     int
	Height    int
	Blurhash  string
	Provider  string
	IsPrimary bool
	AddedAt   time.Time
}

type ImageRepository struct {
	db *sql.DB
}

func NewImageRepository(database *sql.DB) *ImageRepository {
	return &ImageRepository{db: database}
}

func (r *ImageRepository) Create(ctx context.Context, img *Image) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO images (id, item_id, type, path, width, height, blurhash, provider, is_primary, added_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		img.ID, img.ItemID, img.Type, img.Path, img.Width, img.Height,
		img.Blurhash, img.Provider, img.IsPrimary, img.AddedAt,
	)
	if err != nil {
		return fmt.Errorf("create image: %w", err)
	}
	return nil
}

func (r *ImageRepository) ListByItem(ctx context.Context, itemID string) ([]*Image, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, item_id, type, path, COALESCE(width,0), COALESCE(height,0),
		        COALESCE(blurhash,''), COALESCE(provider,''), is_primary, added_at
		 FROM images WHERE item_id = ? ORDER BY is_primary DESC, type`, itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var images []*Image
	for rows.Next() {
		img := &Image{}
		if err := rows.Scan(&img.ID, &img.ItemID, &img.Type, &img.Path, &img.Width,
			&img.Height, &img.Blurhash, &img.Provider, &img.IsPrimary, &img.AddedAt); err != nil {
			return nil, fmt.Errorf("scan image: %w", err)
		}
		images = append(images, img)
	}
	return images, rows.Err()
}

func (r *ImageRepository) GetPrimary(ctx context.Context, itemID, imgType string) (*Image, error) {
	img := &Image{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, item_id, type, path, COALESCE(width,0), COALESCE(height,0),
		        COALESCE(blurhash,''), COALESCE(provider,''), is_primary, added_at
		 FROM images WHERE item_id = ? AND type = ? AND is_primary = 1`, itemID, imgType,
	).Scan(&img.ID, &img.ItemID, &img.Type, &img.Path, &img.Width,
		&img.Height, &img.Blurhash, &img.Provider, &img.IsPrimary, &img.AddedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("image for %s/%s: %w", itemID, imgType, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get primary image: %w", err)
	}
	return img, nil
}

func (r *ImageRepository) DeleteByItem(ctx context.Context, itemID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM images WHERE item_id = ?`, itemID)
	if err != nil {
		return fmt.Errorf("delete images for item: %w", err)
	}
	return nil
}
