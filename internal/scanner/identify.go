package scanner

// Flujo de "Identify" + edición manual + metadata locks. Es la
// contrapartida humana del enrichment automático (enrich.go): cuando el
// match contra TMDb sale equivocado o el operator quiere imponer un
// título / overview / año a mano, el camino entra por aquí.
//
// El lock es la mecánica de protección: tras un identify manual o una
// edición humana, se llama a `metaLocks.Lock` y el resto del scanner
// (`enrichIfMissing`, `RefreshMetadata`, `RefreshItemMetadata`) lo
// respeta — los items locked no se vuelven a pisar hasta que el
// operator los desbloquee explícitamente. Sin esto, un re-scan
// destruiría silenciosamente cualquier corrección manual.

import (
	"context"
	"fmt"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/provider"
)

// SearchCandidates ejecuta una búsqueda en los providers de metadatos
// usando el título y año del propio item como semilla por defecto, o
// los que el operador haya escrito en el diálogo de "Identify". Devuelve
// los candidatos en bruto para que el frontend pueda renderizar la lista
// con pósters; la decisión de cuál aplicar la toma la persona, no el
// algoritmo. Sólo películas y series — episodios/temporadas no tienen
// flujo de identify (su match cuelga del padre).
func (s *Scanner) SearchCandidates(ctx context.Context, itemID, query string, year int) ([]provider.SearchResult, error) {
	if s.providers == nil {
		return nil, fmt.Errorf("scanner: no metadata providers configured")
	}
	item, err := s.items.GetByID(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("scanner: load item %s: %w", itemID, err)
	}
	if item.Type == "episode" || item.Type == "season" {
		return nil, fmt.Errorf("scanner: identify not supported for %s items", item.Type)
	}
	if query == "" {
		query, _ = parseTitleYear(item.Title)
		if query == "" {
			query = item.Title
		}
	}
	if year == 0 {
		year = item.Year
	}
	return s.providers.SearchMetadata(ctx, provider.SearchQuery{
		Title:    query,
		Year:     year,
		ItemType: itemTypeForProvider(item.Type),
	})
}

// IdentifyAndApply forza el emparejamiento del item con un externalID
// concreto elegido por el operador (TMDb id que se ha visto en el diálogo
// de "Identify"). Borra las imágenes y metadatos previos antes de aplicar
// los nuevos — un rematch manual implica que los datos viejos eran
// incorrectos y queremos un estado limpio, no una fusión silenciosa.
//
// Sólo películas y series. Episodios y temporadas no se reidentifican por
// id propio: heredan el match de la serie padre vía season/episode number,
// así que el flujo correcto para arreglar un episodio mal nombrado es
// identificar la serie y dejar que el siguiente refresh recree las hojas.
func (s *Scanner) IdentifyAndApply(ctx context.Context, itemID, externalID string) error {
	if s.providers == nil {
		return fmt.Errorf("scanner: no metadata providers configured")
	}
	if externalID == "" {
		return fmt.Errorf("scanner: external_id required")
	}
	item, err := s.items.GetByID(ctx, itemID)
	if err != nil {
		return fmt.Errorf("scanner: load item %s: %w", itemID, err)
	}
	if item.Type == "episode" || item.Type == "season" {
		return fmt.Errorf("scanner: identify not supported for %s items", item.Type)
	}

	itemType := itemTypeForProvider(item.Type)
	meta, err := s.providers.FetchMetadata(ctx, externalID, itemType)
	if err != nil {
		return fmt.Errorf("scanner: fetch metadata for %s/%s: %w", item.Type, externalID, err)
	}
	if meta == nil {
		return fmt.Errorf("scanner: provider returned no metadata for %s", externalID)
	}

	// Limpia el estado previo antes de aplicar: imágenes locales pueden
	// estar apuntando al match equivocado y la metadata textual (overview,
	// género, reparto) la regenera applyMetadata completa. Si el borrado
	// falla no es bloqueante — el upsert posterior sobrescribe la fila.
	// Debug por si en producción aparece corrupción inexplicable: poder
	// rastrear si el delete fallaba silenciosamente.
	if err := s.images.DeleteByItem(ctx, item.ID); err != nil {
		s.logger.Debug("identify: delete previous images failed", "item_id", item.ID, "error", err)
	}
	if err := s.metadata.Delete(ctx, item.ID); err != nil {
		s.logger.Debug("identify: delete previous metadata failed", "item_id", item.ID, "error", err)
	}

	// Aplica el título del nuevo match como Title también — sin esto el
	// item conserva el nombre crudo del fichero ("Pelicula.2024.BRRip")
	// que es justo lo que el operador estaba intentando arreglar.
	if meta.Title != "" {
		item.Title = meta.Title
	}

	s.applyMetadata(ctx, item, meta, itemType, externalID)

	// Lock tras aplicar: el operador acaba de DECIR explícitamente cuál
	// es el match correcto. Sin lock, el siguiente "Refresh metadata"
	// vuelve a hacer Search→Fetch y la heurística automática puede
	// volver al match equivocado original. Esto es precisamente lo que
	// el lock existe para prevenir.
	if s.metaLocks != nil {
		if err := s.metaLocks.Lock(ctx, itemID); err != nil {
			s.logger.Warn("failed to lock item metadata after identify", "id", itemID, "error", err)
		}
	}
	return nil
}

// ItemMetadataPatch es el payload del editor manual de metadatos.
// Cada campo es opcional (puntero); un *nil* deja el campo del item
// inalterado, un puntero no-nil — incluyendo cadenas vacías — escribe
// el valor. Esta semántica deliberadamente permite "borrar" un
// overview escribiendo "", distinta del "no tocar" de nil.
type ItemMetadataPatch struct {
	Title         *string
	OriginalTitle *string
	Year          *int
	Overview      *string
	Tagline       *string
}

// UpdateItemMetadata aplica una edición manual sobre un item: actualiza
// los campos no-nil del patch en `items` y/o `metadata`, y bloquea el
// item para que el siguiente refresh del scanner no lo pise. Mismo
// contrato que IdentifyAndApply respecto al lock — cualquier edición
// humana queda fija hasta que el admin la desbloquee.
//
// Devuelve el item actualizado para que el handler pueda re-emitir el
// JSON de detalle sin un round-trip extra.
func (s *Scanner) UpdateItemMetadata(ctx context.Context, itemID string, patch ItemMetadataPatch) (*librarymodel.Item, error) {
	item, err := s.items.GetByID(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("scanner: load item %s: %w", itemID, err)
	}

	// Aplica los cambios sobre `items`. Reusamos items.Update — que
	// reescribe TODA la fila — porque sólo modificamos los campos
	// del patch sobre el objeto previo. Los otros valores van
	// idénticos a lo que estaba en DB, así que no se pierden.
	touchedItems := false
	if patch.Title != nil {
		item.Title = *patch.Title
		touchedItems = true
	}
	if patch.OriginalTitle != nil {
		item.OriginalTitle = *patch.OriginalTitle
		touchedItems = true
	}
	if patch.Year != nil {
		item.Year = *patch.Year
		touchedItems = true
	}
	if touchedItems {
		item.UpdatedAt = s.clock.Now()
		if err := s.items.Update(ctx, item); err != nil {
			return nil, fmt.Errorf("scanner: update item %s: %w", itemID, err)
		}
	}

	// `metadata` es Upsert; necesitamos la fila previa para preservar
	// los campos que el patch no toca (studio, genres, trailer, etc.).
	if patch.Overview != nil || patch.Tagline != nil {
		meta, err := s.metadata.GetByItemID(ctx, itemID)
		if err != nil {
			return nil, fmt.Errorf("scanner: load metadata %s: %w", itemID, err)
		}
		if meta == nil {
			meta = &librarymodel.Metadata{ItemID: itemID}
		}
		if patch.Overview != nil {
			meta.Overview = *patch.Overview
		}
		if patch.Tagline != nil {
			meta.Tagline = *patch.Tagline
		}
		if err := s.metadata.Upsert(ctx, meta); err != nil {
			return nil, fmt.Errorf("scanner: upsert metadata %s: %w", itemID, err)
		}
	}

	// Lock: cualquier edición manual implica "no me pises esto".
	if s.metaLocks != nil {
		if err := s.metaLocks.Lock(ctx, itemID); err != nil {
			s.logger.Warn("failed to lock item after manual edit", "id", itemID, "error", err)
		}
	}

	return item, nil
}

// RefreshItemMetadata re-corre el flujo de enrichMetadata sobre un
// item concreto. Se usa desde el kebab "Actualizar metadatos" del
// detalle / posters: el operador acaba de arreglar metadata source
// (e.g. configuró TMDb api_key, o el fix del año-mismatch del scanner
// llegó después del scan inicial) y quiere que el item se re-empareje
// SIN tener que dispararse un full library refresh.
//
// Respeta el lock — locked items no se tocan, igual que el resto del
// scanner. La diferencia frente a IdentifyAndApply es que aquí NO se
// fuerza un externalID; el scanner decide vía Search→Fetch como en el
// scan normal. Para imponer un match concreto, IdentifyAndApply sigue
// siendo el camino.
func (s *Scanner) RefreshItemMetadata(ctx context.Context, itemID string) error {
	if s.providers == nil {
		return fmt.Errorf("scanner: no metadata providers configured")
	}
	item, err := s.items.GetByID(ctx, itemID)
	if err != nil {
		return fmt.Errorf("scanner: load item %s: %w", itemID, err)
	}
	if s.metaLocks != nil {
		if locked, lErr := s.metaLocks.IsLocked(ctx, itemID); lErr == nil && locked {
			// Locked: no-op silencioso. El kebab tiene un toggle de
			// lock distinto si el operador quiere soltarlo.
			return nil
		}
	}
	// Limpia el estado previo (igual que RefreshMetadata global) — un
	// "Actualizar metadatos" implica que lo que había podía estar
	// estale; mejor partir limpio que mergear incoherencias.
	if err := s.images.DeleteByItem(ctx, item.ID); err != nil {
		s.logger.Debug("refresh-metadata: delete previous images failed", "item_id", item.ID, "error", err)
	}
	if err := s.metadata.Delete(ctx, item.ID); err != nil {
		s.logger.Debug("refresh-metadata: delete previous metadata failed", "item_id", item.ID, "error", err)
	}

	switch item.Type {
	case "episode":
		if item.SeasonNumber != nil && item.EpisodeNumber != nil && item.ParentID != "" {
			s.enrichEpisode(ctx, item, item.ParentID, *item.SeasonNumber, *item.EpisodeNumber)
		}
	case "season":
		if item.SeasonNumber != nil && item.ParentID != "" {
			s.enrichSeason(ctx, item, item.ParentID, *item.SeasonNumber)
		}
	default:
		s.enrichMetadata(ctx, item)
	}
	return nil
}

// SetMetadataLock cambia el estado del lock de un item directamente,
// sin pasar por un identify. Lo usan el editor manual de metadatos
// (para que la edición sobreviva refreshes) y el toggle del kebab
// "Bloquear/Desbloquear metadatos" en la UI del detalle.
func (s *Scanner) SetMetadataLock(ctx context.Context, itemID string, locked bool) error {
	if s.metaLocks == nil {
		return fmt.Errorf("scanner: metadata locks repository not wired")
	}
	if locked {
		return s.metaLocks.Lock(ctx, itemID)
	}
	return s.metaLocks.Unlock(ctx, itemID)
}

// IsMetadataLocked es el lookup que la UI del detalle usa para
// renderizar el estado del candado en el kebab. Devuelve false sin
// error cuando no hay lock o el repo no está cableado.
func (s *Scanner) IsMetadataLocked(ctx context.Context, itemID string) (bool, error) {
	if s.metaLocks == nil {
		return false, nil
	}
	return s.metaLocks.IsLocked(ctx, itemID)
}
