package library

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/imaging"
	"hubplay/internal/imaging/pathmap"
	"hubplay/internal/provider"
)

// ─── ImageRefreshScheduler ──────────────────────────────────────────────────

// ImageRefreshScheduler recorre periódicamente las libraries no-manuales
// para rellenar artwork faltante. Cadencia semanal — suficiente para
// captar actualizaciones de posters en TMDb sin saturar al provider.
// Imágenes locked se respetan por kind (ADR-003).
type ImageRefreshScheduler struct {
	libraries ImageRefreshLibrariesRepo
	refresher *ImageRefresher
	logger    *slog.Logger
	stopCh    chan struct{}
	interval  time.Duration
	startup   time.Duration
}

// ImageRefreshLibrariesRepo: subset del repo de libraries para enumerar targets.
type ImageRefreshLibrariesRepo interface {
	List(ctx context.Context) ([]*librarymodel.Library, error)
}

// NewImageRefreshScheduler crea el scheduler. interval=0 → 7 días;
// startup=0 → 1h tras arranque para no solapar con el scan inicial.
func NewImageRefreshScheduler(libraries ImageRefreshLibrariesRepo, refresher *ImageRefresher, logger *slog.Logger) *ImageRefreshScheduler {
	return &ImageRefreshScheduler{
		libraries: libraries,
		refresher: refresher,
		logger:    logger.With("module", "image-refresh-scheduler"),
		stopCh:    make(chan struct{}),
		interval:  7 * 24 * time.Hour,
		startup:   1 * time.Hour,
	}
}

// Start lanza el scheduler en su propia goroutine.
func (s *ImageRefreshScheduler) Start(ctx context.Context) {
	s.logger.Info("image refresh scheduler started", "interval", s.interval, "first_sweep_in", s.startup)

	go func() {
		select {
		case <-time.After(s.startup):
			s.runSweep(ctx)
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		}

		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.runSweep(ctx)
			case <-s.stopCh:
				s.logger.Info("image refresh scheduler stopped")
				return
			case <-ctx.Done():
				s.logger.Info("image refresh scheduler context cancelled")
				return
			}
		}
	}()
}

func (s *ImageRefreshScheduler) Stop() {
	close(s.stopCh)
}

// runSweep recorre libraries y rellena artwork faltante.
// Libraries en modo manual se saltan. Fallos per-library se loggean y continúan.
func (s *ImageRefreshScheduler) runSweep(ctx context.Context) {
	libs, err := s.libraries.List(ctx)
	if err != nil {
		s.logger.Error("image refresh: list libraries failed", "error", err)
		return
	}
	for _, lib := range libs {
		if lib.ScanMode == "manual" {
			continue
		}
		added, err := s.refresher.RefreshForLibrary(ctx, lib.ID)
		if err != nil {
			s.logger.Warn("image refresh: per-library failure", "library", lib.Name, "error", err)
			continue
		}
		if added > 0 {
			s.logger.Info("image refresh: filled missing artwork", "library", lib.Name, "added", added)
		}
	}
}

// ─── Interfaces colaboradoras ───────────────────────────────────────────────
//
// Definidas aquí (no importadas de handlers) para evitar ciclo de imports.

// ImageRefresherItemRepo: operaciones de items que necesita el refresher.
type ImageRefresherItemRepo interface {
	List(ctx context.Context, filter librarymodel.ItemFilter) ([]*librarymodel.Item, int, error)
}

// ImageRefresherExternalIDRepo: lookup de external-id por item.
type ImageRefresherExternalIDRepo interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.ExternalID, error)
}

// ImageRefresherImagesRepo: operaciones de image-repository usadas.
type ImageRefresherImagesRepo interface {
	ListByItem(ctx context.Context, itemID string) ([]*librarymodel.Image, error)
	Create(ctx context.Context, img *librarymodel.Image) error
	SetPrimary(ctx context.Context, itemID, imgType, imageID string) error
	HasLockedForKind(ctx context.Context, itemID, kind string) (bool, error)
}

// ImageRefresherProvider envuelve la llamada al provider que usa el refresher.
type ImageRefresherProvider interface {
	FetchImages(ctx context.Context, ids map[string]string, itemType provider.ItemType) ([]provider.ImageResult, error)
}

// ─── ImageRefresher ─────────────────────────────────────────────────────────

// ImageRefresher descarga imágenes faltantes de providers externos por cada
// item de una library. Extraído del handler HTTP para testabilidad aislada
// (ADR-005 anti-ciclo).
type ImageRefresher struct {
	items       ImageRefresherItemRepo
	externalIDs ImageRefresherExternalIDRepo
	images      ImageRefresherImagesRepo
	providers   ImageRefresherProvider
	pathmap     *pathmap.Store
	imageDir    string
	logger      *slog.Logger
}

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

// RefreshForLibrary enumera root items (max 50) y descarga cada kind
// de imagen que falte. Descargas via imaging.SafeGet (SSRF-protegido).
// Best-effort por item: fallos se loggean y se saltan.
func (r *ImageRefresher) RefreshForLibrary(ctx context.Context, libraryID string) (int, error) {
	items, _, err := r.items.List(ctx, librarymodel.ItemFilter{
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

func (r *ImageRefresher) refreshForItem(ctx context.Context, item *librarymodel.Item) int {
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

	// Mejor candidato por kind faltante. Kinds locked se saltan:
	// una elección manual sobrevive cada refresh hasta que el admin
	// desbloquee explícitamente.
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

// downloadAndPersist descarga y persiste una imagen. Delega a
// imaging.IngestRemoteImage (atomic writes, blurhash, SSRF-safe).
func (r *ImageRefresher) downloadAndPersist(ctx context.Context, itemID, imgType string, best provider.ImageResult) bool {
	dir := filepath.Join(r.imageDir, itemID)
	ing, err := imaging.IngestRemoteImage(dir, imgType, best.URL, r.logger)
	if err != nil {
		r.logger.Warn("refresh: ingest failed", "url", best.URL, "error", err)
		return false
	}

	imgID := uuid.NewString()
	dbImg := &librarymodel.Image{
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
		// DB rechazó la fila — borrar fichero para no filtrar storage.
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

func itemTypeOf(item *librarymodel.Item) provider.ItemType {
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
