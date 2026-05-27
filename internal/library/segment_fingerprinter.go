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

// SegmentFingerprinter is Phase 2 of the skip-intro / skip-credits
// detector. Where SegmentDetector reads chapter titles (only useful
// for files with embedded "Intro"/"Credits" markers — usually
// Blu-ray rips), this one analyses the audio waveform via
// chromaprint and finds the segment that recurs across episodes of
// the same series.
//
// Run after the chapter-based detector finishes. Both write to the
// same `episode_segments` table but scoped to different `source`
// values (`chapter` vs `fingerprint`), so they coexist; the API
// handler picks the highest-confidence row per kind when serialising.
//
// Heavy work — fingerprinting one 10-min audio window takes ~5-10 s
// on a single CPU. Cached on disk (see fingerprint.go) so re-scans
// of unchanged files are free. We process one library at a time
// (mutex) and one season at a time within a library; episodes
// inside a season are fingerprinted in parallel up to a small
// worker pool.
// FingerprintComputer is the slice of *Fingerprinter the orchestrator
// actually depends on. Extracted as an interface so tests can wire
// in a stub that returns synthetic hashes without spawning fpcalc.
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

	// Hard cap on concurrent fpcalc invocations. Fingerprinting is
	// CPU-bound; saturating with > NumCPU goroutines hurts
	// throughput and competes with active transcodes. 2 is a safe
	// floor on every host we've tested.
	workers int

	mu sync.Mutex

	// bgWG espera a las goroutines de DetectLibrary lanzadas desde
	// el handler del bus. Patrón paralelo al de SegmentDetector
	// (audit olor Y).
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

// Start suscribe a library.scan.completed. Cuando fpcalc no está
// disponible, loggea una vez y devuelve un unsub no-op — el chapter
// detector sigue corriendo, la instalación simplemente salta Phase 2.
//
// El unsub retornado, además de desuscribir del bus, drena las
// goroutines de DetectLibrary en vuelo (audit olor Y).
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
		log := f.logger.With("library_id", libID)
		f.bgWG.Add(1)
		go func() {
			defer f.bgWG.Done()
			if err := f.DetectLibrary(ctx, libID); err != nil {
				log.Warn("fingerprint detection failed", "error", err)
			}
		}()
	})
	return func() {
		busUnsub()
		f.bgWG.Wait()
	}
}

// DetectLibrary fingerprints every episode in the library and
// writes any common-segment matches found within each season.
// Episodes are grouped by season (parent_id); seasons with fewer
// than 2 episodes are skipped (nothing to compare against).
//
// Exported so a future "redetect this library now" admin endpoint
// can call it directly without piggy-backing on a fake scan event.
func (f *SegmentFingerprinter) DetectLibrary(ctx context.Context, libraryID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	log := f.logger.With("library_id", libraryID)

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
			continue // orphan episode without season parent — skip
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
				// outro frames are relative to the tail window, not
				// the file start; offset by (duration - window).
				offset := outroOffsetSeconds(ep.DurationTicks)
				r.startSec += offset
				r.endSec += offset
				segs = append(segs, toSegment(r, librarymodel.EpisodeSegmentOutro, now))
			}
			if err := f.segments.Replace(ctx, ep.ID, librarymodel.EpisodeSegmentSourceFingerprint, segs); err != nil {
				// Usa el `log` con library_id ya capturado al entry.
				log.Warn("replace fingerprint segments",
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
	log.Info("fingerprint detection complete",
		"episodes_scanned", scanned,
		"segments_detected", detected)
	return nil
}

// detectSeason fingerprints every episode in the season (intro
// window first, outro window second, both cached) and runs the
// matcher across each set. Returns two maps keyed by the episode's
// index in `eps` so the caller can write the right segment back to
// the right item.
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

// fingerprintAll computes the requested window for every episode in
// the season, in parallel up to f.workers. Episodes whose
// fingerprint can't be computed (missing file, ffmpeg/fpcalc
// failure) come back as nil — they don't contribute to matching but
// also don't poison the pass.
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

// segRange is the orchestrator-side view of a matched segment:
// seconds in the episode timeline (NOT frame indices) plus the
// matcher's confidence verbatim.
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
