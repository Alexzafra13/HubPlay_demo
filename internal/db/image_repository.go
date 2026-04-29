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
	// IsLocked guards a manual choice (admin uploaded a custom poster
	// or picked a specific candidate) from being overwritten by a
	// scheduled or scanner-triggered refresh. Plex and Jellyfin both
	// expose this as "lock". Default is false — refreshes work as
	// before until the admin explicitly locks something.
	IsLocked bool
	AddedAt  time.Time
	// DominantColor / DominantColorMuted are pre-computed CSS rgb()
	// strings extracted at ingest time. Empty when extraction failed
	// (non-decodable formats, undecidable palette) — clients fall back
	// to runtime extraction or a static colour in that case.
	DominantColor      string
	DominantColorMuted string
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
		ID:                 img.ID,
		ItemID:             img.ItemID,
		Type:               img.Type,
		Path:               img.Path,
		Width:              nullableInt64(int64(img.Width)),
		Height:             nullableInt64(int64(img.Height)),
		Blurhash:           nullableString(img.Blurhash),
		Provider:           nullableString(img.Provider),
		IsPrimary:          sql.NullBool{Bool: img.IsPrimary, Valid: true},
		IsLocked:           sql.NullBool{Bool: img.IsLocked, Valid: true},
		AddedAt:            img.AddedAt,
		DominantColor:      img.DominantColor,
		DominantColorMuted: img.DominantColorMuted,
	})
	if err != nil {
		return fmt.Errorf("create image: %w", err)
	}
	return nil
}

// SetLocked toggles the manual-override lock on an image. Locked
// images are skipped by ImageRefresher (per kind, not per row — the
// idea is "this poster stays, don't try to download a new one") so
// admins can curate their library without the next refresh
// clobbering their picks.
func (r *ImageRepository) SetLocked(ctx context.Context, imageID string, locked bool) error {
	if err := r.q.SetImageLocked(ctx, sqlc.SetImageLockedParams{
		ID:       imageID,
		IsLocked: sql.NullBool{Bool: locked, Valid: true},
	}); err != nil {
		return fmt.Errorf("set image locked: %w", err)
	}
	return nil
}

// HasLockedForKind reports whether the (item, kind) pair has any
// locked image. The refresher consults this before downloading a
// new candidate — a single lock on (item, "primary") prevents the
// poster from being touched, but other kinds (backdrop, logo) can
// still refresh freely.
func (r *ImageRepository) HasLockedForKind(ctx context.Context, itemID, kind string) (bool, error) {
	has, err := r.q.HasLockedImageForKind(ctx, sqlc.HasLockedImageForKindParams{
		ItemID: itemID,
		Type:   kind,
	})
	if err != nil {
		return false, fmt.Errorf("has locked for kind: %w", err)
	}
	return has, nil
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

// PrimaryImageRef is the per-(item,type) payload returned by
// GetPrimaryURLs. Path is always set; the rest are best-effort fields
// populated at ingest time and may be empty for older rows or formats
// the extractor couldn't classify. Clients use them as cheap loading
// placeholders (solid colour fill, blurhash decode) before the real
// image arrives.
type PrimaryImageRef struct {
	Path               string
	Blurhash           string
	DominantColor      string
	DominantColorMuted string
}

// GetPrimaryURLs returns the primary poster/backdrop/logo refs for a
// batch of item IDs. Uses raw SQL because sqlc doesn't support
// dynamic IN() on SQLite.
func (r *ImageRepository) GetPrimaryURLs(ctx context.Context, itemIDs []string) (map[string]map[string]PrimaryImageRef, error) {
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
		`SELECT item_id, type, path, blurhash, dominant_color, dominant_color_muted FROM images
		 WHERE item_id IN (%s) AND is_primary = 1 AND type IN ('primary', 'backdrop', 'logo')
		 ORDER BY item_id, type`,
		joinStrings(placeholders, ","),
	)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get primary urls: %w", err)
	}
	defer rows.Close()

	result := make(map[string]map[string]PrimaryImageRef)
	for rows.Next() {
		var itemID, imgType, path, dominant, dominantMuted string
		var blurhash sql.NullString
		if err := rows.Scan(&itemID, &imgType, &path, &blurhash, &dominant, &dominantMuted); err != nil {
			return nil, fmt.Errorf("scan primary url: %w", err)
		}
		if result[itemID] == nil {
			result[itemID] = make(map[string]PrimaryImageRef)
		}
		result[itemID][imgType] = PrimaryImageRef{
			Path:               path,
			Blurhash:           blurhash.String,
			DominantColor:      dominant,
			DominantColorMuted: dominantMuted,
		}
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
		ID:                 r.ID,
		ItemID:             r.ItemID,
		Type:               r.Type,
		Path:               r.Path,
		Width:              int(r.Width),
		Height:             int(r.Height),
		Blurhash:           r.Blurhash,
		Provider:           r.Provider,
		IsPrimary:          r.IsPrimary.Bool,
		IsLocked:           r.IsLocked.Bool,
		AddedAt:            r.AddedAt,
		DominantColor:      r.DominantColor,
		DominantColorMuted: r.DominantColorMuted,
	}
}

func imageFromPrimaryRow(r sqlc.GetPrimaryImageRow) Image {
	return Image{
		ID:                 r.ID,
		ItemID:             r.ItemID,
		Type:               r.Type,
		Path:               r.Path,
		Width:              int(r.Width),
		Height:             int(r.Height),
		Blurhash:           r.Blurhash,
		Provider:           r.Provider,
		IsPrimary:          r.IsPrimary.Bool,
		IsLocked:           r.IsLocked.Bool,
		AddedAt:            r.AddedAt,
		DominantColor:      r.DominantColor,
		DominantColorMuted: r.DominantColorMuted,
	}
}

func imageFromListRow(r sqlc.ListImagesByItemRow) Image {
	return Image{
		ID:                 r.ID,
		ItemID:             r.ItemID,
		Type:               r.Type,
		Path:               r.Path,
		Width:              int(r.Width),
		Height:             int(r.Height),
		Blurhash:           r.Blurhash,
		Provider:           r.Provider,
		IsPrimary:          r.IsPrimary.Bool,
		IsLocked:           r.IsLocked.Bool,
		AddedAt:            r.AddedAt,
		DominantColor:      r.DominantColor,
		DominantColorMuted: r.DominantColorMuted,
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
