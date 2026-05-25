package library

import (
	"context"
	"log/slog"

	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/imaging/pathmap"
	"hubplay/internal/probe"
	"hubplay/internal/provider"
	"hubplay/internal/scanner"
)

// Module agrupa los componentes long-lived del feature library —
// scanner + service + dos schedulers (scan + image refresh) + dos
// detectores de skip-intro (chapter + audio fingerprint) + filesystem
// watcher. Reemplaza el bloque de ~90 LoC que `cmd/hubplay/main.go`
// ejecutaba inline para cablearlos.
//
// Cierra la fase library del olor G del audit 2026-05-14. Con esta
// pieza, junto a la fase iptv ya cerrada (PR #417) y `lifecycle.go`
// (PR #396), el olor queda cerrado al 100 %.
type Module struct {
	Scanner               *scanner.Scanner
	Service               *Service
	ScanScheduler         *Scheduler
	ImageRefresher        *ImageRefresher
	ImageRefreshScheduler *ImageRefreshScheduler
	Fingerprinter         *Fingerprinter
	SegmentDetector       *SegmentDetector
	SegmentFingerprinter  *SegmentFingerprinter
	FSWatcher             *FSWatcher

	// Handles de unsubscribe que Start devuelve. Los dos detectores
	// drenan sus goroutines de bus-handler en estos cierres (audit
	// olor Y) — hay que llamarlos en shutdown para no leakearlos.
	segmentDetectorUnsub      func()
	segmentFingerprinterUnsub func()

	// fsWatcherStarted indica si el FSWatcher arrancó OK. Si fue
	// fail-soft (sin inotify / equivalente), no hay nada que parar
	// en shutdown.
	fsWatcherStarted bool
}

// Deps es la entrada explícita a New. Mantiene library libre de
// dependencias hacia config — main resuelve los paths derivados
// (imageDir, fingerprint cache dir) y los pasa pre-resueltos.
type Deps struct {
	// Repos compartidos con scanner.
	Libraries         *db.LibraryRepository
	Items             *db.ItemRepository
	MediaStreams      *db.MediaStreamRepository
	Metadata          *db.MetadataRepository
	ExternalIDs       *db.ExternalIDRepository
	Images            *db.ImageRepository
	Chapters          *db.ChapterRepository
	EpisodeSegments   *db.EpisodeSegmentRepository
	People            *db.PeopleRepository
	ItemValues        *db.ItemValueRepository
	Studios           *db.StudioRepository
	Collections       *db.CollectionRepository
	ItemMetadataLocks *db.ItemMetadataLockRepository
	Channels          *db.ChannelRepository

	// Singletons compartidos con otras features.
	Providers *provider.Manager
	Prober    probe.Prober
	EventBus  *event.Bus
	Pathmap   *pathmap.Store

	// Paths derivados.
	ImageDir              string // raíz de imágenes locales (poster/backdrop)
	FingerprintCacheDir   string // workdir de chromaprint (fpcalc)

	Logger *slog.Logger
}

// New construye el Module en el orden correcto, aplica el cross-wiring
// entre piezas (library.Service recibe el *scanner.Scanner;
// segment-detector + fingerprinter se suscriben al event bus en
// `library.scan.completed`) y arranca los workers contra el ctx
// pasado.
//
// El FSWatcher es fail-soft: si su Start falla (Docker on Windows con
// bind mounts no soporta inotify), el error se loggea Warn y el
// scheduler periódico sigue siendo la fuente de truth.
func New(ctx context.Context, deps Deps) (*Module, error) {
	scnr := scanner.New(
		deps.Items,
		deps.MediaStreams,
		deps.Metadata,
		deps.ExternalIDs,
		deps.Images,
		deps.Chapters,
		deps.People,
		deps.ItemValues,
		deps.Studios,
		deps.Collections,
		deps.ItemMetadataLocks,
		deps.Providers,
		deps.Prober,
		deps.EventBus,
		deps.ImageDir,
		deps.Pathmap,
		deps.Logger,
	)

	svc := NewService(
		deps.Libraries,
		deps.Items,
		deps.MediaStreams,
		deps.Images,
		deps.Channels,
		deps.ItemValues,
		scnr,
		deps.Logger,
	)

	scanSched := NewScheduler(svc, deps.Logger)
	scanSched.Start(ctx)

	// Image freshness es señal distinta del scheduler de scans: TMDb
	// publica mejor artwork periódicamente, sin que aparezcan ficheros
	// nuevos. Weekly basta — images locked (admin curation, ADR-003)
	// se saltan per-kind dentro del refresher.
	imageRefresher := NewImageRefresher(
		deps.Items, deps.ExternalIDs, deps.Images, deps.Providers,
		deps.Pathmap, deps.ImageDir, deps.Logger,
	)
	imageRefreshSched := NewImageRefreshScheduler(deps.Libraries, imageRefresher, deps.Logger)
	imageRefreshSched.Start(ctx)

	// Skip-intro detector — chapter-based. Se suscribe al bus en
	// `library.scan.completed`; el unsub devuelto desuscribe Y drena
	// las goroutines de DetectLibrary en vuelo (audit olor Y).
	segDetector := NewSegmentDetector(
		deps.Items, deps.Chapters, deps.EpisodeSegments, deps.EventBus, deps.Logger,
	)
	segDetectorUnsub := segDetector.Start(ctx)

	// Skip-intro detector — audio-fingerprint fallback. fpcalc no en
	// PATH ⇒ Start devuelve unsub no-op y el feature degrada
	// silenciosamente al chapter-based detector.
	fingerprinter := NewFingerprinter(deps.FingerprintCacheDir)
	segFingerprinter := NewSegmentFingerprinter(
		deps.Items, deps.EpisodeSegments, fingerprinter, deps.EventBus, deps.Logger,
	)
	segFingerprinterUnsub := segFingerprinter.Start(ctx)

	// FSWatcher: complemento reactivo al scheduler (15min tick) — un
	// fichero copiado a una librería dispara scan en ~2s. Fail-soft
	// si no hay inotify / equivalente en la plataforma.
	fsWatcher := NewFSWatcher(svc, deps.Logger)
	fsWatcherStarted := true
	if err := fsWatcher.Start(ctx); err != nil {
		deps.Logger.Warn("filesystem watcher unavailable, scheduler-only mode",
			"error", err)
		fsWatcherStarted = false
	}

	return &Module{
		Scanner:                   scnr,
		Service:                   svc,
		ScanScheduler:             scanSched,
		ImageRefresher:            imageRefresher,
		ImageRefreshScheduler:     imageRefreshSched,
		Fingerprinter:             fingerprinter,
		SegmentDetector:           segDetector,
		SegmentFingerprinter:      segFingerprinter,
		FSWatcher:                 fsWatcher,
		segmentDetectorUnsub:      segDetectorUnsub,
		segmentFingerprinterUnsub: segFingerprinterUnsub,
		fsWatcherStarted:          fsWatcherStarted,
	}, nil
}

// LifecycleRegistrar es la interface mínima que Module necesita del
// `lifecycle` del binario. Definida aquí (en lugar de en main) para
// que el paquete library no importe nada de cmd/. El tipo
// `*lifecycle` del paquete main la satisface estructuralmente
// gracias al alias `stopFn = func(context.Context) error`.
//
// Idéntica en firma a `iptv.LifecycleRegistrar` — la duplicación es
// intencional (cada paquete dueño de su contrato local) y barata
// (2 métodos × 2 paquetes).
type LifecycleRegistrar interface {
	AddWorker(name string, fn func(ctx context.Context) error)
	AddService(name string, fn func(ctx context.Context) error)
}

// RegisterWith registra los hooks de teardown del módulo contra el
// lifecycle del binario. Orden:
//
//  - **Workers** (fase 1, add-order): scan scheduler → image refresh
//    scheduler → fs watcher (si arrancó). Los tres son productores de
//    actividad nueva (scans, refreshes); pararlos primero deja que
//    los services drenen su backlog sin que crezca durante el
//    shutdown.
//
//  - **Services** (fase 3, LIFO ⇒ último registrado = primero
//    parado). Orden de registro:
//      1. segment detector   (último parado)
//      2. segment fingerprinter
//      3. library service    (primero parado en LIFO)
//
//    LIFO ⇒ library service se para primero (drena scans en vuelo
//    que pueden emitir `library.scan.completed`); los dos detectores
//    aún están suscritos durante ese drain, así que el último scan
//    sigue produciendo markers. Después se desuscriben en orden
//    inverso al de registro: fingerprinter → detector (su unsub
//    drena las goroutines de DetectLibrary aún en vuelo, audit
//    olor Y).
func (m *Module) RegisterWith(lc LifecycleRegistrar) {
	lc.AddWorker("scan scheduler", func(context.Context) error {
		m.ScanScheduler.Stop()
		return nil
	})
	lc.AddWorker("image refresh scheduler", func(context.Context) error {
		m.ImageRefreshScheduler.Stop()
		return nil
	})
	if m.fsWatcherStarted {
		lc.AddWorker("fs watcher", func(context.Context) error {
			m.FSWatcher.Stop()
			return nil
		})
	}

	lc.AddService("segment detector", func(context.Context) error {
		m.segmentDetectorUnsub()
		return nil
	})
	lc.AddService("segment fingerprinter", func(context.Context) error {
		m.segmentFingerprinterUnsub()
		return nil
	})
	lc.AddService("library service", func(context.Context) error {
		m.Service.Shutdown()
		return nil
	})
}
