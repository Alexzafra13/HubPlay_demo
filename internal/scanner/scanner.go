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

// providerFetcher es el trozo de provider.Manager que el scanner necesita.
// Lo definimos como interfaz para poder mockearlo en los tests.
type providerFetcher interface {
	SearchMetadata(ctx context.Context, query provider.SearchQuery) ([]provider.SearchResult, error)
	FetchMetadata(ctx context.Context, externalID string, itemType provider.ItemType) (*provider.MetadataResult, error)
	FetchImages(ctx context.Context, ids map[string]string, itemType provider.ItemType) ([]provider.ImageResult, error)
	FetchEpisodeMetadata(ctx context.Context, showExternalID string, seasonNumber, episodeNumber int) (*provider.EpisodeMetadataResult, error)
	FetchSeasonMetadata(ctx context.Context, showExternalID string, seasonNumber int) (*provider.SeasonMetadataResult, error)
}

// Scanner recorre las rutas de una biblioteca y crea o actualiza los
// elementos en la base de datos.
//
// imageDir y pathmap son opcionales. Si están, el scanner descarga las
// imágenes a local; si no, se salta esa parte. Nunca guarda URLs
// remotas, porque cargarlas filtraría la IP del usuario a TMDb cada vez
// que se viera un póster.
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
	// metaLocks protege los items que un humano ha tocado (identify
	// manual o editor) de que un refresh automático los pise. nil-safe:
	// si no se cablea, todos los items son refrescables como antes.
	metaLocks   *db.ItemMetadataLockRepository
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
	metaLocks *db.ItemMetadataLockRepository,
	providers *provider.Manager,
	prober probe.Prober,
	bus *event.Bus,
	imageDir string,
	pm *pathmap.Store,
	logger *slog.Logger,
) *Scanner {
	// Por fuera aceptamos *provider.Manager para que el wiring en main.go
	// sea claro; por dentro lo guardamos como interfaz pequeña para los tests.
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
		metaLocks:   metaLocks,
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

	// Apuntamos los paths ya conocidos (para detectar lo que ya no está) y de
	// paso cargamos en cache las series y temporadas que ya están en BD.
	existingPaths := make(map[string]bool)
	cache := newShowCache()
	// Guardamos también las temporadas para repasarlas al final, cuando ya
	// se ha enriquecido la serie padre (que es la que tiene el id de TMDb).
	// Los episodios se curan solos al procesar el fichero, pero las
	// temporadas no tienen fichero asociado.
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

	// Repaso final de temporadas que no tienen metadatos. Si ya tienen
	// imágenes, no hace nada — es barato. Importante para quien escaneó
	// sin TMDb configurado.
	for _, season := range existingSeasons {
		s.enrichIfMissing(ctx, season)
	}

	// Marcar como no disponibles los ficheros que ya no están en disco.
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

// iterateLibraryItems recorre todos los elementos de la biblioteca
// (series, temporadas, episodios, películas, audio) llamando a fn por
// cada uno. Enumeramos por tipo a propósito, porque el filtro por
// defecto sólo trae los raíz y se nos escaparían temporadas y episodios.
//
// La paginación se basa en el tamaño real devuelto, no en el que
// pedimos, porque el filtro recorta internamente a 100.
func (s *Scanner) iterateLibraryItems(ctx context.Context, libraryID string, fn func(*librarymodel.Item)) error {
	const pageSize = 100
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
	// Resolvemos la raíz a su ruta real para validar luego los enlaces simbólicos.
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

		// Resolvemos enlaces simbólicos para evitar que apunten fuera de la biblioteca.
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

		// Aviso de progreso cada 50 ficheros: suficiente para que un disco
		// lento parezca vivo, sin saturar al SSD que va a cientos por segundo.
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

func (s *Scanner) processFile(ctx context.Context, lib *librarymodel.Library, libRoot, path string, cache *showCache, result *ScanResult) error {
	existing, err := s.items.GetByPath(ctx, path)
	if err == nil {
		fp, fpErr := fingerprint(path)
		if fpErr != nil {
			return fpErr
		}
		if existing.Fingerprint == fp && existing.IsAvailable {
			// Fichero sin cambios. Intentamos rellenar metadatos si faltan
			// (p.ej. porque al escanear no había clave de TMDb).
			s.enrichIfMissing(ctx, existing)
			return nil
		}
		// Fichero cambiado o que no estaba disponible: lo volvemos a leer.
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

	// En bibliotecas de series construimos la jerarquía
	// serie → temporada → episodio. La serie y la temporada se crean la
	// primera vez que aparecen y se guardan en caché para el resto del scan.
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
		// Si no se reconoce la estructura de carpetas, el episodio queda
		// suelto sin serie padre. A propósito: mejor verlo aunque sea así
		// que perderlo.
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

	// El repositorio de capítulos es opcional (tests viejos no lo cablean).
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

	// Películas, series y audio se buscan por título. Los episodios no, porque
	// el título tipo "Breaking.Bad.S01E01" no encuentra nada en TMDb: se piden
	// directamente usando el id de TMDb de la serie padre.
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

	// Re-leemos capítulos: una recodificación puede haber movido los marcadores.
	// Replace borra los viejos antes de insertar, así un fichero sin capítulos
	// también limpia los que tenía antes.
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

// probeResultToChapters convierte los capítulos del probe al formato de
// BD. Si no hay capítulos devuelve nil; quien la llama puede pasárselo
// directamente a Replace, que ya trata el caso vacío.
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
	// Title = nombre canónico que ve el usuario. Antes este flujo
	// SÓLO actualizaba OriginalTitle y dejaba Title con el nombre
	// crudo del filename ("Toy Story (2020).mkv" → Title="Toy Story
	// (2020)") — por eso los grids enseñaban el filename aunque el
	// match contra TMDb hubiera funcionado. El identify manual SÍ lo
	// hacía, sólo el scan automático estaba roto.
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
	item.UpdatedAt = time.Now()
	if err := s.items.Update(ctx, item); err != nil {
		s.logger.Warn("failed to update item with metadata", "id", item.ID, "error", err)
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
		s.logger.Warn("failed to store metadata", "id", item.ID, "error", err)
	}

	// Replicamos los géneros en una tabla aparte para que los filtros de
	// /movies y /series puedan buscar por índice en vez de escanear el JSON.
	// Es replace: si TMDb deja de devolver un género, el chip desaparece.
	if s.itemValues != nil {
		if err := s.itemValues.SetGenres(ctx, item.ID, meta.Genres); err != nil {
			s.logger.Warn("failed to mirror genres into item_values", "id", item.ID, "error", err)
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
			s.logger.Warn("failed to ensure studio", "id", item.ID, "studio", meta.Studio, "error", sErr)
		} else if err := s.studios.SetItemStudio(ctx, item.ID, studioID); err != nil {
			s.logger.Warn("failed to link item to studio", "id", item.ID, "studio_id", studioID, "error", err)
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

	s.logger.Info("enriched metadata", "title", item.Title, "tmdb_id", primaryExternalID, "year", item.Year)
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
	_ = s.images.DeleteByItem(ctx, item.ID)
	_ = s.metadata.Delete(ctx, item.ID)

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
		item.UpdatedAt = time.Now()
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

// fetchAndStoreImages descarga la mejor candidata de cada tipo de imagen
// (póster, fondo, logo) y la guarda en local. Si una imagen falla, se
// loguea y se sigue — perder un póster es mejor que tumbar todo el scan.
func (s *Scanner) fetchAndStoreImages(ctx context.Context, itemID string, externalIDs map[string]string, itemType provider.ItemType) {
	results, err := s.providers.FetchImages(ctx, externalIDs, itemType)
	if err != nil {
		s.logger.Debug("provider image fetch failed", "id", itemID, "error", err)
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
			s.logger.Warn("scanner: image ingest failed", "id", itemID, "kind", kind, "error", err)
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
			s.logger.Warn("scanner: failed to store image row", "id", itemID, "kind", kind, "error", err)
			_ = os.Remove(ing.LocalPath)
			continue
		}
		if err := s.pathmap.Write(imgID, ing.LocalPath); err != nil {
			s.logger.Warn("scanner: pathmap write failed", "id", imgID, "error", err)
		}
	}
}


// enrichSeason pide los metadatos de una temporada usando el id de TMDb
// de la serie padre. Si falla algo (sin id, sin provider, 404 en TMDb),
// se deja como está y el siguiente scan lo intenta de nuevo.
//
// Siempre reemplazamos el título "Temporada N" por lo que diga TMDb,
// para arreglar duplicados tipo "Season 1 / Season 1" que se colaban
// cuando el placeholder se creó sin id de TMDb.
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

// fetchAndStoreSeasonPoster descarga el póster de una temporada y lo
// guarda como imagen principal de esa temporada. Funciona igual que el
// equivalente para episodios.
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

// enrichEpisode pide los metadatos de un episodio usando el id de TMDb
// de la serie padre. Si falla, el episodio queda visible sin metadatos y
// el siguiente scan lo reintenta.
//
// seasonItemID es la temporada del episodio; subimos un nivel para
// llegar a la serie y leer ahí el id de TMDb.
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
		// La serie aún no tiene id de TMDb; lo intentamos en el siguiente scan.
		return
	}

	meta, err := s.providers.FetchEpisodeMetadata(ctx, tmdbID, seasonNum, episodeNum)
	if err != nil || meta == nil {
		s.logger.Debug("episode enrich: provider returned nothing", "tmdb_id", tmdbID, "season", seasonNum, "episode", episodeNum, "error", err)
		return
	}

	// Si TMDb da título, lo usamos: suele ser más limpio que algo tipo
	// "S01E01". No pisamos nombres curados a mano que ya estuvieran.
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
	// La duración de TMDb redondea a minutos y no es precisa. Sólo la
	// usamos si el análisis del fichero no devolvió duración.
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
