package library

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
	"hubplay/internal/event"
)

// SegmentFingerprinter es la Fase 2 del detector skip-intro/credits.
// Analiza el waveform de audio via chromaprint para encontrar segmentos
// recurrentes entre episodios de la misma serie.
//
// Corre tras el detector de chapters. Ambos escriben a episode_segments
// con `source` distinto, coexistiendo; el handler API escoge el row
// de mayor confidence por kind.
//
// Trabajo pesado: ~5-10s por ventana de 10 min. Caché en disco hace
// re-scans de ficheros sin cambios gratuitos. Un library a la vez
// (mutex), una season a la vez, episodios dentro de season en paralelo
// hasta un pool de workers.

// FingerprintComputer: interfaz del Fingerprinter que el orquestador usa.
// Extraída para que tests inyecten stub sin spawnar fpcalc.
type FingerprintComputer interface {
	Available() bool
	Compute(ctx context.Context, itemID, sourcePath string, window FingerprintWindow) ([]uint32, error)
}

type SegmentFingerprinter struct {
	items    *db.ItemRepository
	segments *db.EpisodeSegmentRepository
	prints   FingerprintComputer
	bus      *event.Bus
	logger   *slog.Logger

	// Tope de invocaciones concurrentes de fpcalc. Es CPU-bound;
	// saturar compite con transcodes activos. 2 es piso seguro.
	workers int

	mu sync.Mutex

	// bgWG espera goroutines de DetectLibrary lanzadas desde el bus.
	bgWG sync.WaitGroup
}

func NewSegmentFingerprinter(
	items *db.ItemRepository,
	segments *db.EpisodeSegmentRepository,
	prints FingerprintComputer,
	bus *event.Bus,
	logger *slog.Logger,
) *SegmentFingerprinter {
	return &SegmentFingerprinter{
		items:    items,
		segments: segments,
		prints:   prints,
		bus:      bus,
		logger:   logger.With("module", "segment_fingerprinter"),
		workers:  2,
	}
}

// Start suscribe a library.scan.completed. Sin fpcalc, loggea una vez
// y devuelve unsub no-op — Fase 1 (chapters) sigue corriendo.
// El unsub también drena goroutines en vuelo.
func (f *SegmentFingerprinter) Start(ctx context.Context) (unsub func()) {
	if !f.prints.Available() {
		f.logger.Info("fpcalc not on PATH — skipping audio fingerprint detection (install chromaprint-tools to enable)")
		return func() {}
	}
	busUnsub := f.bus.Subscribe(event.LibraryScanCompleted, func(e event.Event) {
		libID, _ := e.Data["library_id"].(string)
		if libID == "" {
			return
		}
		f.bgWG.Add(1)
		go func() {
			defer f.bgWG.Done()
			if err := f.DetectLibrary(ctx, libID); err != nil {
				f.logger.Warn("fingerprint detection failed",
					"library_id", libID, "error", err)
			}
		}()
	})
	return func() {
		busUnsub()
		f.bgWG.Wait()
	}
}

// DetectLibrary fingerprinta cada episodio y escribe matches comunes
// dentro de cada season. Seasons con <2 episodios se saltan.
// Exportado para futuro endpoint admin "redetectar".
func (f *SegmentFingerprinter) DetectLibrary(ctx context.Context, libraryID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	episodes, _, err := f.items.List(ctx, librarymodel.ItemFilter{
		LibraryID: libraryID,
		Type:      "episode",
		Limit:     100000,
	})
	if err != nil {
		return err
	}
	bySeason := make(map[string][]*librarymodel.Item, 8)
	for _, ep := range episodes {
		if ep.ParentID == "" {
			continue
		}
		bySeason[ep.ParentID] = append(bySeason[ep.ParentID], ep)
	}

	totalEps := 0
	for _, eps := range bySeason {
		if len(eps) >= 2 {
			totalEps += len(eps)
		}
	}
	f.bus.Publish(event.Event{
		Type: event.SegmentDetectStarted,
		Data: map[string]any{
			"library_id": libraryID,
			"total":      totalEps,
			"source":     "fingerprint",
		},
	})

	scanned := 0
	detected := 0
	for seasonID, eps := range bySeason {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(eps) < 2 {
			continue
		}
		introHits, outroHits := f.detectSeason(ctx, eps)
		now := time.Now().Unix()
		for i, ep := range eps {
			segs := make([]librarymodel.EpisodeSegment, 0, 2)
			if r, ok := introHits[i]; ok {
				segs = append(segs, toSegment(r, librarymodel.EpisodeSegmentIntro, now))
			}
			if r, ok := outroHits[i]; ok {
				// Frames del outro son relativos a la ventana tail,
				// no al inicio del fichero; sumar offset.
				offset := outroOffsetSeconds(ep.DurationTicks)
				r.startSec += offset
				r.endSec += offset
				segs = append(segs, toSegment(r, librarymodel.EpisodeSegmentOutro, now))
			}
			if err := f.segments.Replace(ctx, ep.ID, librarymodel.EpisodeSegmentSourceFingerprint, segs); err != nil {
				f.logger.Warn("replace fingerprint segments",
					"item_id", ep.ID, "season_id", seasonID, "error", err)
				continue
			}
			scanned++
			detected += len(segs)
		}
	}

	f.bus.Publish(event.Event{
		Type: event.SegmentDetectCompleted,
		Data: map[string]any{
			"library_id": libraryID,
			"scanned":    scanned,
			"detected":   detected,
			"source":     "fingerprint",
		},
	})
	f.logger.Info("fingerprint detection complete",
		"library_id", libraryID,
		"episodes_scanned", scanned,
		"segments_detected", detected)
	return nil
}

// detectSeason fingerprinta cada episodio de la season (intro y outro,
// ambos cacheados) y corre el matcher. Retorna maps indexados por
// posición en `eps`.
func (f *SegmentFingerprinter) detectSeason(
	ctx context.Context,
	eps []*librarymodel.Item,
) (intro, outro map[int]segRange) {
	intro = make(map[int]segRange)
	outro = make(map[int]segRange)

	introPrints := f.fingerprintAll(ctx, eps, WindowIntro)
	if matches := FindCommonSegments(introPrints); len(matches) > 0 {
		for _, m := range matches {
			intro[m.EpisodeIndex] = segRange{
				startSec:   FramesToSeconds(m.Range.Start),
				endSec:     FramesToSeconds(m.Range.End),
				confidence: m.Confidence,
			}
		}
	}

	outroPrints := f.fingerprintAll(ctx, eps, WindowOutro)
	if matches := FindCommonSegments(outroPrints); len(matches) > 0 {
		for _, m := range matches {
			outro[m.EpisodeIndex] = segRange{
				startSec:   FramesToSeconds(m.Range.Start),
				endSec:     FramesToSeconds(m.Range.End),
				confidence: m.Confidence,
			}
		}
	}
	return intro, outro
}

// fingerprintAll computa la ventana indicada para cada episodio en
// paralelo hasta f.workers. Episodios sin fingerprint (fichero faltante,
// fallo de ffmpeg/fpcalc) quedan nil — no contribuyen al matching
// pero no envenenan el paso.
func (f *SegmentFingerprinter) fingerprintAll(
	ctx context.Context,
	eps []*librarymodel.Item,
	window FingerprintWindow,
) [][]uint32 {
	out := make([][]uint32, len(eps))
	sem := make(chan struct{}, f.workers)
	var wg sync.WaitGroup
	for i, ep := range eps {
		if ep.Path == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, ep *librarymodel.Item) {
			defer wg.Done()
			defer func() { <-sem }()
			hashes, err := f.prints.Compute(ctx, ep.ID, ep.Path, window)
			if err != nil {
				if !errors.Is(err, errFpcalcMissing) {
					f.logger.Debug("fingerprint compute failed",
						"item_id", ep.ID, "window", window, "error", err)
				}
				return
			}
			out[i] = hashes
		}(i, ep)
	}
	wg.Wait()
	return out
}

// segRange: vista del orquestador de un segmento matcheado — segundos
// en timeline del episodio + confidence del matcher.
type segRange struct {
	startSec   float64
	endSec     float64
	confidence float64
}

func toSegment(r segRange, kind librarymodel.EpisodeSegmentKind, now int64) librarymodel.EpisodeSegment {
	return librarymodel.EpisodeSegment{
		Kind:       kind,
		StartTicks: secondsToTicks(r.startSec),
		EndTicks:   secondsToTicks(r.endSec),
		Confidence: r.confidence,
		DetectedAt: now,
	}
}

func secondsToTicks(s float64) int64 {
	return int64(s * 10_000_000)
}

func outroOffsetSeconds(durationTicks int64) float64 {
	if durationTicks <= 0 {
		return 0
	}
	dur := float64(durationTicks) / 10_000_000.0
	off := dur - float64(OutroWindowSeconds)
	if off < 0 {
		return 0
	}
	return off
}
