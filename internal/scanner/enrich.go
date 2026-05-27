package scanner

// Enrichment de movies + series con metadatos de TMDb. Este fichero
// cubre el camino del provider para items "raíz" (movies y series — las
// hojas season/episode tienen flujo propio en enrich_season_episode.go).
//
// Tres entry points públicos consumen este código:
//   - `RefreshMetadata` — refresh global de toda la library (admin).
//   - `enrichIfMissing` — relleno opportunístico durante el walk.
//   - `enrichMetadata` — primer enrichment al crear el item.
//
// El common ground (`applyMetadata`) se extrajo para que el flujo de
// "Identify" (rematch manual del operator) reutilice la misma maquinaria.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/provider"
)

// RefreshMetadata: borra images+metadata y re-enriquece desde los providers.
//
// Dispatch por tipo importa: enrichMetadata sólo cubre movies+series (TMDb
// search por título). Episodes → enrichEpisode (TMDb /tv/../season/N/ep/M);
// seasons → enrichSeason. Sin el dispatch, refrescar deja episode/season sin
// stills ni sinopsis.
//
// Orden: iterateLibraryItems pasa series → season → episode → movie → audio,
// así cuando llegamos a season/episode el tmdb id del parent ya está en DB.
func (s *Scanner) RefreshMetadata(ctx context.Context, lib *librarymodel.Library) error {
	s.logger.Info("refreshing metadata for library", "library", lib.Name)

	count := 0
	err := s.iterateLibraryItems(ctx, lib.ID, func(item *librarymodel.Item) {
		// Lock check: el "Refresh metadata" global respeta los locks
		// igual que el scan normal. Sin esto el botón de "Refrescar"
		// destruye silenciosamente cualquier identify manual previo,
		// que es justo lo que el lock existe para prevenir.
		if s.metaLocks != nil {
			if locked, err := s.metaLocks.IsLocked(ctx, item.ID); err == nil && locked {
				return
			}
		}
		// Borramos imágenes y metadatos para que se vuelvan a generar. Si
		// el borrado falla, no pasa nada — el upsert posterior sobrescribe.
		_ = s.images.DeleteByItem(ctx, item.ID)
		_ = s.metadata.Delete(ctx, item.ID)

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
		count++
	})
	if err != nil {
		return fmt.Errorf("listing items for refresh: %w", err)
	}

	s.logger.Info("metadata refresh complete", "library", lib.Name, "items", count)
	return nil
}

// Reconoce (2023), [2023] o 2023 suelto en un nombre de fichero.
var yearPattern = regexp.MustCompile(`[\(\[\s]?((?:19|20)\d{2})[\)\]\s]?`)

// parseTitleYear separa el título y el año del nombre del fichero.
// Ej: "Transformers El despertar (2023)" → ("Transformers El despertar", 2023).
func parseTitleYear(filename string) (string, int) {
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filepath.Base(filename), ext)

	// El último número que parece año suele ser el año de estreno.
	matches := yearPattern.FindAllStringSubmatchIndex(name, -1)
	if len(matches) == 0 {
		return name, 0
	}

	last := matches[len(matches)-1]
	yearStr := name[last[2]:last[3]]
	year, _ := strconv.Atoi(yearStr)

	// El título es todo lo que viene antes del año.
	title := strings.TrimSpace(name[:last[0]])
	if title == "" {
		title = name
	}

	return title, year
}

// enrichIfMissing intenta volver a rellenar los metadatos de un elemento
// al que le faltan (p.ej. porque no había clave de TMDb cuando se escaneó).
func (s *Scanner) enrichIfMissing(ctx context.Context, item *librarymodel.Item) {
	if s.providers == nil {
		return
	}
	// Lock check — locked items conservan lo que el humano dejó, aunque
	// les falten imágenes. El admin pidió este estado explícitamente.
	if s.metaLocks != nil {
		if locked, err := s.metaLocks.IsLocked(ctx, item.ID); err == nil && locked {
			return
		}
	}
	// Si ya tiene alguna imagen, lo damos por hecho.
	imgs, err := s.images.ListByItem(ctx, item.ID)
	if err == nil && len(imgs) > 0 {
		return
	}
	s.logger.Info("re-enriching item missing metadata", "title", item.Title, "id", item.ID)
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
}

// enrichMetadata busca en TMDb y guarda los metadatos y las imágenes.
//
// Sólo se aplica a series y películas. Las temporadas y los episodios se
// saltan a propósito: sus títulos son demasiado ruidosos y una serie de
// 100 capítulos quemaría 100 búsquedas para resultados que ni se enseñan.
// Esta protección evita que "Refrescar metadatos" se cargue la cuota de TMDb.
func (s *Scanner) enrichMetadata(ctx context.Context, item *librarymodel.Item) {
	if s.providers == nil {
		return
	}
	if item.Type == "episode" || item.Type == "season" {
		return
	}
	// Lock check: si un humano ha identificado / editado este item, no
	// lo pisamos. El admin desbloquea explícitamente desde la UI si
	// quiere volver al modo auto. Mismo guard en enrichIfMissing y
	// RefreshMetadata — ningún camino de scanner toca un locked.
	if s.metaLocks != nil {
		if locked, err := s.metaLocks.IsLocked(ctx, item.ID); err == nil && locked {
			return
		}
	}

	cleanTitle, year := parseTitleYear(item.Title)
	if year == 0 {
		year = item.Year
	}

	itemType := itemTypeForProvider(item.Type)

	results, err := s.providers.SearchMetadata(ctx, provider.SearchQuery{
		Title:    cleanTitle,
		Year:     year,
		ItemType: itemType,
	})
	if err != nil {
		s.logger.Debug("TMDB search failed", "title", cleanTitle, "year", year, "error", err)
		return
	}
	// Fallback: muchos M3U / filenames traen el año entre paréntesis
	// como pista del operador y NO el año oficial de estreno
	// (ej. "Toy Story (2020).mkv" cuando la peli es de 1995). Si el
	// filtro de año no devuelve nada, reintentamos sólo con el título —
	// mucho mejor un match aproximado que dejar el item sin metadatos.
	if len(results) == 0 && year > 0 {
		retry, retryErr := s.providers.SearchMetadata(ctx, provider.SearchQuery{
			Title:    cleanTitle,
			ItemType: itemType,
		})
		if retryErr == nil && len(retry) > 0 {
			results = retry
			s.logger.Debug("TMDB matched after year-less retry", "title", cleanTitle, "skipped_year", year)
		}
	}
	if len(results) == 0 {
		s.logger.Debug("no TMDB results", "title", cleanTitle, "year", year)
		return
	}

	best := results[0]

	meta, err := s.providers.FetchMetadata(ctx, best.ExternalID, itemType)
	if err != nil || meta == nil {
		s.logger.Debug("TMDB metadata fetch failed", "id", best.ExternalID, "error", err)
		return
	}

	s.applyMetadata(ctx, item, meta, itemType, best.ExternalID)
}

// applyMetadata aplica un MetadataResult ya obtenido sobre un item en BD:
// actualiza la fila de `items`, hace upsert de `metadata`, enlaza estudio y
// saga, persiste external_ids, sincroniza el reparto y descarga imágenes.
//
// Se extrajo de enrichMetadata para que el flujo de "Identify" (rematch
// manual desde la UI admin) reutilice exactamente la misma maquinaria.
// Ambos caminos llegan aquí con un meta válido — la diferencia es que el
// scanner lo obtiene de Search→Fetch y el handler de Identify lo obtiene
// llamando a FetchMetadata directo con el TMDb id que eligió el operador.
func (s *Scanner) applyMetadata(ctx context.Context, item *librarymodel.Item, meta *provider.MetadataResult, itemType provider.ItemType, primaryExternalID string) {
	log := s.logger.With("item_id", item.ID)

	if meta.Title != "" {
		item.Title = meta.Title
		item.OriginalTitle = meta.OriginalTitle
	}
	if meta.Year > 0 {
		item.Year = meta.Year
	}
	if meta.Rating != nil {
		item.CommunityRating = meta.Rating
	}
	if meta.ContentRating != "" {
		item.ContentRating = meta.ContentRating
	}
	if meta.PremiereDate != nil {
		item.PremiereDate = meta.PremiereDate
	}
	item.UpdatedAt = s.clock.Now()
	if err := s.items.Update(ctx, item); err != nil {
		log.Warn("failed to update item with metadata", "error", err)
	}

	// El tráiler viene en la misma llamada de TMDb (vídeos adjuntos), así
	// que no hace falta una segunda petición sólo para el id de YouTube.
	genresJSON, _ := json.Marshal(meta.Genres)
	tagsJSON, _ := json.Marshal(meta.Tags)
	if err := s.metadata.Upsert(ctx, &librarymodel.Metadata{
		ItemID:        item.ID,
		Overview:      meta.Overview,
		Tagline:       meta.Tagline,
		Studio:        meta.Studio,
		GenresJSON:    string(genresJSON),
		TagsJSON:      string(tagsJSON),
		TrailerKey:    meta.TrailerKey,
		TrailerSite:   meta.TrailerSite,
		StudioLogoURL: meta.StudioLogoURL,
	}); err != nil {
		log.Warn("failed to store metadata", "error", err)
	}

	// Replicamos los géneros en una tabla aparte para que los filtros de
	// /movies y /series puedan buscar por índice en vez de escanear el JSON.
	// Es replace: si TMDb deja de devolver un género, el chip desaparece.
	if s.itemValues != nil {
		if err := s.itemValues.SetGenres(ctx, item.ID, meta.Genres); err != nil {
			log.Warn("failed to mirror genres into item_values", "error", err)
		}
	}

	// Enlazamos al estudio (productora o cadena) para que el logo en el
	// detalle abra la página de ese estudio. Si no hay estudio, queda sin
	// enlazar y no aparece en /studios.
	if s.studios != nil {
		var tmdbIDPtr *int64
		if meta.StudioTMDBID > 0 {
			tmdbIDPtr = &meta.StudioTMDBID
		}
		studioID, sErr := s.studios.EnsureStudio(ctx, meta.Studio, meta.StudioLogoURL, tmdbIDPtr)
		if sErr != nil {
			log.Warn("failed to ensure studio", "studio", meta.Studio, "error", sErr)
		} else if err := s.studios.SetItemStudio(ctx, item.ID, studioID); err != nil {
			log.Warn("failed to link item to studio", "studio_id", studioID, "error", err)
		}
	}

	// Enlazamos la película con su saga (X-Men, MCU, Toy Story...) para que
	// se pueda navegar entera en /collections/{id}. Las series no tienen
	// saga en TMDb, así que en su caso se queda sin enlazar.
	if s.collections != nil {
		collectionID, cErr := s.collections.EnsureCollection(
			ctx,
			meta.CollectionTMDBID,
			meta.CollectionName,
			meta.CollectionOverview,
			meta.CollectionPoster,
			meta.CollectionBackdrop,
		)
		if cErr != nil {
			log.Warn("failed to ensure collection", "collection", meta.CollectionName, "error", cErr)
		} else if err := s.collections.SetItemCollection(ctx, item.ID, collectionID); err != nil {
			log.Warn("failed to link item to collection", "collection_id", collectionID, "error", err)
		}
	}

	for prov, extID := range meta.ExternalIDs {
		if err := s.externalIDs.Upsert(ctx, &librarymodel.ExternalID{
			ItemID:     item.ID,
			Provider:   prov,
			ExternalID: extID,
		}); err != nil {
			log.Warn("failed to store external id", "provider", prov, "error", err)
		}
	}

	// El reparto se guarda con tolerancia a fallos: si algo va mal, se loguea
	// pero no se aborta el scan.
	s.syncPeople(ctx, item.ID, meta.People)

	// Las imágenes se descargan a local y se sirven desde nuestra propia
	// URL. Nunca guardamos la URL remota, porque filtraría la IP del
	// usuario a TMDb cada vez que se viera un póster y rompería la
	// biblioteca el día que TMDb no respondiera.
	if len(meta.ExternalIDs) > 0 && s.imageDir != "" && s.pathmap != nil {
		s.fetchAndStoreImages(ctx, item.ID, meta.ExternalIDs, itemType)
	}

	log.Info("enriched metadata", "title", item.Title, "tmdb_id", primaryExternalID, "year", item.Year)
}

// itemTypeForProvider mapea el tipo interno de un item al tipo que entiende
// el provider de metadatos. Movies → ItemMovie, series/episodios → ItemSeries
// (TMDb agrupa los episodios bajo /tv/{id}, el episodio concreto se resuelve
// con GetEpisodeMetadata aparte).
func itemTypeForProvider(itemType string) provider.ItemType {
	if itemType == "series" || itemType == "season" || itemType == "episode" {
		return provider.ItemSeries
	}
	return provider.ItemMovie
}
