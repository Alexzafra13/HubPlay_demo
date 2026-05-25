package scanner

// Ingest de media externa (imágenes + thumbnails de people) desde los
// providers. Ambas funciones comparten el patrón "descarga via
// imaging.IngestRemoteImage (SSRF/size/atomic-write/blurhash), persiste
// la fila, registra el pathmap; un fallo se loguea pero no aborta el
// scan — perder un póster es mejor que tumbar la library".
//
// `fetchAndStoreImages` cubre el flujo de movies/series (poster +
// backdrop + logo, dedupeado por mejor score). `syncPeople` cubre el
// reparto (download dedupado por persona, primer item que ve a una
// persona dispara la descarga). El uso de un subdir `.people/` saca a
// los thumbnails del listado per-item.

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"hubplay/internal/imaging"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/provider"

	"github.com/google/uuid"
)

// fetchAndStoreImages descarga la mejor candidata de cada tipo de imagen
// (póster, fondo, logo) y la guarda en local. Si una imagen falla, se
// loguea y se sigue — perder un póster es mejor que tumbar todo el scan.
func (s *Scanner) fetchAndStoreImages(ctx context.Context, itemID string, externalIDs map[string]string, itemType provider.ItemType) {
	log := s.logger.With("item_id", itemID)
	results, err := s.providers.FetchImages(ctx, externalIDs, itemType)
	if err != nil {
		log.Debug("provider image fetch failed", "error", err)
		return
	}
	if len(results) == 0 {
		return
	}

	// Para cada tipo de imagen elegimos la mejor puntuada. Sin esto
	// cogeríamos la primera que llegue, y dos scans seguidos darían
	// resultados distintos.
	bestByKind := make(map[string]provider.ImageResult)
	for _, img := range results {
		switch img.Type {
		case "primary", "backdrop", "logo":
		default:
			continue
		}
		if cur, ok := bestByKind[img.Type]; !ok || img.Score > cur.Score {
			bestByKind[img.Type] = img
		}
	}

	dir := filepath.Join(s.imageDir, itemID)
	for kind, best := range bestByKind {
		ing, err := imaging.IngestRemoteImage(dir, kind, best.URL, s.logger)
		if err != nil {
			log.Warn("scanner: image ingest failed", "kind", kind, "error", err)
			continue
		}

		imgID := uuid.NewString()
		// El nombre del provider lo marca el Manager al devolver la imagen,
		// así no hay que adivinarlo por la URL. "unknown" es el último
		// recurso por si en el futuro algún provider no lo rellena.
		providerName := best.Source
		if providerName == "" {
			providerName = "unknown"
		}
		dbImg := &librarymodel.Image{
			ID:                 imgID,
			ItemID:             itemID,
			Type:               kind,
			Path:               "/api/v1/images/file/" + imgID,
			Width:              best.Width,
			Height:             best.Height,
			Blurhash:           ing.Blurhash,
			Provider:           providerName,
			IsPrimary:          true,
			AddedAt:            time.Now(),
			DominantColor:      ing.DominantColor,
			DominantColorMuted: ing.DominantColorMuted,
		}
		if err := s.images.Create(ctx, dbImg); err != nil {
			log.Warn("scanner: failed to store image row", "kind", kind, "error", err)
			_ = os.Remove(ing.LocalPath)
			continue
		}
		if err := s.pathmap.Write(imgID, ing.LocalPath); err != nil {
			s.logger.Warn("scanner: pathmap write failed", "id", imgID, "error", err)
		}
	}
}

// syncPeople: persiste cast/crew del provider. Idempotente: limpia
// item_people antes de re-insert, así re-scans recogen cambios (p.ej.
// guest-star nueva en TMDb) sin dejar rows stale.
//
// Photos: people dedupeados por nombre — el PRIMER item que ve a una persona
// dispara la descarga. Items siguientes reusan el row sin red. Fallos de
// descarga se loguean; el cast row se persiste con thumb_path vacío y el UI
// cae a la chip de inicial.
//
// Storage: <imageDir>/.people/<personID>/ — el prefijo "." los saca del
// listado per-item, y un subdir per-person hace `delete-by-person` = 1 sola
// os.RemoveAll.
func (s *Scanner) syncPeople(ctx context.Context, itemID string, people []provider.Person) {
	if s.people == nil || len(people) == 0 {
		return
	}

	credits := make([]librarymodel.ItemPersonCredit, 0, len(people))
	for _, p := range people {
		if p.Name == "" {
			continue
		}
		personID, created, err := s.people.EnsureByName(ctx, p.Name, p.Role)
		if err != nil {
			s.logger.Warn("person upsert failed", "name", p.Name, "error", err)
			continue
		}
		// Descarga sólo para people recién creados con URL. IngestRemoteImage
		// reusa el pipeline SSRF/size/atomic-write — sin re-implementar.
		if created && p.ThumbURL != "" && s.imageDir != "" {
			dir := filepath.Join(s.imageDir, ".people", personID)
			if ing, err := imaging.IngestRemoteImage(dir, "profile", p.ThumbURL, s.logger); err == nil {
				if err := s.people.SetThumbPath(ctx, personID, ing.LocalPath); err != nil {
					s.logger.Warn("person thumb path save failed", "id", personID, "error", err)
				}
			} else {
				s.logger.Debug("person thumb download failed", "name", p.Name, "url", p.ThumbURL, "error", err)
			}
		}
		credits = append(credits, librarymodel.ItemPersonCredit{
			PersonID:      personID,
			Role:          p.Role,
			CharacterName: p.Character,
			SortOrder:     p.Order,
		})
	}
	if len(credits) == 0 {
		return
	}
	if err := s.people.ReplaceItemPeople(ctx, itemID, credits); err != nil {
		s.logger.Warn("replace item people failed", "item_id", itemID, "error", err)
	}
}
