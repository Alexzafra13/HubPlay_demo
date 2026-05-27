package scanner

// Top-level del scanner: struct + constructor + entry points
// (`ScanLibrary`, `iterateLibraryItems`, `walkPath`). El detalle del
// procesamiento por fichero, enrichment de TMDb, identify manual e
// ingest de imágenes/people vive en ficheros hermanos del mismo paquete
// (`scan_walk.go`, `enrich.go`, `enrich_season_episode.go`, `identify.go`,
// `media_ingest.go`). Split por responsabilidad — el código sigue
// montado contra el mismo `*Scanner`, sólo se ha dispersado en ficheros
// temáticos para que no sean 1500 LoC en uno solo (olor W del audit
// 2026-05-14).

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hubplay/internal/clock"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/imaging/pathmap"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/probe"
	"hubplay/internal/provider"
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
	metaLocks *db.ItemMetadataLockRepository
	providers providerFetcher
	prober    probe.Prober
	bus       *event.Bus
	imageDir  string
	pathmap   *pathmap.Store
	logger    *slog.Logger
	clock     clock.Clock
}

// Config agrupa los 17 parámetros que necesita el scanner. Antes era
// un constructor posicional difícil de leer; ahora cada campo se nombra
// en el call site y los nuevos opcionales (clock) tienen default sin
// romper callers existentes.
type Config struct {
	Items       *db.ItemRepository
	Streams     *db.MediaStreamRepository
	Metadata    *db.MetadataRepository
	ExternalIDs *db.ExternalIDRepository
	Images      *db.ImageRepository
	Chapters    *db.ChapterRepository
	People      *db.PeopleRepository
	ItemValues  *db.ItemValueRepository
	Studios     *db.StudioRepository
	Collections *db.CollectionRepository
	MetaLocks   *db.ItemMetadataLockRepository
	Providers   *provider.Manager
	Prober      probe.Prober
	Bus         *event.Bus
	ImageDir    string
	Pathmap     *pathmap.Store
	Logger      *slog.Logger
	// Clock opcional — default `clock.New()` (tiempo real). Inyectable
	// para tests determinísticos.
	Clock clock.Clock
}

func New(cfg Config) *Scanner {
	// Por fuera aceptamos *provider.Manager para que el wiring en main.go
	// sea claro; por dentro lo guardamos como interfaz pequeña para los tests.
	var pf providerFetcher
	if cfg.Providers != nil {
		pf = cfg.Providers
	}
	clk := cfg.Clock
	if clk == nil {
		clk = clock.New()
	}
	return &Scanner{
		items:       cfg.Items,
		streams:     cfg.Streams,
		metadata:    cfg.Metadata,
		externalIDs: cfg.ExternalIDs,
		images:      cfg.Images,
		chapters:    cfg.Chapters,
		people:      cfg.People,
		itemValues:  cfg.ItemValues,
		studios:     cfg.Studios,
		collections: cfg.Collections,
		metaLocks:   cfg.MetaLocks,
		providers:   pf,
		prober:      cfg.Prober,
		bus:         cfg.Bus,
		imageDir:    cfg.ImageDir,
		pathmap:     cfg.Pathmap,
		logger:      cfg.Logger.With("module", "scanner"),
		clock:       clk,
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
	start := s.clock.Now()
	result := &ScanResult{}
	log := s.logger.With("library_id", lib.ID)

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
			log.Error("error walking path", "path", libPath, "error", err)
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
				item.UpdatedAt = s.clock.Now()
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

	result.Elapsed = s.clock.Now().Sub(start)

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

	log.Info("scan complete",
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
