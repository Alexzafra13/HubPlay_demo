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

	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/probe"
	"hubplay/internal/provider"

	"github.com/google/uuid"
)

// Known media file extensions.
var mediaExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".mov": true, ".wmv": true,
	".flv": true, ".webm": true, ".m4v": true, ".ts": true, ".mpg": true,
	".mpeg": true, ".3gp": true, ".ogv": true,
	// Audio
	".mp3": true, ".flac": true, ".aac": true, ".ogg": true, ".wma": true,
	".wav": true, ".m4a": true, ".opus": true, ".alac": true,
}

// IsMediaFile returns true if the file extension is a known media format.
func IsMediaFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return mediaExtensions[ext]
}

// Scanner walks library paths and creates/updates items in the database.
type Scanner struct {
	items       *db.ItemRepository
	streams     *db.MediaStreamRepository
	metadata    *db.MetadataRepository
	externalIDs *db.ExternalIDRepository
	images      *db.ImageRepository
	providers   *provider.Manager
	prober      probe.Prober
	bus         *event.Bus
	logger      *slog.Logger
}

func New(
	items *db.ItemRepository,
	streams *db.MediaStreamRepository,
	metadata *db.MetadataRepository,
	externalIDs *db.ExternalIDRepository,
	images *db.ImageRepository,
	providers *provider.Manager,
	prober probe.Prober,
	bus *event.Bus,
	logger *slog.Logger,
) *Scanner {
	return &Scanner{
		items:       items,
		streams:     streams,
		metadata:    metadata,
		externalIDs: externalIDs,
		images:      images,
		providers:   providers,
		prober:      prober,
		bus:         bus,
		logger:      logger.With("module", "scanner"),
	}
}

// ScanResult contains statistics from a library scan.
type ScanResult struct {
	Added   int
	Updated int
	Removed int
	Errors  int
	Elapsed time.Duration
}

// ScanLibrary scans all paths for a library and updates the database.
func (s *Scanner) ScanLibrary(ctx context.Context, lib *db.Library) (*ScanResult, error) {
	start := time.Now()
	result := &ScanResult{}

	s.bus.Publish(event.Event{
		Type: event.LibraryScanStarted,
		Data: map[string]any{"library_id": lib.ID, "library_name": lib.Name},
	})

	// Collect all existing paths for this library to detect removals
	existingPaths := make(map[string]bool)
	existingItems, _, err := s.items.List(ctx, db.ItemFilter{
		LibraryID: lib.ID,
		Limit:     100000, // get all
	})
	if err != nil {
		return nil, fmt.Errorf("listing existing items: %w", err)
	}
	for _, item := range existingItems {
		if item.Path != "" {
			existingPaths[item.Path] = true
		}
	}

	// Walk each library path
	seenPaths := make(map[string]bool)
	for _, libPath := range lib.Paths {
		if err := s.walkPath(ctx, lib, libPath, seenPaths, result); err != nil {
			s.logger.Error("error walking path", "path", libPath, "error", err)
			result.Errors++
		}
	}

	// Mark missing files as unavailable
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

func (s *Scanner) walkPath(ctx context.Context, lib *db.Library, root string, seenPaths map[string]bool, result *ScanResult) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			s.logger.Warn("walk error", "path", path, "error", err)
			return nil // continue walking
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

		seenPaths[path] = true

		if err := s.processFile(ctx, lib, path, result); err != nil {
			s.logger.Warn("error processing file", "path", path, "error", err)
			result.Errors++
		}
		return nil
	})
}

func (s *Scanner) processFile(ctx context.Context, lib *db.Library, path string, result *ScanResult) error {
	// Check if item already exists
	existing, err := s.items.GetByPath(ctx, path)
	if err == nil {
		// Item exists — check if file changed via fingerprint
		fp, fpErr := fingerprint(path)
		if fpErr != nil {
			return fpErr
		}
		if existing.Fingerprint == fp && existing.IsAvailable {
			return nil // unchanged
		}
		// File changed or was unavailable — re-probe and update
		return s.updateItem(ctx, existing, path, fp, result)
	}

	// New file — probe and create
	return s.createItem(ctx, lib, path, result)
}

func (s *Scanner) createItem(ctx context.Context, lib *db.Library, path string, result *ScanResult) error {
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

	item := &db.Item{
		ID:            itemID,
		LibraryID:     lib.ID,
		Type:          itemTypeFromLibrary(lib.ContentType),
		Title:         title,
		SortTitle:     strings.ToLower(title),
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

	// Store streams
	streams := probeResultToStreams(itemID, probeResult)
	if len(streams) > 0 {
		if err := s.streams.ReplaceForItem(ctx, itemID, streams); err != nil {
			s.logger.Warn("failed to store streams", "item_id", itemID, "error", err)
		}
	}

	result.Added++
	s.bus.Publish(event.Event{
		Type: event.ItemAdded,
		Data: map[string]any{"item_id": itemID, "title": title, "library_id": lib.ID},
	})

	// Fetch metadata from TMDB (best-effort, don't fail the scan)
	s.enrichMetadata(ctx, item)

	return nil
}

func (s *Scanner) updateItem(ctx context.Context, item *db.Item, path, fp string, result *ScanResult) error {
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

	result.Updated++
	s.bus.Publish(event.Event{
		Type: event.ItemUpdated,
		Data: map[string]any{"item_id": item.ID, "title": item.Title},
	})

	return nil
}

func probeResultToStreams(itemID string, pr *probe.Result) []*db.MediaStream {
	var streams []*db.MediaStream
	for _, s := range pr.Streams {
		streams = append(streams, &db.MediaStream{
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

// titleFromPath extracts a human-readable title from the file path.
func titleFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

// yearPattern matches (2023), [2023], or just 2023 in a filename.
var yearPattern = regexp.MustCompile(`[\(\[\s]?((?:19|20)\d{2})[\)\]\s]?`)

// parseTitleYear extracts a clean title and year from a filename.
// Examples: "Transformers El despertar (2023)" -> ("Transformers El despertar", 2023)
//           "Toy Story 3 [2010]" -> ("Toy Story 3", 2010)
func parseTitleYear(filename string) (string, int) {
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filepath.Base(filename), ext)

	// Find the last year match (most likely the release year)
	matches := yearPattern.FindAllStringSubmatchIndex(name, -1)
	if len(matches) == 0 {
		return name, 0
	}

	last := matches[len(matches)-1]
	yearStr := name[last[2]:last[3]]
	year, _ := strconv.Atoi(yearStr)

	// Title is everything before the year match
	title := strings.TrimSpace(name[:last[0]])
	if title == "" {
		title = name
	}

	return title, year
}

// enrichMetadata searches TMDB for the item and stores metadata + images.
func (s *Scanner) enrichMetadata(ctx context.Context, item *db.Item) {
	if s.providers == nil {
		return
	}

	// Parse title and year from filename for better TMDB search
	cleanTitle, year := parseTitleYear(item.Title)
	if year == 0 {
		year = item.Year
	}

	itemType := provider.ItemMovie
	if item.Type == "episode" || item.Type == "series" {
		itemType = provider.ItemSeries
	}

	// Search TMDB
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

	// Fetch full metadata
	meta, err := s.providers.FetchMetadata(ctx, best.ExternalID, itemType)
	if err != nil || meta == nil {
		s.logger.Debug("TMDB metadata fetch failed", "id", best.ExternalID, "error", err)
		return
	}

	// Update item fields
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

	// Store extended metadata
	genresJSON, _ := json.Marshal(meta.Genres)
	tagsJSON, _ := json.Marshal(meta.Tags)
	if err := s.metadata.Upsert(ctx, &db.Metadata{
		ItemID:     item.ID,
		Overview:   meta.Overview,
		Tagline:    meta.Tagline,
		Studio:     meta.Studio,
		GenresJSON: string(genresJSON),
		TagsJSON:   string(tagsJSON),
	}); err != nil {
		s.logger.Warn("failed to store metadata", "id", item.ID, "error", err)
	}

	// Store external IDs
	for prov, extID := range meta.ExternalIDs {
		if err := s.externalIDs.Upsert(ctx, &db.ExternalID{
			ItemID:     item.ID,
			Provider:   prov,
			ExternalID: extID,
		}); err != nil {
			s.logger.Warn("failed to store external id", "id", item.ID, "provider", prov, "error", err)
		}
	}

	// Fetch and store images
	if len(meta.ExternalIDs) > 0 {
		images, err := s.providers.FetchImages(ctx, meta.ExternalIDs, itemType)
		if err != nil {
			s.logger.Debug("TMDB image fetch failed", "id", item.ID, "error", err)
			return
		}

		posterStored := false
		backdropStored := false
		for _, img := range images {
			if img.Type == "primary" && posterStored {
				continue
			}
			if img.Type == "backdrop" && backdropStored {
				continue
			}

			dbImg := &db.Image{
				ID:        uuid.NewString(),
				ItemID:    item.ID,
				Type:      img.Type,
				Path:      img.URL, // Store TMDB URL directly
				Width:     img.Width,
				Height:    img.Height,
				Provider:  "tmdb",
				IsPrimary: (img.Type == "primary" && !posterStored) || (img.Type == "backdrop" && !backdropStored),
				AddedAt:   time.Now(),
			}
			if err := s.images.Create(ctx, dbImg); err != nil {
				s.logger.Warn("failed to store image", "id", item.ID, "type", img.Type, "error", err)
				continue
			}

			if img.Type == "primary" {
				posterStored = true
			}
			if img.Type == "backdrop" {
				backdropStored = true
			}
			// Only store first poster + first backdrop
			if posterStored && backdropStored {
				break
			}
		}
	}

	s.logger.Info("enriched metadata", "title", item.Title, "tmdb_id", best.ExternalID, "year", item.Year)
}

// itemTypeFromLibrary maps library content types to item types.
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

// fingerprint computes a fast fingerprint of a file using size + first 64KB hash.
func fingerprint(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %q: %w", path, err)
	}

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %q: %w", path, err)
	}
	defer f.Close() //nolint:errcheck

	h := sha256.New()
	// Hash first 64KB for speed
	if _, err := io.CopyN(h, f, 65536); err != nil && err != io.EOF {
		return "", fmt.Errorf("hashing %q: %w", path, err)
	}

	return fmt.Sprintf("%d:%x", info.Size(), h.Sum(nil)[:16]), nil
}
