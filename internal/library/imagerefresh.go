package library

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"hubplay/internal/db"
	"hubplay/internal/imaging"
	"hubplay/internal/imaging/pathmap"
	"hubplay/internal/provider"
)

// ─── Collaborator interfaces ────────────────────────────────────────────────
//
// Defined here (not imported from handlers) to avoid an import cycle — handlers
// depend on library, not the other way round. The concrete db.*Repository and
// provider.Manager types satisfy these; the handler's fakes satisfy them too
// via structural typing.

// ImageRefresherItemRepo is the subset of item operations the refresher needs.
type ImageRefresherItemRepo interface {
	List(ctx context.Context, filter db.ItemFilter) ([]*db.Item, int, error)
}

// ImageRefresherExternalIDRepo is the subset for external-id lookup per item.
type ImageRefresherExternalIDRepo interface {
	ListByItem(ctx context.Context, itemID string) ([]*db.ExternalID, error)
}

// ImageRefresherImagesRepo is the subset of image-repository operations used.
type ImageRefresherImagesRepo interface {
	ListByItem(ctx context.Context, itemID string) ([]*db.Image, error)
	Create(ctx context.Context, img *db.Image) error
	SetPrimary(ctx context.Context, itemID, imgType, imageID string) error
	HasLockedForKind(ctx context.Context, itemID, kind string) (bool, error)
}

// ImageRefresherProvider wraps the single provider call used by the refresher.
type ImageRefresherProvider interface {
	FetchImages(ctx context.Context, ids map[string]string, itemType provider.ItemType) ([]provider.ImageResult, error)
}

// ─── ImageRefresher ─────────────────────────────────────────────────────────

// ImageRefresher pulls missing images from external providers for every item
// in a library. Extracted out of the HTTP handler so the loop is testable
// in isolation and the handler stays thin (ADR-005 anti-cycle — the handler
// depends on a small ImageRefreshService interface, not this concrete type).
type ImageRefresher struct {
	items       ImageRefresherItemRepo
	externalIDs ImageRefresherExternalIDRepo
	images      ImageRefresherImagesRepo
	providers   ImageRefresherProvider
	pathmap     *pathmap.Store
	imageDir    string
	logger      *slog.Logger
}

// NewImageRefresher constructs an ImageRefresher. imageDir is the root for
// on-disk storage (<imageDir>/<itemID>/filename); pathmap persists the
// imageID → local-path mapping used at serve time.
func NewImageRefresher(
	items ImageRefresherItemRepo,
	externalIDs ImageRefresherExternalIDRepo,
	images ImageRefresherImagesRepo,
	providers ImageRefresherProvider,
	pm *pathmap.Store,
	imageDir string,
	logger *slog.Logger,
) *ImageRefresher {
	return &ImageRefresher{
		items:       items,
		externalIDs: externalIDs,
		images:      images,
		providers:   providers,
		pathmap:     pm,
		imageDir:    imageDir,
		logger:      logger.With("module", "image-refresh"),
	}
}

// RefreshForLibrary enumerates root items (max 50 per call) and for each one
// fetches every image kind that isn't already stored. Downloads go through
// imaging.SafeGet (SSRF-protected) and are rejected if dimensions exceed
// imaging.MaxPixels. Returns the count of newly persisted images.
//
// The method is best-effort per item: a download, save, or DB failure for one
// item is logged and skipped, not propagated. Only a failure to enumerate the
// library's items surfaces as an error.
func (r *ImageRefresher) RefreshForLibrary(ctx context.Context, libraryID string) (int, error) {
	items, _, err := r.items.List(ctx, db.ItemFilter{
		LibraryID: libraryID,
		Limit:     50,
	})
	if err != nil {
		return 0, fmt.Errorf("list items: %w", err)
	}

	updated := 0
	for _, item := range items {
		updated += r.refreshForItem(ctx, item)
	}
	return updated, nil
}

// refreshForItem is the per-item loop extracted for readability. Errors are
// logged and counted as zero updates — the caller keeps going.
func (r *ImageRefresher) refreshForItem(ctx context.Context, item *db.Item) int {
	extIDs, err := r.externalIDs.ListByItem(ctx, item.ID)
	if err != nil || len(extIDs) == 0 {
		return 0
	}

	idMap := make(map[string]string, len(extIDs))
	for _, e := range extIDs {
		idMap[e.Provider] = e.ExternalID
	}

	results, err := r.providers.FetchImages(ctx, idMap, itemTypeOf(item))
	if err != nil || len(results) == 0 {
		return 0
	}

	existing, err := r.images.ListByItem(ctx, item.ID)
	if err != nil {
		return 0
	}
	existingTypes := make(map[string]bool, len(existing))
	for _, img := range existing {
		existingTypes[img.Type] = true
	}

	// Pick the highest-scored candidate for each missing kind.
	// Skip kinds the user has locked: a manual choice (uploaded
	// custom poster, picked a specific candidate) survives every
	// refresh until the admin explicitly unlocks. Without this guard
	// the next scheduled refresh silently clobbers curation work.
	bestByType := make(map[string]provider.ImageResult)
	for _, img := range results {
		if !imaging.IsValidKind(img.Type) {
			continue
		}
		if existingTypes[img.Type] {
			continue
		}
		if locked, err := r.images.HasLockedForKind(ctx, item.ID, img.Type); err != nil {
			r.logger.Warn("refresh: lock check failed", "item_id", item.ID, "kind", img.Type, "error", err)
			continue
		} else if locked {
			continue
		}
		if cur, ok := bestByType[img.Type]; !ok || img.Score > cur.Score {
			bestByType[img.Type] = img
		}
	}

	added := 0
	for imgType, best := range bestByType {
		if r.downloadAndPersist(ctx, item.ID, imgType, best) {
			added++
		}
	}
	return added
}

// downloadAndPersist returns true if the image was successfully stored. A
// false return means the caller should move on — every failure is logged
// inline so the caller doesn't need to distinguish causes.
//
// All disk + network work is delegated to imaging.IngestRemoteImage so the
// scanner and the refresher land bytes through identical code paths
// (atomic writes, blurhash, SSRF-safe downloads). The only thing that
// stays per-caller is what we record in the DB and the path-mapping
// table.
func (r *ImageRefresher) downloadAndPersist(ctx context.Context, itemID, imgType string, best provider.ImageResult) bool {
	dir := filepath.Join(r.imageDir, itemID)
	ing, err := imaging.IngestRemoteImage(dir, imgType, best.URL, r.logger)
	if err != nil {
		r.logger.Warn("refresh: ingest failed", "url", best.URL, "error", err)
		return false
	}

	imgID := uuid.NewString()
	dbImg := &db.Image{
		ID:                 imgID,
		ItemID:             itemID,
		Type:               imgType,
		Path:               "/api/v1/images/file/" + imgID,
		Width:              best.Width,
		Height:             best.Height,
		Blurhash:           ing.Blurhash,
		Provider:           "refresh",
		IsPrimary:          true,
		AddedAt:            time.Now(),
		DominantColor:      ing.DominantColor,
		DominantColorMuted: ing.DominantColorMuted,
	}

	if err := r.images.Create(ctx, dbImg); err != nil {
		// DB rejected the row; back out the on-disk file so we don't
		// leak storage behind a record that no longer exists.
		_ = os.Remove(ing.LocalPath)
		return false
	}

	if err := r.images.SetPrimary(ctx, itemID, imgType, imgID); err != nil {
		r.logger.Warn("refresh: failed to set primary", "error", err)
	}

	if err := r.pathmap.Write(imgID, ing.LocalPath); err != nil {
		r.logger.Warn("refresh: pathmap write failed", "id", imgID, "error", err)
	}
	return true
}

// itemTypeOf maps the DB-level item type string onto the provider-level enum.
// Unknown values default to Movie (matches the original handler behaviour).
func itemTypeOf(item *db.Item) provider.ItemType {
	switch item.Type {
	case "series":
		return provider.ItemSeries
	case "season":
		return provider.ItemSeason
	case "episode":
		return provider.ItemEpisode
	default:
		return provider.ItemMovie
	}
}
