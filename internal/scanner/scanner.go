package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/imaging"
	"hubplay/internal/imaging/pathmap"
	"hubplay/internal/probe"
	"hubplay/internal/provider"

	"github.com/google/uuid"
)

// Extensiones de media conocidas.
var mediaExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".mov": true, ".wmv": true,
	".flv": true, ".webm": true, ".m4v": true, ".ts": true, ".mpg": true,
	".mpeg": true, ".3gp": true, ".ogv": true,
	// Audio
	".mp3": true, ".flac": true, ".aac": true, ".ogg": true, ".wma": true,
	".wav": true, ".m4a": true, ".opus": true, ".alac": true,
}

func IsMediaFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return mediaExtensions[ext]
}

// providerFetcher: subset de provider.Manager que el scanner usa. Interfaz
// para que un test pueda mockear sin construir el Manager completo (mismo
// patrón que ImageRefresher con ImageRefresherProvider).
type providerFetcher interface {
	SearchMetadata(ctx context.Context, query provider.SearchQuery) ([]provider.SearchResult, error)
	FetchMetadata(ctx context.Context, externalID string, itemType provider.ItemType) (*provider.MetadataResult, error)
	FetchImages(ctx context.Context, ids map[string]string, itemType provider.ItemType) ([]provider.ImageResult, error)
	FetchEpisodeMetadata(ctx context.Context, showExternalID string, seasonNumber, episodeNumber int) (*provider.EpisodeMetadataResult, error)
	FetchSeasonMetadata(ctx context.Context, showExternalID string, seasonNumber int) (*provider.SeasonMetadataResult, error)
}

// Scanner: recorre paths de biblioteca y crea/actualiza items en DB.
//
// `imageDir` y `pathmap` opcionales: si ambos cableados, el scanner descarga
// el artwork a local en lugar de persistir URLs remotas. Si alguno es nil
// (tests viejos), se omite el image enrichment silenciosamente — NUNCA
// persiste URLs remotas que filtrarían la IP del user a TMDb en cada poster.
type Scanner struct {
	items       *db.ItemRepository
	streams     *db.MediaStreamRepository
	metadata    *db.MetadataRepository
	externalIDs *db.ExternalIDRepository
	images      *db.ImageRepository
	chapters    *db.ChapterRepository
	people      *db.PeopleRepository
	itemValues  *db.ItemValueRepository
	studios     *db.StudioRepository
	collections *db.CollectionRepository
	providers   providerFetcher
	prober      probe.Prober
	bus         *event.Bus
	imageDir    string
	pathmap     *pathmap.Store
	logger      *slog.Logger
}

func New(
	items *db.ItemRepository,
	streams *db.MediaStreamRepository,
	metadata *db.MetadataRepository,
	externalIDs *db.ExternalIDRepository,
	images *db.ImageRepository,
	chapters *db.ChapterRepository,
	people *db.PeopleRepository,
	itemValues *db.ItemValueRepository,
	studios *db.StudioRepository,
	collections *db.CollectionRepository,
	providers *provider.Manager,
	prober probe.Prober,
	bus *event.Bus,
	imageDir string,
	pm *pathmap.Store,
	logger *slog.Logger,
) *Scanner {
	// providers tipado como *provider.Manager en la API pública para que el
	// wiring en main.go sea obvio; internamente lo guardamos bajo interfaz
	// pequeña para poder fakear en tests.
	var pf providerFetcher
	if providers != nil {
		pf = providers
	}
	return &Scanner{
		items:       items,
		streams:     streams,
		metadata:    metadata,
		externalIDs: externalIDs,
		images:      images,
		chapters:    chapters,
		people:      people,
		itemValues:  itemValues,
		studios:     studios,
		collections: collections,
		providers:   pf,
		prober:      prober,
		bus:         bus,
		imageDir:    imageDir,
		pathmap:     pm,
		logger:      logger.With("module", "scanner"),
	}
}

type ScanResult struct {
	Added   int
	Updated int
	Removed int
	Errors  int
	Elapsed time.Duration
}

func (s *Scanner) ScanLibrary(ctx context.Context, lib *librarymodel.Library) (*ScanResult, error) {
	start := time.Now()
	result := &ScanResult{}

	s.bus.Publish(event.Event{
		Type: event.LibraryScanStarted,
		Data: map[string]any{"library_id": lib.ID, "library_name": lib.Name},
	})

	// Colecta paths existentes (para detectar removals) y, en la misma pasada,
	// pre-puebla el showCache (series + season ya en DB) para no re-crearlos.
	existingPaths := make(map[string]bool)
	cache := newShowCache()
	// Snapshot de seasons existentes: el self-heal corre AL FINAL, cuando el
	// walker ya ha enriquecido las series (que llevan el tmdb id). Episodes
	// se auto-curan vía processFile→enrichIfMissing, pero seasons no tienen
	// path → processFile no las visita.
	var existingSeasons []*librarymodel.Item
	if err := s.iterateLibraryItems(ctx, lib.ID, func(item *librarymodel.Item) {
		if item.Path != "" {
			existingPaths[item.Path] = true
		}
		switch item.Type {
		case "series":
			cache.rememberSeries(item.Title, item.ID)
		case "season":
			if item.ParentID != "" && item.SeasonNumber != nil {
				cache.rememberSeason(item.ParentID, *item.SeasonNumber, item.ID)
				copy := *item
				existingSeasons = append(existingSeasons, &copy)
			}
		}
	}); err != nil {
		return nil, fmt.Errorf("listing existing items: %w", err)
	}

	seenPaths := make(map[string]bool)
	for _, libPath := range lib.Paths {
		if err := s.walkPath(ctx, lib, libPath, seenPaths, cache, result); err != nil {
			s.logger.Error("error walking path", "path", libPath, "error", err)
			result.Errors++
		}
	}

	// Self-heal de seasons pre-existentes sin metadata. enrichIfMissing es
	// no-op si ya hay imágenes — barato en libraries enriquecidas. Crítico
	// para usuarios que escanearon sin TMDb: la SeasonGrid necesita el poster.
	for _, season := range existingSeasons {
		s.enrichIfMissing(ctx, season)
	}

	// Marcar ficheros que ya no aparecen como unavailable.
	for path := range existingPaths {
		if !seenPaths[path] {
			item, err := s.items.GetByPath(ctx, path)
			if err != nil {
				continue
			}
			if item.IsAvailable {
				item.IsAvailable = false
				item.UpdatedAt = time.Now()
				if err := s.items.Update(ctx, item); err == nil {
					result.Removed++
					s.bus.Publish(event.Event{
						Type: event.ItemRemoved,
						Data: map[string]any{"item_id": item.ID, "path": path},
					})
				}
			}
		}
	}

	result.Elapsed = time.Since(start)

	s.bus.Publish(event.Event{
		Type: event.LibraryScanCompleted,
		Data: map[string]any{
			"library_id": lib.ID,
			"added":      result.Added,
			"updated":    result.Updated,
			"removed":    result.Removed,
			"errors":     result.Errors,
			"elapsed_ms": result.Elapsed.Milliseconds(),
		},
	})

	s.logger.Info("scan complete",
		"library", lib.Name,
		"added", result.Added,
		"updated", result.Updated,
		"removed", result.Removed,
		"errors", result.Errors,
		"elapsed", result.Elapsed,
	)

	return result, nil
}

// iterateLibraryItems: pagina TODOS los items (series, season, episode,
// movie, audio) llamando fn por cada uno. Enumera por tipo a propósito —
// el ItemFilter por defecto sólo trae root items (parent_id IS NULL), lo
// que silenciaba todas las seasons + episodes.
//
// La paginación usa el len() real del slice devuelto, NO el pageSize pedido:
// ItemFilter.List capa Limit a 100 internamente, así que pedir 500 devuelve
// 100 y `len < requested` siempre dispararía tras la primera batch (bug
// histórico que perdía entradas de cache en libraries >100 items).
func (s *Scanner) iterateLibraryItems(ctx context.Context, libraryID string, fn func(*librarymodel.Item)) error {
	const pageSize = 100 // tope interno de ItemFilter
	for _, t := range []string{"series", "season", "episode", "movie", "audio"} {
		offset := 0
		for {
			items, _, err := s.items.List(ctx, librarymodel.ItemFilter{
				LibraryID: libraryID,
				Type:      t,
				Limit:     pageSize,
				Offset:    offset,
			})
			if err != nil {
				return err
			}
			for _, item := range items {
				fn(item)
			}
			if len(items) < pageSize {
				break
			}
			offset += pageSize
		}
	}
	return nil
}

func (s *Scanner) walkPath(ctx context.Context, lib *librarymodel.Library, root string, seenPaths map[string]bool, cache *showCache, result *ScanResult) error {
	// Resolución a path absoluto real para los checks de boundary de symlinks.
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolving library root %q: %w", root, err)
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return fmt.Errorf("resolving library root symlinks %q: %w", root, err)
	}

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			s.logger.Warn("walk error", "path", path, "error", err)
			return nil // seguir el walk
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if !IsMediaFile(path) {
			return nil
		}

		// Resuelve symlinks para evitar path-traversal.
		realPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			s.logger.Warn("cannot resolve symlink, skipping", "path", path, "error", err)
			return nil
		}
		if !strings.HasPrefix(realPath, realRoot+string(os.PathSeparator)) && realPath != realRoot {
			s.logger.Warn("symlink target outside library root, skipping",
				"path", path, "target", realPath, "root", realRoot)
			return nil
		}

		seenPaths[path] = true

		if err := s.processFile(ctx, lib, root, path, cache, result); err != nil {
			s.logger.Warn("error processing file", "path", path, "error", err)
			result.Errors++
		}

		// Progress cada 50 ficheros. Publish es async (barato), y suficiente
		// para que un disco lento (5 fps) sienta live en el panel admin sin
		// floodear en SSD (cientos/seg). El relative path muestra "scanning
		// Action/2024/foo.mkv" — más informativo que un contador pelado.
		if len(seenPaths)%50 == 0 {
			rel, _ := filepath.Rel(root, path)
			s.bus.Publish(event.Event{
				Type: event.LibraryScanProgress,
				Data: map[string]any{
					"library_id":   lib.ID,
					"library_name": lib.Name,
					"scanned":      len(seenPaths),
					"current_path": rel,
				},
			})
		}
		return nil
	})
}

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
		// Borra images/metadata para que enrichment las recree. Best-effort:
		// un delete fallido aún deja que Upsert sobrescriba.
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

func (s *Scanner) processFile(ctx context.Context, lib *librarymodel.Library, libRoot, path string, cache *showCache, result *ScanResult) error {
	existing, err := s.items.GetByPath(ctx, path)
	if err == nil {
		fp, fpErr := fingerprint(path)
		if fpErr != nil {
			return fpErr
		}
		if existing.Fingerprint == fp && existing.IsAvailable {
			// Fichero sin cambios — re-enrich si falta metadata
			// (p.ej. provider API key añadida después del scan inicial).
			s.enrichIfMissing(ctx, existing)
			return nil
		}
		// Fichero cambiado o estaba unavailable — re-probe + update.
		return s.updateItem(ctx, existing, path, fp, result)
	}

	return s.createItem(ctx, lib, libRoot, path, cache, result)
}

func (s *Scanner) createItem(ctx context.Context, lib *librarymodel.Library, libRoot, path string, cache *showCache, result *ScanResult) error {
	probeResult, err := s.prober.Probe(ctx, path)
	if err != nil {
		return fmt.Errorf("probing %q: %w", path, err)
	}

	fp, err := fingerprint(path)
	if err != nil {
		return err
	}

	now := time.Now()
	title := titleFromPath(path)
	itemID := uuid.NewString()
	itemType := itemTypeFromLibrary(lib.ContentType)

	// En libraries de shows: jerarquía series → season → episode. Episode
	// apunta a su season vía parent_id; series_id es implícito (episode
	// → season.parent_id → series). Series + season se crean lazy en el
	// primer encuentro y van al cache durante el resto del scan.
	var (
		parentID      string
		seasonNumber  *int
		episodeNumber *int
	)
	if itemType == "episode" {
		match := ParseEpisode(libRoot, path)
		if match.OK {
			sID, err := s.ensureSeriesRow(ctx, lib, cache, match.SeriesName)
			if err != nil {
				s.logger.Warn("failed to ensure series row", "series", match.SeriesName, "error", err)
			} else {
				seasonID, err := s.ensureSeasonRow(ctx, lib, cache, sID, match.SeasonNumber)
				if err != nil {
					s.logger.Warn("failed to ensure season row", "series", match.SeriesName, "season", match.SeasonNumber, "error", err)
				} else {
					parentID = seasonID
					sn := match.SeasonNumber
					en := match.EpisodeNumber
					seasonNumber = &sn
					episodeNumber = &en
					if match.EpisodeTitle != "" {
						title = match.EpisodeTitle
					}
				}
			}
		}
		// match.OK == false (layout plano): episode queda como type=episode
		// sin parent. A propósito — mejor verlo suelto que perderlo.
	}

	item := &librarymodel.Item{
		ID:            itemID,
		LibraryID:     lib.ID,
		ParentID:      parentID,
		Type:          itemType,
		Title:         title,
		SortTitle:     strings.ToLower(title),
		SeasonNumber:  seasonNumber,
		EpisodeNumber: episodeNumber,
		Path:          path,
		Size:          probeResult.Format.Size,
		DurationTicks: probe.DurationTicks(probeResult.Format.Duration),
		Container:     probeResult.Format.FormatName,
		Fingerprint:   fp,
		AddedAt:       now,
		UpdatedAt:     now,
		IsAvailable:   true,
	}

	if err := s.items.Create(ctx, item); err != nil {
		return fmt.Errorf("creating item: %w", err)
	}

	streams := probeResultToStreams(itemID, probeResult)
	if len(streams) > 0 {
		if err := s.streams.ReplaceForItem(ctx, itemID, streams); err != nil {
			s.logger.Warn("failed to store streams", "item_id", itemID, "error", err)
		}
	}

	// chapters es dependencia opcional — tests viejos lo construyen sin él.
	if s.chapters != nil && len(probeResult.Chapters) > 0 {
		if err := s.chapters.Replace(ctx, itemID, probeResultToChapters(probeResult)); err != nil {
			s.logger.Warn("failed to store chapters", "item_id", itemID, "error", err)
		}
	}

	result.Added++
	s.bus.Publish(event.Event{
		Type: event.ItemAdded,
		Data: map[string]any{"item_id": itemID, "title": title, "library_id": lib.ID},
	})

	// Enrichment:
	//   - Movies/series/audio → enrichMetadata (title del item = nombre buscable).
	//   - Episodes → ruta distinta: el search TMDb de enrichMetadata destroza
	//     títulos tipo "Breaking.Bad.S01E01". Vamos directos a /tv/{id}/season/
	//     {n}/episode/{m} usando el tmdb id del parent series.
	if itemType == "episode" {
		if seasonNumber != nil && episodeNumber != nil && parentID != "" {
			s.enrichEpisode(ctx, item, parentID, *seasonNumber, *episodeNumber)
		}
	} else {
		s.enrichMetadata(ctx, item)
	}

	return nil
}

func (s *Scanner) updateItem(ctx context.Context, item *librarymodel.Item, path, fp string, result *ScanResult) error {
	probeResult, err := s.prober.Probe(ctx, path)
	if err != nil {
		return fmt.Errorf("probing %q: %w", path, err)
	}

	item.Size = probeResult.Format.Size
	item.DurationTicks = probe.DurationTicks(probeResult.Format.Duration)
	item.Container = probeResult.Format.FormatName
	item.Fingerprint = fp
	item.IsAvailable = true
	item.UpdatedAt = time.Now()

	if err := s.items.Update(ctx, item); err != nil {
		return fmt.Errorf("updating item: %w", err)
	}

	streams := probeResultToStreams(item.ID, probeResult)
	if len(streams) > 0 {
		if err := s.streams.ReplaceForItem(ctx, item.ID, streams); err != nil {
			s.logger.Warn("failed to update streams", "item_id", item.ID, "error", err)
		}
	}

	// Re-derivar chapters: un re-encode puede haber movido markers. Replace
	// limpia el set viejo transaccionalmente antes de insertar, así un probe
	// con 0 chapters borra markers stale de versiones previas del fichero.
	if s.chapters != nil {
		if err := s.chapters.Replace(ctx, item.ID, probeResultToChapters(probeResult)); err != nil {
			s.logger.Warn("failed to update chapters", "item_id", item.ID, "error", err)
		}
	}

	result.Updated++
	s.bus.Publish(event.Event{
		Type: event.ItemUpdated,
		Data: map[string]any{"item_id": item.ID, "title": item.Title},
	})

	return nil
}

// probeResultToChapters: shape de probe → DB (item_id-keyed, ticks).
// Nil input → nil output: caller puede pasarlo directo a Replace, que ya
// trata el caso vacío transaccionalmente.
func probeResultToChapters(pr *probe.Result) []librarymodel.Chapter {
	if len(pr.Chapters) == 0 {
		return nil
	}
	out := make([]librarymodel.Chapter, len(pr.Chapters))
	for i, c := range pr.Chapters {
		out[i] = librarymodel.Chapter{
			StartTicks: probe.DurationTicks(c.Start),
			EndTicks:   probe.DurationTicks(c.End),
			Title:      c.Title,
		}
	}
	return out
}

func probeResultToStreams(itemID string, pr *probe.Result) []*librarymodel.MediaStream {
	var streams []*librarymodel.MediaStream
	for _, s := range pr.Streams {
		streams = append(streams, &librarymodel.MediaStream{
			ItemID:            itemID,
			StreamIndex:       s.Index,
			StreamType:        s.CodecType,
			Codec:             s.CodecName,
			Profile:           s.Profile,
			Bitrate:           s.BitRate,
			Width:             s.Width,
			Height:            s.Height,
			FrameRate:         s.FrameRate,
			HDRType:           s.HDRType,
			ColorSpace:        s.ColorSpace,
			Channels:          s.Channels,
			SampleRate:        s.SampleRate,
			Language:          s.Language,
			Title:             s.Title,
			IsDefault:         s.IsDefault,
			IsForced:          s.IsForced,
			IsHearingImpaired: s.IsHearingImpaired,
		})
	}
	return streams
}

func titleFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

// yearPattern: matchea (2023), [2023], o 2023 suelto.
var yearPattern = regexp.MustCompile(`[\(\[\s]?((?:19|20)\d{2})[\)\]\s]?`)

// parseTitleYear: separa título y año.
// Ej: "Transformers El despertar (2023)" → ("Transformers El despertar", 2023)
//     "Toy Story 3 [2010]"               → ("Toy Story 3", 2010)
func parseTitleYear(filename string) (string, int) {
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filepath.Base(filename), ext)

	// El último match suele ser el año de release.
	matches := yearPattern.FindAllStringSubmatchIndex(name, -1)
	if len(matches) == 0 {
		return name, 0
	}

	last := matches[len(matches)-1]
	yearStr := name[last[2]:last[3]]
	year, _ := strconv.Atoi(yearStr)

	// Título = todo lo anterior al match del año.
	title := strings.TrimSpace(name[:last[0]])
	if title == "" {
		title = name
	}

	return title, year
}

// enrichIfMissing: re-enrich para items sin metadata (p.ej. TMDb sin API key
// en el scan inicial, o parent series sin enrich cuando se creó el episode).
func (s *Scanner) enrichIfMissing(ctx context.Context, item *librarymodel.Item) {
	if s.providers == nil {
		return
	}
	// Si ya tiene imágenes, lo damos por enriquecido.
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

// enrichMetadata: busca en TMDb y guarda metadata + imágenes.
//
// Sólo series y movies van a TMDb. Episodes/seasons se saltan a propósito:
// sus títulos son ruidosos ("Pilot", "Breaking.Bad.S01E01") y un lookup
// per-episode de una serie de 100 caps quemaría 100 search calls para
// resultados que nunca mostramos (el UI usa posters de series). Este guard
// es lo que evita que "Refresh metadata" funda la quota de TMDb.
func (s *Scanner) enrichMetadata(ctx context.Context, item *librarymodel.Item) {
	if s.providers == nil {
		return
	}
	if item.Type == "episode" || item.Type == "season" {
		return
	}

	cleanTitle, year := parseTitleYear(item.Title)
	if year == 0 {
		year = item.Year
	}

	itemType := provider.ItemMovie
	if item.Type == "episode" || item.Type == "series" {
		itemType = provider.ItemSeries
	}

	results, err := s.providers.SearchMetadata(ctx, provider.SearchQuery{
		Title:    cleanTitle,
		Year:     year,
		ItemType: itemType,
	})
	if err != nil || len(results) == 0 {
		s.logger.Debug("no TMDB results", "title", cleanTitle, "year", year, "error", err)
		return
	}

	best := results[0]

	meta, err := s.providers.FetchMetadata(ctx, best.ExternalID, itemType)
	if err != nil || meta == nil {
		s.logger.Debug("TMDB metadata fetch failed", "id", best.ExternalID, "error", err)
		return
	}

	if meta.Title != "" {
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
	item.UpdatedAt = time.Now()
	if err := s.items.Update(ctx, item); err != nil {
		s.logger.Warn("failed to update item with metadata", "id", item.ID, "error", err)
	}

	// Metadata extendida. TrailerKey/Site vienen de la misma call de TMDb
	// (videos appended) — sin round-trip extra para el YouTube id.
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
		s.logger.Warn("failed to store metadata", "id", item.ID, "error", err)
	}

	// Replica géneros en el tag store normalizado para que los filtros de
	// /movies y /series usen lookup indexado en vez de escanear el JSON.
	// Replace-semantics: TMDb refresh que tira un género tira el chip.
	if s.itemValues != nil {
		if err := s.itemValues.SetGenres(ctx, item.ID, meta.Genres); err != nil {
			s.logger.Warn("failed to mirror genres into item_values", "id", item.ID, "error", err)
		}
	}

	// Enlaza al studio (producer/network) para que el brand mark del detalle
	// haga deep-link a la página per-studio. Studio="" → studio_id NULL,
	// sin chip ni entrada en /studios.
	if s.studios != nil {
		var tmdbIDPtr *int64
		if meta.StudioTMDBID > 0 {
			tmdbIDPtr = &meta.StudioTMDBID
		}
		studioID, sErr := s.studios.EnsureStudio(ctx, meta.Studio, meta.StudioLogoURL, tmdbIDPtr)
		if sErr != nil {
			s.logger.Warn("failed to ensure studio", "id", item.ID, "studio", meta.Studio, "error", sErr)
		} else if err := s.studios.SetItemStudio(ctx, item.ID, studioID); err != nil {
			s.logger.Warn("failed to link item to studio", "id", item.ID, "studio_id", studioID, "error", err)
		}
	}

	// Enlaza la movie a su collection (saga) — X-Men/MCU/Toy Story estilo
	// Jellyfin en /collections/{id}. Items TV nunca tienen
	// belongs_to_collection (TMDb scope = movies): CollectionTMDBID=0,
	// link NULL.
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
			s.logger.Warn("failed to ensure collection", "id", item.ID, "collection", meta.CollectionName, "error", cErr)
		} else if err := s.collections.SetItemCollection(ctx, item.ID, collectionID); err != nil {
			s.logger.Warn("failed to link item to collection", "id", item.ID, "collection_id", collectionID, "error", err)
		}
	}

	for prov, extID := range meta.ExternalIDs {
		if err := s.externalIDs.Upsert(ctx, &librarymodel.ExternalID{
			ItemID:     item.ID,
			Provider:   prov,
			ExternalID: extID,
		}); err != nil {
			s.logger.Warn("failed to store external id", "id", item.ID, "provider", prov, "error", err)
		}
	}

	// Cast/crew best-effort: fallos en syncPeople se loguean, no paran el scan.
	s.syncPeople(ctx, item.ID, meta.People)

	// Imágenes: descargamos cada candidato a local y guardamos
	// `/api/v1/images/file/{id}` como path, NUNCA la URL remota. Persistir
	// URLs filtraría IP/User-Agent del user a TMDb en cada poster y rompería
	// la library el día que TMDb no responda.
	//
	// imageDir + pathmap son opcionales: tests sin pipeline de artwork
	// construyen Scanner sin ellos y el enrichment se omite silencioso
	// en lugar de caer a URL persistence.
	if len(meta.ExternalIDs) > 0 && s.imageDir != "" && s.pathmap != nil {
		s.fetchAndStoreImages(ctx, item.ID, meta.ExternalIDs, itemType)
	}

	s.logger.Info("enriched metadata", "title", item.Title, "tmdb_id", best.ExternalID, "year", item.Year)
}

// fetchAndStoreImages: para cada kind (primary, backdrop, logo) coge el
// candidato mejor puntuado, lo ingesta vía imaging.IngestRemoteImage
// (SSRF + size + blurhash + atomic write) y persiste una Image row apuntando
// al fichero local. Errores per-image se loguean y se saltan — perder un
// poster es mejor que tumbar el scan entero.
func (s *Scanner) fetchAndStoreImages(ctx context.Context, itemID string, externalIDs map[string]string, itemType provider.ItemType) {
	results, err := s.providers.FetchImages(ctx, externalIDs, itemType)
	if err != nil {
		s.logger.Debug("provider image fetch failed", "id", itemID, "error", err)
		return
	}
	if len(results) == 0 {
		return
	}

	// Mejor score por kind — sin esto cogeríamos el primero del merge de
	// providers. Mismo ranking que usa ImageRefresher en refreshes manuales,
	// así re-runs son estables.
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
			s.logger.Warn("scanner: image ingest failed", "id", itemID, "kind", kind, "error", err)
			continue
		}

		imgID := uuid.NewString()
		// Provider name viene del Source que el Manager estampa — sin sniff
		// de URL. Fallback a "unknown" sólo si un provider futuro olvida
		// exponer su nombre (hoy ni TMDb ni Fanart caen aquí).
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
			s.logger.Warn("scanner: failed to store image row", "id", itemID, "kind", kind, "error", err)
			_ = os.Remove(ing.LocalPath)
			continue
		}
		if err := s.pathmap.Write(imgID, ing.LocalPath); err != nil {
			s.logger.Warn("scanner: pathmap write failed", "id", imgID, "error", err)
		}
	}
}


// enrichSeason: metadata per-season vía el tmdb id del parent series.
// Best-effort como enrichEpisode: sin tmdb id, sin provider, o TMDb 404 →
// row intacto y el siguiente scan retry (via checkAndEnrichSeason).
//
// Title overwrite: SIEMPRE reemplaza el placeholder "Season N" por lo que
// devuelva TMDb (incluso si TMDb también dice "Season N"). Arregla el
// "Season 1 / Season 1" duplicado cuando el placeholder se coló sin tmdb id.
func (s *Scanner) enrichSeason(ctx context.Context, item *librarymodel.Item, seriesID string, seasonNum int) {
	if s.providers == nil || s.externalIDs == nil {
		return
	}
	extIDs, err := s.externalIDs.ListByItem(ctx, seriesID)
	if err != nil {
		s.logger.Debug("season enrich: series external_ids lookup failed", "series_id", seriesID, "error", err)
		return
	}
	var tmdbID string
	for _, e := range extIDs {
		if e.Provider == "tmdb" {
			tmdbID = e.ExternalID
			break
		}
	}
	if tmdbID == "" {
		return
	}

	meta, err := s.providers.FetchSeasonMetadata(ctx, tmdbID, seasonNum)
	if err != nil || meta == nil {
		s.logger.Debug("season enrich: provider returned nothing", "tmdb_id", tmdbID, "season", seasonNum, "error", err)
		return
	}

	if meta.Title != "" {
		item.Title = meta.Title
		item.SortTitle = strings.ToLower(meta.Title)
	}
	if meta.Rating != nil {
		item.CommunityRating = meta.Rating
	}
	if meta.PremiereDate != nil {
		item.PremiereDate = meta.PremiereDate
		item.Year = meta.PremiereDate.Year()
	}
	item.UpdatedAt = time.Now()
	if err := s.items.Update(ctx, item); err != nil {
		s.logger.Warn("update season with metadata", "id", item.ID, "error", err)
	}

	if meta.Overview != "" {
		if err := s.metadata.Upsert(ctx, &librarymodel.Metadata{
			ItemID:   item.ID,
			Overview: meta.Overview,
		}); err != nil {
			s.logger.Warn("store season metadata", "id", item.ID, "error", err)
		}
	}

	if meta.PosterURL != "" && s.imageDir != "" && s.pathmap != nil {
		s.fetchAndStoreSeasonPoster(ctx, item.ID, meta.PosterURL)
	}

	s.logger.Info("enriched season metadata", "title", item.Title, "id", item.ID, "tmdb_show", tmdbID, "season", seasonNum, "episodes_known", meta.EpisodeCount)
}

// fetchAndStoreSeasonPoster: ingest de 1 URL de poster TMDb + Image row
// `primary` para la season. Espejo de fetchAndStoreEpisodeStill — mismo
// pipeline SSRF/size/blurhash, distinto Type y target.
func (s *Scanner) fetchAndStoreSeasonPoster(ctx context.Context, itemID, posterURL string) {
	dir := filepath.Join(s.imageDir, itemID)
	ing, err := imaging.IngestRemoteImage(dir, "primary", posterURL, s.logger)
	if err != nil {
		s.logger.Warn("scanner: season poster ingest failed", "id", itemID, "error", err)
		return
	}

	imgID := uuid.NewString()
	dbImg := &librarymodel.Image{
		ID:                 imgID,
		ItemID:             itemID,
		Type:               "primary",
		Path:               "/api/v1/images/file/" + imgID,
		Blurhash:           ing.Blurhash,
		Provider:           "tmdb",
		IsPrimary:          true,
		AddedAt:            time.Now(),
		DominantColor:      ing.DominantColor,
		DominantColorMuted: ing.DominantColorMuted,
	}
	if err := s.images.Create(ctx, dbImg); err != nil {
		s.logger.Warn("scanner: failed to store season poster row", "id", itemID, "error", err)
		_ = os.Remove(ing.LocalPath)
		return
	}
	if err := s.pathmap.Write(imgID, ing.LocalPath); err != nil {
		s.logger.Warn("scanner: pathmap write failed (season poster)", "id", imgID, "error", err)
	}
}

// enrichEpisode: metadata per-episode vía el tmdb id del parent series.
// Best-effort: sin tmdb id, sin provider, o 404 → row visible sin metadata,
// el siguiente scan reintenta (enrichIfMissing chequea ausencia de imágenes).
//
// seasonItemID: row de la season; subimos un link para leer external_ids
// del series. El walker nos lo pasa cuando el match de show-hierarchy va bien.
func (s *Scanner) enrichEpisode(ctx context.Context, item *librarymodel.Item, seasonItemID string, seasonNum, episodeNum int) {
	if s.providers == nil || s.externalIDs == nil {
		return
	}
	season, err := s.items.GetByID(ctx, seasonItemID)
	if err != nil || season == nil || season.ParentID == "" {
		return
	}
	seriesID := season.ParentID

	extIDs, err := s.externalIDs.ListByItem(ctx, seriesID)
	if err != nil {
		s.logger.Debug("episode enrich: series external_ids lookup failed", "series_id", seriesID, "error", err)
		return
	}
	var tmdbID string
	for _, e := range extIDs {
		if e.Provider == "tmdb" {
			tmdbID = e.ExternalID
			break
		}
	}
	if tmdbID == "" {
		// Series sin enriquecer aún. El siguiente scan reintenta cuando la
		// series tenga su tmdb id.
		return
	}

	meta, err := s.providers.FetchEpisodeMetadata(ctx, tmdbID, seasonNum, episodeNum)
	if err != nil || meta == nil {
		s.logger.Debug("episode enrich: provider returned nothing", "tmdb_id", tmdbID, "season", seasonNum, "episode", episodeNum, "error", err)
		return
	}

	// Title swap condicional: el de TMDb suele ser más limpio (S01E01 → "Pilot"),
	// pero no queremos pisar nombres curados a mano. El file-derived sólo se
	// queda si no es un código genérico SxxExx.
	if meta.Title != "" {
		item.Title = meta.Title
		item.SortTitle = strings.ToLower(meta.Title)
	}
	if meta.Rating != nil {
		item.CommunityRating = meta.Rating
	}
	if meta.PremiereDate != nil {
		item.PremiereDate = meta.PremiereDate
		item.Year = meta.PremiereDate.Year()
	}
	// RuntimeMinutes de TMDb no es preciso (redondea a minutos) — el probe
	// sabe el ms. Sólo lo usamos como fallback si el probe no dio duración.
	if item.DurationTicks == 0 && meta.RuntimeMinutes > 0 {
		item.DurationTicks = int64(meta.RuntimeMinutes) * 60 * 10_000_000
	}
	item.UpdatedAt = time.Now()
	if err := s.items.Update(ctx, item); err != nil {
		s.logger.Warn("update episode with metadata", "id", item.ID, "error", err)
	}

	if meta.Overview != "" {
		if err := s.metadata.Upsert(ctx, &librarymodel.Metadata{
			ItemID:   item.ID,
			Overview: meta.Overview,
		}); err != nil {
			s.logger.Warn("store episode metadata", "id", item.ID, "error", err)
		}
	}

	// Guardamos el still como "backdrop" del episode para que el item-detail
	// handler (que indexa por type=backdrop para el hero) lo pinte sin
	// distinguir episode/series client-side. SSRF/size/blurhash van por el
	// mismo IngestRemoteImage que series.
	if meta.StillURL != "" && s.imageDir != "" && s.pathmap != nil {
		s.fetchAndStoreEpisodeStill(ctx, item.ID, meta.StillURL)
	}

	s.logger.Info("enriched episode metadata", "title", item.Title, "id", item.ID, "tmdb_show", tmdbID, "season", seasonNum, "episode", episodeNum)
}

// fetchAndStoreEpisodeStill: ingest de 1 still TMDb + Image row `backdrop`
// del episode. Sin loop per-kind: episodes no tienen poster ni logo en TMDb.
func (s *Scanner) fetchAndStoreEpisodeStill(ctx context.Context, itemID, stillURL string) {
	dir := filepath.Join(s.imageDir, itemID)
	ing, err := imaging.IngestRemoteImage(dir, "backdrop", stillURL, s.logger)
	if err != nil {
		s.logger.Warn("scanner: episode still ingest failed", "id", itemID, "error", err)
		return
	}

	imgID := uuid.NewString()
	dbImg := &librarymodel.Image{
		ID:                 imgID,
		ItemID:             itemID,
		Type:               "backdrop",
		Path:               "/api/v1/images/file/" + imgID,
		Blurhash:           ing.Blurhash,
		Provider:           "tmdb",
		IsPrimary:          true,
		AddedAt:            time.Now(),
		DominantColor:      ing.DominantColor,
		DominantColorMuted: ing.DominantColorMuted,
	}
	if err := s.images.Create(ctx, dbImg); err != nil {
		s.logger.Warn("scanner: failed to store episode still row", "id", itemID, "error", err)
		_ = os.Remove(ing.LocalPath)
		return
	}
	if err := s.pathmap.Write(imgID, ing.LocalPath); err != nil {
		s.logger.Warn("scanner: pathmap write failed (episode still)", "id", imgID, "error", err)
	}
}

// itemTypeFromLibrary: library.content_type → item.type.
func itemTypeFromLibrary(contentType string) string {
	switch contentType {
	case "movies":
		return "movie"
	case "shows":
		return "episode"
	case "music":
		return "audio"
	default:
		return "movie"
	}
}

// fingerprint: size + sha256 de los primeros 64 KB — barato y bastante único.
func fingerprint(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %q: %w", path, err)
	}
	defer f.Close() //nolint:errcheck

	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat %q: %w", path, err)
	}

	h := sha256.New()
	if _, err := io.CopyN(h, f, 65536); err != nil && err != io.EOF {
		return "", fmt.Errorf("hashing %q: %w", path, err)
	}

	return fmt.Sprintf("%d:%x", info.Size(), h.Sum(nil)[:16]), nil
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
