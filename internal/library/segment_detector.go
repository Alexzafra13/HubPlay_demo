package library

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
	"hubplay/internal/event"
)

// SegmentDetector derives skip-intro / skip-credits / skip-recap
// markers for every episode in a library and persists them in
// `episode_segments`.
//
// Phase 1 (this file): chapter-title heuristic. Many ripped or
// professionally-encoded files already carry chapter markers
// (mkv/mp4 chapters); when those chapters are titled "Intro",
// "Opening", "Credits", "Recap", etc., we map them straight to
// segments at confidence 0.95 (chapter-titled-intro is essentially
// ground truth). Files without chapter markers are skipped — Phase 2
// (audio fingerprinting) will handle the unlabeled-content case
// without changing this file's contract: it'll just write rows
// with `source = 'fingerprint'` alongside the chapter-derived ones.
//
// The detector subscribes to `library.scan.completed`. Each scan
// kicks an async run scoped to the library that just finished.
// Detection is idempotent — `EpisodeSegmentRepository.Replace` clears
// the prior chapter-source rows for an item before re-inserting, so
// re-running on a re-scanned episode replaces stale ranges cleanly.
//
// One run at a time per library. A scan that completes while the
// detector is still processing its previous run is queued; we do
// not run two passes against the same library concurrently because
// chapter-source rows would race on Replace().
type SegmentDetector struct {
	items    *db.ItemRepository
	chapters *db.ChapterRepository
	segments *db.EpisodeSegmentRepository
	bus      *event.Bus
	logger   *slog.Logger

	// Mutex serialises runs across all libraries — sufficient at the
	// scale this scheduler runs at (libraries are dozens, not
	// thousands) and keeps the implementation tiny. If concurrency
	// per-library ever matters, swap for a per-library mutex map.
	mu sync.Mutex

	// bgWG espera a las goroutines de DetectLibrary lanzadas desde
	// el handler del bus. Sin esto, shutdown podía dejar writes en
	// vuelo contra una DB ya cerrada — produciendo "sql: database
	// is closed" en logs y, en patológico, writes parciales (audit
	// olor Y). El patrón replica `library.Service` y
	// `iptv.TransmuxManager`.
	bgWG sync.WaitGroup
}

func NewSegmentDetector(
	items *db.ItemRepository,
	chapters *db.ChapterRepository,
	segments *db.EpisodeSegmentRepository,
	bus *event.Bus,
	logger *slog.Logger,
) *SegmentDetector {
	return &SegmentDetector{
		items:    items,
		chapters: chapters,
		segments: segments,
		bus:      bus,
		logger:   logger.With("module", "segment_detector"),
	}
}

// Start suscribe el detector a library.scan.completed y devuelve un
// handle que desuscribe Y drena las goroutines de DetectLibrary en
// vuelo. El caller (main.go) lo difiere en shutdown.
//
// El handler lanza la detección en una goroutine para que el
// watchdog de 30s del bus nunca se dispare; un scan sobre cientos
// de episodes puede pasar fácilmente de 30s. La goroutine se
// registra en bgWG para que Stop espere su finalización (audit
// olor Y).
func (d *SegmentDetector) Start(ctx context.Context) (unsub func()) {
	busUnsub := d.bus.Subscribe(event.LibraryScanCompleted, func(e event.Event) {
		libID, _ := e.Data["library_id"].(string)
		if libID == "" {
			return
		}
		// Sub-logger por library para que el Warn de abajo y futuros
		// logs no repitan "library_id". DetectLibrary también lo extrae
		// internamente; aquí lo necesitamos para el handler del bus.
		log := d.logger.With("library_id", libID)
		d.bgWG.Add(1)
		go func() {
			defer d.bgWG.Done()
			if err := d.DetectLibrary(ctx, libID); err != nil {
				log.Warn("segment detection failed", "error", err)
			}
		}()
	})
	return func() {
		busUnsub()
		d.bgWG.Wait()
	}
}

// DetectLibrary walks every episode in the given library, runs the
// chapter-title heuristic against each one, and writes segments.
// Emits SegmentDetect{Started,Progress,Completed} events along the
// way so the admin SSE stream can surface a banner.
//
// Exported so a future "redetect this library" admin button can
// trigger it directly without a fake scan event.
func (d *SegmentDetector) DetectLibrary(ctx context.Context, libraryID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	log := d.logger.With("library_id", libraryID)

	episodes, _, err := d.items.List(ctx, librarymodel.ItemFilter{
		LibraryID: libraryID,
		Type:      "episode",
		Limit:     100000, // upper bound; libraries this size are pathological
	})
	if err != nil {
		return err
	}

	d.bus.Publish(event.Event{
		Type: event.SegmentDetectStarted,
		Data: map[string]any{
			"library_id": libraryID,
			"total":      len(episodes),
		},
	})

	scanned := 0
	detected := 0
	const progressEvery = 25

	for _, ep := range episodes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		chapters, err := d.chapters.ListByItem(ctx, ep.ID)
		if err != nil {
			// Usa el `log` con library_id ya capturado al entry; añadimos item_id.
			log.Warn("list chapters for segment detection",
				"item_id", ep.ID,
				"error", err)
			continue
		}
		segs := DetectFromChapters(ep.DurationTicks, chapters, time.Now().Unix())
		if err := d.segments.Replace(ctx, ep.ID, librarymodel.EpisodeSegmentSourceChapter, segs); err != nil {
			log.Warn("replace segments",
				"item_id", ep.ID,
				"error", err)
			continue
		}
		scanned++
		detected += len(segs)

		if scanned%progressEvery == 0 {
			d.bus.Publish(event.Event{
				Type: event.SegmentDetectProgress,
				Data: map[string]any{
					"library_id": libraryID,
					"scanned":    scanned,
					"detected":   detected,
				},
			})
		}
	}

	d.bus.Publish(event.Event{
		Type: event.SegmentDetectCompleted,
		Data: map[string]any{
			"library_id": libraryID,
			"scanned":    scanned,
			"detected":   detected,
		},
	})
	log.Info("segment detection complete",
		"episodes_scanned", scanned,
		"segments_detected", detected)
	return nil
}

// chapter-title regexes. Word-anchored prefix match — we don't want
// "Introduction to the Series" (a chapter literally named that on
// some Blu-rays) to count as an intro, but "Intro", "Intro Theme",
// "Opening Credits" should. `\b` anchors after the keyword.
//
// Compiled once at package init; regex compilation is the dominant
// cost in this detector's hot path.
var (
	introPattern = regexp.MustCompile(`(?i)^(intro|opening|theme|prelude|cold[\s-]?open|teaser)\b`)
	outroPattern = regexp.MustCompile(`(?i)^(outro|credits|ending|end\s+credits|closing|tag|stinger|coda)\b`)
	recapPattern = regexp.MustCompile(`(?i)^(recap|previously)\b`)
)

// DetectFromChapters maps a list of chapters onto at most three
// segments (one intro, one outro, one recap). Pure function — no
// DB, no IO, no time.Now — `now` is passed in so tests can pin it.
//
// Heuristic:
//
//   - Intro: first chapter matching `introPattern` whose start_ticks
//     is in the first 50% of the file. The position bound is a guard
//     against weird trailers / bonus content that some rips include
//     after the main feature with chapter titles like "Opening
//     Reprise".
//   - Recap: first chapter matching `recapPattern`, also gated to
//     the first 50% of the file. Recaps precede intros in episode
//     order so the position guard is generous.
//   - Outro: last chapter matching `outroPattern` whose start_ticks
//     is in the last 50% of the file. We pick the last to avoid
//     matching e.g. an "Opening Credits" chapter from a flashback
//     scene before the real outro.
//
// `durationTicks <= 0` (item with unknown duration) disables the
// position guards — we still match by title, just with no sanity
// check on where the title falls. That's OK because a chapter
// titled "Intro" with no duration context is still very likely an
// intro.
//
// Returns segments in (kind) order: recap, intro, outro. The order
// is mostly cosmetic since the repo writes them all in one tx, but
// keeps test golden output deterministic.
func DetectFromChapters(durationTicks int64, chapters []*librarymodel.Chapter, now int64) []librarymodel.EpisodeSegment {
	if len(chapters) == 0 {
		return nil
	}

	// Use a 50/50 split as the sanity bound. With duration = 0 the
	// midpoint becomes 0 too, which would reject every chapter, so
	// short-circuit the bound to "always pass" in that case.
	hasDuration := durationTicks > 0
	mid := durationTicks / 2

	matchKind := func(title string) (librarymodel.EpisodeSegmentKind, bool) {
		t := strings.TrimSpace(title)
		switch {
		case introPattern.MatchString(t):
			return librarymodel.EpisodeSegmentIntro, true
		case outroPattern.MatchString(t):
			return librarymodel.EpisodeSegmentOutro, true
		case recapPattern.MatchString(t):
			return librarymodel.EpisodeSegmentRecap, true
		}
		return "", false
	}

	// Pick first match for intro/recap (earliest in file) and last
	// match for outro (latest in file). We pre-classify into three
	// buckets to keep the loop linear and avoid re-scanning.
	var firstIntro, firstRecap, lastOutro *librarymodel.Chapter
	for _, c := range chapters {
		kind, ok := matchKind(c.Title)
		if !ok {
			continue
		}
		switch kind {
		case librarymodel.EpisodeSegmentIntro:
			if firstIntro == nil && (!hasDuration || c.StartTicks < mid) {
				firstIntro = c
			}
		case librarymodel.EpisodeSegmentRecap:
			if firstRecap == nil && (!hasDuration || c.StartTicks < mid) {
				firstRecap = c
			}
		case librarymodel.EpisodeSegmentOutro:
			if !hasDuration || c.StartTicks >= mid {
				lastOutro = c
			}
		}
	}

	out := make([]librarymodel.EpisodeSegment, 0, 3)
	emit := func(c *librarymodel.Chapter, kind librarymodel.EpisodeSegmentKind) {
		if c == nil {
			return
		}
		// EndTicks must be > StartTicks per the CHECK constraint.
		// Some chapters store EndTicks = 0 when ffprobe didn't
		// emit one; default to a 1-second range so the player at
		// least has a defined window.
		end := c.EndTicks
		if end <= c.StartTicks {
			end = c.StartTicks + 10_000_000 // 1s in ticks
		}
		out = append(out, librarymodel.EpisodeSegment{
			Kind:       kind,
			StartTicks: c.StartTicks,
			EndTicks:   end,
			Confidence: 0.95,
			DetectedAt: now,
		})
	}
	emit(firstRecap, librarymodel.EpisodeSegmentRecap)
	emit(firstIntro, librarymodel.EpisodeSegmentIntro)
	emit(lastOutro, librarymodel.EpisodeSegmentOutro)
	return out
}
