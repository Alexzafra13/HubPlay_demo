package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/domain"
)

// ImageRepository — dual-dialect (Pattern A + Pattern B). The sqlc
// surface (Create / SetLocked / HasLocked / List / GetPrimary /
// GetByID / Delete / SetPrimary tx) branches per-call; GetPrimaryURLs
// stays raw SQL via `r.db` + rewritePlaceholders for the dynamic IN.
//
// BOOLEAN gotcha: GetPrimaryURLs used `is_primary = 1` — rewritten to
// `is_primary` (truthy in both dialects).
type ImageRepository struct {
	db *sql.DB
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewImageRepository(driver string, database *sql.DB) *ImageRepository {
	r := &ImageRepository{db: database}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *ImageRepository) useSQLite() bool { return r.sq != nil }

func (r *ImageRepository) driver() string {
	if r.useSQLite() {
		return DriverSQLite
	}
	return DriverPostgres
}

func (r *ImageRepository) Create(ctx context.Context, img *librarymodel.Image) error {
	if r.useSQLite() {
		err := r.sq.CreateImage(ctx, sqlc.CreateImageParams{
			ID:                 img.ID,
			ItemID:             img.ItemID,
			Type:               img.Type,
			Path:               img.Path,
			Width:              nullableInt64(int64(img.Width)),
			Height:             nullableInt64(int64(img.Height)),
			Blurhash:           nullableString(img.Blurhash),
			Provider:           nullableString(img.Provider),
			IsPrimary:          sql.NullBool{Bool: img.IsPrimary, Valid: true},
			IsLocked:           img.IsLocked,
			AddedAt:            img.AddedAt,
			DominantColor:      img.DominantColor,
			DominantColorMuted: img.DominantColorMuted,
		})
		if err != nil {
			return fmt.Errorf("create image: %w", err)
		}
		return nil
	}
	err := r.pq.CreateImage(ctx, sqlc_pg.CreateImageParams{
		ID:                 img.ID,
		ItemID:             img.ItemID,
		Type:               img.Type,
		Path:               img.Path,
		Width:              nullableInt32(int32(img.Width)),
		Height:             nullableInt32(int32(img.Height)),
		Blurhash:           nullableString(img.Blurhash),
		Provider:           nullableString(img.Provider),
		IsPrimary:          sql.NullBool{Bool: img.IsPrimary, Valid: true},
		IsLocked:           img.IsLocked,
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
	var err error
	if r.useSQLite() {
		err = r.sq.SetImageLocked(ctx, sqlc.SetImageLockedParams{
			ID:       imageID,
			IsLocked: locked,
		})
	} else {
		err = r.pq.SetImageLocked(ctx, sqlc_pg.SetImageLockedParams{
			ID:       imageID,
			IsLocked: locked,
		})
	}
	if err != nil {
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
	var (
		has bool
		err error
	)
	if r.useSQLite() {
		has, err = r.sq.HasLockedImageForKind(ctx, sqlc.HasLockedImageForKindParams{
			ItemID: itemID,
			Type:   kind,
		})
	} else {
		has, err = r.pq.HasLockedImageForKind(ctx, sqlc_pg.HasLockedImageForKindParams{
			ItemID: itemID,
			Type:   kind,
		})
	}
	if err != nil {
		return false, fmt.Errorf("has locked for kind: %w", err)
	}
	return has, nil
}

func (r *ImageRepository) ListByItem(ctx context.Context, itemID string) ([]*librarymodel.Image, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListImagesByItem(ctx, itemID)
		if err != nil {
			return nil, fmt.Errorf("list images: %w", err)
		}
		return imagesFromSqliteListRows(rows), nil
	}
	rows, err := r.pq.ListImagesByItem(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	return imagesFromPgListRows(rows), nil
}

func (r *ImageRepository) GetPrimary(ctx context.Context, itemID, imgType string) (*librarymodel.Image, error) {
	if r.useSQLite() {
		row, err := r.sq.GetPrimaryImage(ctx, sqlc.GetPrimaryImageParams{
			ItemID: itemID,
			Type:   imgType,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("image for %s/%s: %w", itemID, imgType, domain.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get primary image: %w", err)
		}
		img := imageFromSqlitePrimaryRow(row)
		return &img, nil
	}
	row, err := r.pq.GetPrimaryImage(ctx, sqlc_pg.GetPrimaryImageParams{
		ItemID: itemID,
		Type:   imgType,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("image for %s/%s: %w", itemID, imgType, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get primary image: %w", err)
	}
	img := imageFromPgPrimaryRow(row)
	return &img, nil
}

// GetPrimaryURLs returns the primary poster/backdrop/logo/thumb refs
// for a batch of item IDs. Uses raw SQL because sqlc doesn't support
// dynamic IN(). Thumb is the 16:9 "miniatura" providers ship alongside
// the cartel — landscape rails (Continue Watching) use it for movies
// so the cards stay rectangular like episodes do.
func (r *ImageRepository) GetPrimaryURLs(ctx context.Context, itemIDs []string) (map[string]map[string]librarymodel.PrimaryImageRef, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(itemIDs))
	args := make([]any, len(itemIDs))
	for i, id := range itemIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := rewritePlaceholders(r.driver(), fmt.Sprintf(
		`SELECT item_id, type, path, blurhash, dominant_color, dominant_color_muted FROM images
		 WHERE item_id IN (%s) AND is_primary AND type IN ('primary', 'backdrop', 'logo', 'thumb')
		 ORDER BY item_id, type`,
		joinStrings(placeholders, ","),
	))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get primary urls: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	result := make(map[string]map[string]librarymodel.PrimaryImageRef)
	for rows.Next() {
		var itemID, imgType, path, dominant, dominantMuted string
		var blurhash sql.NullString
		if err := rows.Scan(&itemID, &imgType, &path, &blurhash, &dominant, &dominantMuted); err != nil {
			return nil, fmt.Errorf("scan primary url: %w", err)
		}
		if result[itemID] == nil {
			result[itemID] = make(map[string]librarymodel.PrimaryImageRef)
		}
		result[itemID][imgType] = librarymodel.PrimaryImageRef{
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
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteImagesByItem(ctx, itemID)
	} else {
		err = r.pq.DeleteImagesByItem(ctx, itemID)
	}
	if err != nil {
		return fmt.Errorf("delete images for item: %w", err)
	}
	return nil
}

func (r *ImageRepository) DeleteByID(ctx context.Context, id string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteImageByID(ctx, id)
	} else {
		err = r.pq.DeleteImageByID(ctx, id)
	}
	if err != nil {
		return fmt.Errorf("delete image: %w", err)
	}
	return nil
}

func (r *ImageRepository) GetByID(ctx context.Context, id string) (*librarymodel.Image, error) {
	if r.useSQLite() {
		row, err := r.sq.GetImageByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("image %s: %w", id, domain.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get image: %w", err)
		}
		img := imageFromSqliteGetRow(row)
		return &img, nil
	}
	row, err := r.pq.GetImageByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("image %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get image: %w", err)
	}
	img := imageFromPgGetRow(row)
	return &img, nil
}

func (r *ImageRepository) SetPrimary(ctx context.Context, itemID, imgType, imageID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if r.useSQLite() {
		qtx := r.sq.WithTx(tx)
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
	} else {
		qtx := r.pq.WithTx(tx)
		if err := qtx.UnsetPrimaryImages(ctx, sqlc_pg.UnsetPrimaryImagesParams{
			ItemID: itemID,
			Type:   imgType,
		}); err != nil {
			return fmt.Errorf("unset primary: %w", err)
		}
		if err := qtx.SetImagePrimary(ctx, sqlc_pg.SetImagePrimaryParams{
			ID:     imageID,
			ItemID: itemID,
			Type:   imgType,
		}); err != nil {
			return fmt.Errorf("set primary: %w", err)
		}
	}

	return tx.Commit()
}

// ── row mapping helpers ─────────────────────────────────────────────────

func imageFromSqliteGetRow(r sqlc.GetImageByIDRow) librarymodel.Image {
	return librarymodel.Image{
		ID:                 r.ID,
		ItemID:             r.ItemID,
		Type:               r.Type,
		Path:               r.Path,
		Width:              int(r.Width),
		Height:             int(r.Height),
		Blurhash:           r.Blurhash,
		Provider:           r.Provider,
		IsPrimary:          r.IsPrimary.Bool,
		IsLocked:           r.IsLocked,
		AddedAt:            r.AddedAt,
		DominantColor:      r.DominantColor,
		DominantColorMuted: r.DominantColorMuted,
	}
}

func imageFromPgGetRow(r sqlc_pg.GetImageByIDRow) librarymodel.Image {
	return librarymodel.Image{
		ID:                 r.ID,
		ItemID:             r.ItemID,
		Type:               r.Type,
		Path:               r.Path,
		Width:              int(r.Width),
		Height:             int(r.Height),
		Blurhash:           r.Blurhash,
		Provider:           r.Provider,
		IsPrimary:          r.IsPrimary.Bool,
		IsLocked:           r.IsLocked,
		AddedAt:            r.AddedAt,
		DominantColor:      r.DominantColor,
		DominantColorMuted: r.DominantColorMuted,
	}
}

func imageFromSqlitePrimaryRow(r sqlc.GetPrimaryImageRow) librarymodel.Image {
	return librarymodel.Image{
		ID:                 r.ID,
		ItemID:             r.ItemID,
		Type:               r.Type,
		Path:               r.Path,
		Width:              int(r.Width),
		Height:             int(r.Height),
		Blurhash:           r.Blurhash,
		Provider:           r.Provider,
		IsPrimary:          r.IsPrimary.Bool,
		IsLocked:           r.IsLocked,
		AddedAt:            r.AddedAt,
		DominantColor:      r.DominantColor,
		DominantColorMuted: r.DominantColorMuted,
	}
}

func imageFromPgPrimaryRow(r sqlc_pg.GetPrimaryImageRow) librarymodel.Image {
	return librarymodel.Image{
		ID:                 r.ID,
		ItemID:             r.ItemID,
		Type:               r.Type,
		Path:               r.Path,
		Width:              int(r.Width),
		Height:             int(r.Height),
		Blurhash:           r.Blurhash,
		Provider:           r.Provider,
		IsPrimary:          r.IsPrimary.Bool,
		IsLocked:           r.IsLocked,
		AddedAt:            r.AddedAt,
		DominantColor:      r.DominantColor,
		DominantColorMuted: r.DominantColorMuted,
	}
}

func imageFromSqliteListRow(r sqlc.ListImagesByItemRow) librarymodel.Image {
	return librarymodel.Image{
		ID:                 r.ID,
		ItemID:             r.ItemID,
		Type:               r.Type,
		Path:               r.Path,
		Width:              int(r.Width),
		Height:             int(r.Height),
		Blurhash:           r.Blurhash,
		Provider:           r.Provider,
		IsPrimary:          r.IsPrimary.Bool,
		IsLocked:           r.IsLocked,
		AddedAt:            r.AddedAt,
		DominantColor:      r.DominantColor,
		DominantColorMuted: r.DominantColorMuted,
	}
}

func imageFromPgListRow(r sqlc_pg.ListImagesByItemRow) librarymodel.Image {
	return librarymodel.Image{
		ID:                 r.ID,
		ItemID:             r.ItemID,
		Type:               r.Type,
		Path:               r.Path,
		Width:              int(r.Width),
		Height:             int(r.Height),
		Blurhash:           r.Blurhash,
		Provider:           r.Provider,
		IsPrimary:          r.IsPrimary.Bool,
		IsLocked:           r.IsLocked,
		AddedAt:            r.AddedAt,
		DominantColor:      r.DominantColor,
		DominantColorMuted: r.DominantColorMuted,
	}
}

func imagesFromSqliteListRows(rows []sqlc.ListImagesByItemRow) []*librarymodel.Image {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*librarymodel.Image, len(rows))
	for i, row := range rows {
		img := imageFromSqliteListRow(row)
		out[i] = &img
	}
	return out
}

func imagesFromPgListRows(rows []sqlc_pg.ListImagesByItemRow) []*librarymodel.Image {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*librarymodel.Image, len(rows))
	for i, row := range rows {
		img := imageFromPgListRow(row)
		out[i] = &img
	}
	return out
}
