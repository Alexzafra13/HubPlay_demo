package scanner

// Procesamiento por fichero: lo que ocurre cada vez que el walk del
// scanner llega a un fichero de media — crea o actualiza la fila en
// `items`, persiste streams y capítulos, y dispara los eventos del bus.
//
// Separado del walk principal (`scanner.go`) para que esa capa sólo
// orquesta y este fichero contenga la traducción `probe.Result →
// librarymodel.*` + la decisión create/update sobre `items`. Helpers
// puros (`fingerprint`, `titleFromPath`, `itemTypeFromLibrary`,
// `probeResultTo*`) viven aquí porque son los que materializan los
// campos del item desde el fichero.

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"hubplay/internal/event"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/probe"

	"github.com/google/uuid"
)

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

	now := s.clock.Now()
	title := titleFromPath(path)
	itemID := uuid.NewString()
	itemType := itemTypeFromLibrary(lib.ContentType)
	// Sub-logger con item_id + path desde el entry: los Warns de series/
	// season ensure de abajo + los de streams/people/chapters más adelante
	// comparten contexto. Antes los primeros warns no llevaban item_id.
	log := s.logger.With("item_id", itemID, "path", path)

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
				log.Warn("failed to ensure series row", "series", match.SeriesName, "error", err)
			} else {
				seasonID, err := s.ensureSeasonRow(ctx, lib, cache, sID, match.SeasonNumber)
				if err != nil {
					log.Warn("failed to ensure season row", "series", match.SeriesName, "season", match.SeasonNumber, "error", err)
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

	streams := probeResultToStreams(itemID, probeResult)
	// El repositorio de capítulos es opcional (tests viejos no lo cablean);
	// conservamos esa condición para decidir si poblamos capítulos.
	var chapters []librarymodel.Chapter
	if s.chapters != nil && len(probeResult.Chapters) > 0 {
		chapters = probeResultToChapters(probeResult)
	}

	// Una sola transacción para item + streams + chapters: un fsync y una
	// adquisición del write-lock por fichero en vez de tres. En bibliotecas
	// grandes sobre SQLite (single-writer) este es el cuello dominante del
	// throughput de scan. El fallo hace rollback del item completo — el
	// fichero se reintenta en el siguiente scan (no queda a medias).
	if err := s.items.IngestItem(ctx, item, streams, chapters); err != nil {
		return fmt.Errorf("creating item: %w", err)
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
	item.UpdatedAt = s.clock.Now()

	if err := s.items.Update(ctx, item); err != nil {
		return fmt.Errorf("updating item: %w", err)
	}

	log := s.logger.With("item_id", item.ID)
	streams := probeResultToStreams(item.ID, probeResult)
	if len(streams) > 0 {
		if err := s.streams.ReplaceForItem(ctx, item.ID, streams); err != nil {
			log.Warn("failed to update streams", "error", err)
		}
	}

	if s.chapters != nil {
		if err := s.chapters.Replace(ctx, item.ID, probeResultToChapters(probeResult)); err != nil {
			log.Warn("failed to update chapters", "error", err)
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
		// Carátula embebida (cover art mjpeg/png con disposition
		// attached_pic): no es una pista de vídeo reproducible. Si se
		// persiste, la decisión de playback la toma por "el vídeo" del
		// fichero → un MP3 con carátula caía a transcode completo, y el
		// UI listaba una pista de vídeo fantasma. PB-24 (audit 2026-06-10).
		if s.CodecType == "video" && s.IsAttachedPic {
			continue
		}
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
