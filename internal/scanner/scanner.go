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
	"hubplay/internal/imaging"
	"hubplay/internal/imaging/pathmap"
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

// providerFetcher is the slice of provider.Manager the scanner actually
// uses. Defined as an interface so a test can swap it out without
// constructing a full Manager + provider registration cycle (the same
// pattern ImageRefresher uses for ImageRefresherProvider).
type providerFetcher interface {
	SearchMetadata(ctx context.Context, query provider.SearchQuery) ([]provider.SearchResult, error)
	FetchMetadata(ctx context.Context, externalID string, itemType provider.ItemType) (*provider.MetadataResult, error)
	FetchImages(ctx context.Context, ids map[string]string, itemType provider.ItemType) ([]provider.ImageResult, error)
}

// Scanner walks library paths and creates/updates items in the database.
//
// `imageDir` and `pathmap` are optional; when both are wired the scanner
// downloads provider artwork to local storage during enrichment instead
// of persisting remote URLs. When either is nil (e.g. older test
// environments) image enrichment is skipped silently — never persists
// remote URLs that would leak the user's IP to TMDb on every poster
// view.
type Scanner struct {
	items       *db.ItemRepository
	streams     *db.MediaStreamRepository
	metadata    *db.MetadataRepository
	externalIDs *db.ExternalIDRepository
	images      *db.ImageRepository
	chapters    *db.ChapterRepository
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
	providers *provider.Manager,
	prober probe.Prober,
	bus *event.Bus,
	imageDir string,
	pm *pathmap.Store,
	logger *slog.Logger,
) *Scanner {
	// `providers` is typed as the concrete *provider.Manager in the
	// public API to keep the wiring in main.go obvious; internally we
	// store it under a small interface so tests can fake it.
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
		providers:   pf,
		prober:      prober,
		bus:         bus,
		imageDir:    imageDir,
		pathmap:     pm,
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

	// Collect all existing paths for this library to detect removals.
	// Same pass populates the show-hierarchy cache (series + season
	// rows that already exist) so the scan doesn't re-create them
	// for each episode it walks.
	existingPaths := make(map[string]bool)
	cache := newShowCache()
	if err := s.iterateLibraryItems(ctx, lib.ID, func(item *db.Item) {
		if item.Path != "" {
			existingPaths[item.Path] = true
		}
		switch item.Type {
		case "series":
			cache.rememberSeries(item.Title, item.ID)
		case "season":
			if item.ParentID != "" && item.SeasonNumber != nil {
				cache.rememberSeason(item.ParentID, *item.SeasonNumber, item.ID)
			}
		}
	}); err != nil {
		return nil, fmt.Errorf("listing existing items: %w", err)
	}

	// Walk each library path
	seenPaths := make(map[string]bool)
	for _, libPath := range lib.Paths {
		if err := s.walkPath(ctx, lib, libPath, seenPaths, cache, result); err != nil {
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

// iterateLibraryItems pages through all items in a library in batches,
// calling fn for each item. This avoids loading the entire library into memory.
func (s *Scanner) iterateLibraryItems(ctx context.Context, libraryID string, fn func(*db.Item)) error {
	const pageSize = 500
	offset := 0
	for {
		items, _, err := s.items.List(ctx, db.ItemFilter{
			LibraryID: libraryID,
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
	return nil
}

func (s *Scanner) walkPath(ctx context.Context, lib *db.Library, root string, seenPaths map[string]bool, cache *showCache, result *ScanResult) error {
	// Resolve the root to a real absolute path for symlink boundary checks.
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

		// Resolve symlinks to prevent path traversal attacks.
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
		return nil
	})
}

// RefreshMetadata re-fetches metadata and images for all items in a library.
// It deletes existing images/metadata and re-enriches from providers.
func (s *Scanner) RefreshMetadata(ctx context.Context, lib *db.Library) error {
	s.logger.Info("refreshing metadata for library", "library", lib.Name)

	count := 0
	err := s.iterateLibraryItems(ctx, lib.ID, func(item *db.Item) {
		// Delete old images and metadata so enrichMetadata re-fetches them
		_ = s.images.DeleteByItem(ctx, item.ID)
		_ = s.metadata.Delete(ctx, item.ID)
		s.enrichMetadata(ctx, item)
		count++
	})
	if err != nil {
		return fmt.Errorf("listing items for refresh: %w", err)
	}

	s.logger.Info("metadata refresh complete", "library", lib.Name, "items", count)
	return nil
}

func (s *Scanner) processFile(ctx context.Context, lib *db.Library, libRoot, path string, cache *showCache, result *ScanResult) error {
	// Check if item already exists
	existing, err := s.items.GetByPath(ctx, path)
	if err == nil {
		// Item exists — check if file changed via fingerprint
		fp, fpErr := fingerprint(path)
		if fpErr != nil {
			return fpErr
		}
		if existing.Fingerprint == fp && existing.IsAvailable {
			// File unchanged — re-enrich if metadata is missing
			// (e.g. provider API key was added after initial scan).
			s.enrichIfMissing(ctx, existing)
			return nil
		}
		// File changed or was unavailable — re-probe and update
		return s.updateItem(ctx, existing, path, fp, result)
	}

	// New file — probe and create
	return s.createItem(ctx, lib, libRoot, path, cache, result)
}

func (s *Scanner) createItem(ctx context.Context, lib *db.Library, libRoot, path string, cache *showCache, result *ScanResult) error {
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

	// For shows libraries, build the series → season → episode
	// hierarchy. Each episode points at its season via parent_id;
	// the series_id link is implicit (episode → season.parent_id →
	// series). The series + season rows are created lazily on first
	// encounter and cached for the rest of the scan.
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
		// `match.OK == false` (flat layout): the episode lands as a
		// type=episode row with no parent. Deliberate — better to
		// keep the file visible somewhere than drop it on the floor.
	}

	item := &db.Item{
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

	// Store streams
	streams := probeResultToStreams(itemID, probeResult)
	if len(streams) > 0 {
		if err := s.streams.ReplaceForItem(ctx, itemID, streams); err != nil {
			s.logger.Warn("failed to store streams", "item_id", itemID, "error", err)
		}
	}

	// Store chapters. Optional dependency — older test environments
	// build a Scanner without one and skip the persistence silently.
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

	// Re-derive chapters: a re-encode may have shifted markers, and
	// `Replace` clears the old set transactionally before inserting,
	// so a probe with zero chapters intentionally clears any stale
	// markers from a previous version of the file.
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

// probeResultToChapters converts the probe-time Chapter slice to the
// DB shape (item_id-keyed, ticks instead of time.Duration). Returns
// nil for an empty input so the caller can pass it straight to
// Replace without a length check — Replace's transactional
// clear-then-insert handles the empty case.
func probeResultToChapters(pr *probe.Result) []db.Chapter {
	if len(pr.Chapters) == 0 {
		return nil
	}
	out := make([]db.Chapter, len(pr.Chapters))
	for i, c := range pr.Chapters {
		out[i] = db.Chapter{
			StartTicks: probe.DurationTicks(c.Start),
			EndTicks:   probe.DurationTicks(c.End),
			Title:      c.Title,
		}
	}
	return out
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

// enrichIfMissing re-runs metadata enrichment for existing items that lack
// metadata (e.g. because the TMDB API key was not configured during the
// initial scan).
func (s *Scanner) enrichIfMissing(ctx context.Context, item *db.Item) {
	if s.providers == nil {
		return
	}
	// Check if this item already has images (poster) — if so, skip
	imgs, err := s.images.ListByItem(ctx, item.ID)
	if err == nil && len(imgs) > 0 {
		return // already enriched
	}
	s.logger.Info("re-enriching item missing metadata", "title", item.Title, "id", item.ID)
	s.enrichMetadata(ctx, item)
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

	// Fetch and store images. The scanner downloads each candidate to
	// local storage and records `/api/v1/images/file/{id}` as the
	// path — never the upstream URL. Persisting remote URLs would
	// leak the user's IP/User-Agent to TMDb on every poster view and
	// break the library the day TMDb is unreachable.
	//
	// imageDir + pathmap are optional dependencies: tests that don't
	// exercise the artwork pipeline can construct a Scanner without
	// them, and image enrichment is skipped silently rather than
	// falling back to URL persistence.
	if len(meta.ExternalIDs) > 0 && s.imageDir != "" && s.pathmap != nil {
		s.fetchAndStoreImages(ctx, item.ID, meta.ExternalIDs, itemType)
	}

	s.logger.Info("enriched metadata", "title", item.Title, "tmdb_id", best.ExternalID, "year", item.Year)
}

// fetchAndStoreImages picks the highest-scored candidate for each kind
// (primary, backdrop, logo) the providers return, downloads it via
// imaging.IngestRemoteImage (SSRF + size + blurhash + atomic write),
// and persists a db.Image row pointing at the local file.
//
// Errors per image are logged and skipped — losing one poster is
// strictly better than failing the whole scan. The first stored image
// of each kind becomes that kind's primary; subsequent items of the
// same kind in the same call are dropped.
func (s *Scanner) fetchAndStoreImages(ctx context.Context, itemID string, externalIDs map[string]string, itemType provider.ItemType) {
	results, err := s.providers.FetchImages(ctx, externalIDs, itemType)
	if err != nil {
		s.logger.Debug("provider image fetch failed", "id", itemID, "error", err)
		return
	}
	if len(results) == 0 {
		return
	}

	// Pick the best-scored candidate per kind so the scanner doesn't
	// settle for whatever happened to come first in the merged
	// provider response. Mirrors the selection logic ImageRefresher
	// already uses for manual refreshes — same input shape, same
	// ranking, so re-runs are stable.
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
		// Provider name comes straight from the Manager-stamped
		// `Source` field on the result — no URL sniffing. Falls back
		// to "unknown" only if a future provider implementation forgets
		// to surface its name through the manager (today neither
		// TMDb nor Fanart can hit this branch).
		providerName := best.Source
		if providerName == "" {
			providerName = "unknown"
		}
		dbImg := &db.Image{
			ID:        imgID,
			ItemID:    itemID,
			Type:      kind,
			Path:      "/api/v1/images/file/" + imgID,
			Width:     best.Width,
			Height:    best.Height,
			Blurhash:  ing.Blurhash,
			Provider:  providerName,
			IsPrimary: true,
			AddedAt:   time.Now(),
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
	// Hash first 64KB for speed
	if _, err := io.CopyN(h, f, 65536); err != nil && err != io.EOF {
		return "", fmt.Errorf("hashing %q: %w", path, err)
	}

	return fmt.Sprintf("%d:%x", info.Size(), h.Sum(nil)[:16]), nil
}
