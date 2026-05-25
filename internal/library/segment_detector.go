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

// SegmentDetector deriva marcadores skip-intro/credits/recap por episodio
// y los persiste en `episode_segments`.
//
// Fase 1: heurística de títulos de chapters. Archivos con chapters
// titulados "Intro", "Credits", "Recap" se mapean a segmentos con
// confidence 0.95. Fase 2 (audio fingerprinting) manejará contenido
// sin chapters escribiendo rows con source='fingerprint'.
//
// Se suscribe a library.scan.completed. Detección idempotente via
// Replace(). Un run a la vez por library (mutex) para evitar races
// en Replace().
type SegmentDetector struct {
	items    *db.ItemRepository
	chapters *db.ChapterRepository
	segments *db.EpisodeSegmentRepository
	bus      *event.Bus
	logger   *slog.Logger

	// Mutex serializa runs entre todas las libraries — suficiente a esta
	// escala. Si se necesita concurrencia por library, cambiar a map de mutex.
	mu sync.Mutex

	// bgWG espera goroutines de DetectLibrary lanzadas desde el bus.
	// Sin esto, shutdown dejaría writes en vuelo contra DB cerrada.
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

// Start suscribe al bus y devuelve handle que desuscribe Y drena goroutines.
// La detección corre en goroutine para no disparar el watchdog de 30s del bus.
func (d *SegmentDetector) Start(ctx context.Context) (unsub func()) {
	busUnsub := d.bus.Subscribe(event.LibraryScanCompleted, func(e event.Event) {
		libID, _ := e.Data["library_id"].(string)
		if libID == "" {
			return
		}
		d.bgWG.Add(1)
		go func() {
			defer d.bgWG.Done()
			if err := d.DetectLibrary(ctx, libID); err != nil {
				d.logger.Warn("segment detection failed",
					"library_id", libID,
					"error", err)
			}
		}()
	})
	return func() {
		busUnsub()
		d.bgWG.Wait()
	}
}

// DetectLibrary recorre cada episodio de la library, aplica la heurística
// de chapter-title y escribe segmentos. Emite eventos SegmentDetect*.
// Exportado para un futuro botón admin "redetectar".
func (d *SegmentDetector) DetectLibrary(ctx context.Context, libraryID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	episodes, _, err := d.items.List(ctx, librarymodel.ItemFilter{
		LibraryID: libraryID,
		Type:      "episode",
		Limit:     100000,
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
			d.logger.Warn("list chapters for segment detection",
				"item_id", ep.ID,
				"error", err)
			continue
		}
		segs := DetectFromChapters(ep.DurationTicks, chapters, time.Now().Unix())
		if err := d.segments.Replace(ctx, ep.ID, librarymodel.EpisodeSegmentSourceChapter, segs); err != nil {
			d.logger.Warn("replace segments",
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
	d.logger.Info("segment detection complete",
		"library_id", libraryID,
		"episodes_scanned", scanned,
		"segments_detected", detected)
	return nil
}

// Regexes de títulos de chapter. Prefix match con word-anchor para que
// "Introduction to the Series" no matchee pero "Intro Theme" sí.
// Compilados una vez; la regex es el costo dominante del hot path.
var (
	introPattern = regexp.MustCompile(`(?i)^(intro|opening|theme|prelude|cold[\s-]?open|teaser)\b`)
	outroPattern = regexp.MustCompile(`(?i)^(outro|credits|ending|end\s+credits|closing|tag|stinger|coda)\b`)
	recapPattern = regexp.MustCompile(`(?i)^(recap|previously)\b`)
)

// DetectFromChapters mapea chapters a máximo 3 segmentos (intro, outro, recap).
// Función pura — sin DB, IO ni time.Now.
//
// Heurística:
//   - Intro: primer chapter matcheando introPattern en la primera mitad del fichero.
//   - Recap: primer chapter matcheando recapPattern, también en la primera mitad.
//   - Outro: último chapter matcheando outroPattern en la segunda mitad.
//   - durationTicks <= 0 desactiva los guards de posición.
func DetectFromChapters(durationTicks int64, chapters []*librarymodel.Chapter, now int64) []librarymodel.EpisodeSegment {
	if len(chapters) == 0 {
		return nil
	}

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
		// EndTicks > StartTicks requerido por CHECK constraint.
		// Algunos chapters tienen EndTicks=0 (ffprobe no lo emitió);
		// default a 1s para dar una ventana definida al player.
		end := c.EndTicks
		if end <= c.StartTicks {
			end = c.StartTicks + 10_000_000 // 1s en ticks
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
