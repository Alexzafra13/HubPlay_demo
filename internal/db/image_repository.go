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
	db *sql.DB // kept for GetPrimaryURLs (dynamic IN) and SetPrimary (tx)
	q  *sqlc.Queries
}

func NewImageRepository(database *sql.DB) *ImageRepository {
	return &ImageRepository{db: database, q: sqlc.New(database)}
}

func (r *ImageRepository) Create(ctx context.Context, img *Image) error {
	err := r.q.CreateImage(ctx, sqlc.CreateImageParams{
		ID:        img.ID,
		ItemID:    img.ItemID,
		Type:      img.Type,
		Path:      img.Path,
		Width:     nullableInt64(int64(img.Width)),
		Height:    nullableInt64(int64(img.Height)),
		Blurhash:  nullableString(img.Blurhash),
		Provider:  nullableString(img.Provider),
		IsPrimary: sql.NullBool{Bool: img.IsPrimary, Valid: true},
		AddedAt:   img.AddedAt,
	})
	if err != nil {
		return fmt.Errorf("create image: %w", err)
	}
	return nil
}

func (r *ImageRepository) ListByItem(ctx context.Context, itemID string) ([]*Image, error) {
	rows, err := r.q.ListImagesByItem(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	return imagesFromListRows(rows), nil
}

func (r *ImageRepository) GetPrimary(ctx context.Context, itemID, imgType string) (*Image, error) {
	row, err := r.q.GetPrimaryImage(ctx, sqlc.GetPrimaryImageParams{
		ItemID: itemID,
		Type:   imgType,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("image for %s/%s: %w", itemID, imgType, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get primary image: %w", err)
	}
	img := imageFromPrimaryRow(row)
	return &img, nil
}

// GetPrimaryURLs returns poster and backdrop URLs for a batch of item IDs.
// Uses raw SQL because sqlc doesn't support dynamic IN() on SQLite.
func (r *ImageRepository) GetPrimaryURLs(ctx context.Context, itemIDs []string) (map[string]map[string]string, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(itemIDs))
	args := make([]any, len(itemIDs))
	for i, id := range itemIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT item_id, type, path FROM images
		 WHERE item_id IN (%s) AND is_primary = 1 AND type IN ('primary', 'backdrop', 'logo')
		 ORDER BY item_id, type`,
		joinStrings(placeholders, ","),
	)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get primary urls: %w", err)
	}
	defer rows.Close()

	result := make(map[string]map[string]string)
	for rows.Next() {
		var itemID, imgType, path string
		if err := rows.Scan(&itemID, &imgType, &path); err != nil {
			return nil, fmt.Errorf("scan primary url: %w", err)
		}
		if result[itemID] == nil {
			result[itemID] = make(map[string]string)
		}
		result[itemID][imgType] = path
	}
	return result, rows.Err()
}

func joinStrings(s []string, sep string) string {
	result := ""
	for i, v := range s {
		if i > 0 {
			result += sep
		}
		result += v
	}
	return result
}

func (r *ImageRepository) DeleteByItem(ctx context.Context, itemID string) error {
	err := r.q.DeleteImagesByItem(ctx, itemID)
	if err != nil {
		return fmt.Errorf("delete images for item: %w", err)
	}
	return nil
}

func (r *ImageRepository) DeleteByID(ctx context.Context, id string) error {
	err := r.q.DeleteImageByID(ctx, id)
	if err != nil {
		return fmt.Errorf("delete image: %w", err)
	}
	return nil
}

func (r *ImageRepository) GetByID(ctx context.Context, id string) (*Image, error) {
	row, err := r.q.GetImageByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("image %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get image: %w", err)
	}
	img := imageFromGetRow(row)
	return &img, nil
}

func (r *ImageRepository) SetPrimary(ctx context.Context, itemID, imgType, imageID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	qtx := r.q.WithTx(tx)

	if err := qtx.UnsetPrimaryImages(ctx, sqlc.UnsetPrimaryImagesParams{
		ItemID: itemID,
		Type:   imgType,
	}); err != nil {
		return fmt.Errorf("unset primary: %w", err)
	}

	if err := qtx.SetImagePrimary(ctx, sqlc.SetImagePrimaryParams{
		ID:     imageID,
		ItemID: itemID,
		Type:   imgType,
	}); err != nil {
		return fmt.Errorf("set primary: %w", err)
	}

	return tx.Commit()
}

// ── row mapping helpers ─────────────────────────────────────────────────

func imageFromGetRow(r sqlc.GetImageByIDRow) Image {
	return Image{
		ID:        r.ID,
		ItemID:    r.ItemID,
		Type:      r.Type,
		Path:      r.Path,
		Width:     int(r.Width),
		Height:    int(r.Height),
		Blurhash:  r.Blurhash,
		Provider:  r.Provider,
		IsPrimary: r.IsPrimary.Bool,
		AddedAt:   r.AddedAt,
	}
}

func imageFromPrimaryRow(r sqlc.GetPrimaryImageRow) Image {
	return Image{
		ID:        r.ID,
		ItemID:    r.ItemID,
		Type:      r.Type,
		Path:      r.Path,
		Width:     int(r.Width),
		Height:    int(r.Height),
		Blurhash:  r.Blurhash,
		Provider:  r.Provider,
		IsPrimary: r.IsPrimary.Bool,
		AddedAt:   r.AddedAt,
	}
}

func imageFromListRow(r sqlc.ListImagesByItemRow) Image {
	return Image{
		ID:        r.ID,
		ItemID:    r.ItemID,
		Type:      r.Type,
		Path:      r.Path,
		Width:     int(r.Width),
		Height:    int(r.Height),
		Blurhash:  r.Blurhash,
		Provider:  r.Provider,
		IsPrimary: r.IsPrimary.Bool,
		AddedAt:   r.AddedAt,
	}
}

func imagesFromListRows(rows []sqlc.ListImagesByItemRow) []*Image {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*Image, len(rows))
	for i, row := range rows {
		img := imageFromListRow(row)
		out[i] = &img
	}
	return out
}
