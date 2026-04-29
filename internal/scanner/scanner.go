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
	FetchEpisodeMetadata(ctx context.Context, showExternalID string, seasonNumber, episodeNumber int) (*provider.EpisodeMetadataResult, error)
	FetchSeasonMetadata(ctx context.Context, showExternalID string, seasonNumber int) (*provider.SeasonMetadataResult, error)
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
	people      *db.PeopleRepository
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
		people:      people,
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
	// Collect existing season rows here so we can run a self-healing
	// enrichment pass *after* iteration completes — episodes are
	// enriched lazily via processFile→enrichIfMissing, but seasons are
	// aggregate rows with no file path, so processFile never visits
	// them. Without this sweep a season row created on a previous
	// (TMDb-less) scan would stay poster-less forever.
	var existingSeasons []*db.Item
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
				// Snapshot — sweep runs below, after the walker has
				// had a chance to enrich the parent series (which is
				// what holds the tmdb id we need).
				copy := *item
				existingSeasons = append(existingSeasons, &copy)
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

	// Self-heal seasons that pre-existed without metadata. enrichIfMissing
	// is a no-op when the row already has images, so this re-pass is cheap
	// for fully-enriched libraries. Critical for users who scanned without
	// TMDb configured or before the season-enrichment code shipped — the
	// SeasonGrid card needs the poster/title/rating that this populates.
	for _, season := range existingSeasons {
		s.enrichIfMissing(ctx, season)
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

// iterateLibraryItems pages through every item in a library — series,
// season, episode, movie — calling fn for each. The default
// `db.ItemFilter` projects to root items only (parent_id IS NULL),
// which used to silently miss every season + episode in shows
// libraries; we explicitly enumerate by type so the iteration
// returns the full graph.
//
// Pagination uses the actual returned slice length to detect the
// last page, NOT the requested pageSize — `db.ItemFilter.List`
// caps Limit at 100 internally, so requesting 500 returns 100, and
// a `len < requested` check would always fire after the first batch
// (the bug that caused libraries with >100 items to lose cache
// entries on re-scan).
func (s *Scanner) iterateLibraryItems(ctx context.Context, libraryID string, fn func(*db.Item)) error {
	const pageSize = 100 // matches the upper bound enforced by ItemFilter.
	for _, t := range []string{"series", "season", "episode", "movie", "audio"} {
		offset := 0
		for {
			items, _, err := s.items.List(ctx, db.ItemFilter{
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
//
// Dispatch by item type matters: enrichMetadata only handles movies and
// series (TMDb search by title). Episodes need enrichEpisode (TMDb
// /tv/.../season/N/episode/M, keyed off the parent series' tmdb id);
// seasons need enrichSeason (TMDb /tv/.../season/N). Without the
// dispatch, RefreshMetadata wipes episode/season images + overviews
// and never restores them — the previous behaviour, which is why a
// "Refrescar metadatos" run left the SeasonDetail page with no stills
// and no synopses on episode rows.
//
// Iteration order: iterateLibraryItems already walks
// series → season → episode → movie → audio (see scanner.go), which
// means series/external_ids land first; by the time we hit a season
// or episode the parent's tmdb id is fresh in the database.
func (s *Scanner) RefreshMetadata(ctx context.Context, lib *db.Library) error {
	s.logger.Info("refreshing metadata for library", "library", lib.Name)

	count := 0
	err := s.iterateLibraryItems(ctx, lib.ID, func(item *db.Item) {
		// Delete old images and metadata so the enrichment call below
		// repopulates them. Best-effort — a failed delete still lets
		// enrichment overwrite via Upsert.
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

	// Metadata + image fetching:
	//   - Movies, series and audio go through enrichMetadata —
	//     for them the item title IS the searchable name.
	//   - Episodes use a different path: enrichMetadata's TMDb search
	//     would butcher the title ("Breaking.Bad.S01E01") and never
	//     match. Instead we look up the parent series' tmdb id and
	//     query /tv/{id}/season/{n}/episode/{m} directly — clean
	//     overview, still image and air-date with one call.
	if itemType == "episode" {
		if seasonNumber != nil && episodeNumber != nil && parentID != "" {
			s.enrichEpisode(ctx, item, parentID, *seasonNumber, *episodeNumber)
		}
	} else {
		s.enrichMetadata(ctx, item)
	}

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
// initial scan, or because the parent series wasn't enriched yet at the
// time the episode was first inserted).
func (s *Scanner) enrichIfMissing(ctx context.Context, item *db.Item) {
	if s.providers == nil {
		return
	}
	// Check if this item already has images (poster/still) — if so, skip
	imgs, err := s.images.ListByItem(ctx, item.ID)
	if err == nil && len(imgs) > 0 {
		return // already enriched
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

// enrichMetadata searches TMDB for the item and stores metadata + images.
//
// Only series and movies hit TMDb. Episodes and seasons are intentionally
// skipped: their titles are noisy ("Pilot", "Breaking.Bad.S01E01.Pilot",
// "Season 1") and a per-episode lookup of a 100-episode show would burn
// 100 search calls for results we never display — the UI shows series
// posters, not episode posters. RefreshMetadata iterates every row in
// the library, so this guard is what keeps the admin "Refresh metadata"
// button from melting the TMDb quota.
func (s *Scanner) enrichMetadata(ctx context.Context, item *db.Item) {
	if s.providers == nil {
		return
	}
	if item.Type == "episode" || item.Type == "season" {
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

	// Store extended metadata. Trailer key + site come straight from
	// the same TMDb call (videos appended) so we don't pay a second
	// round-trip per item just to get the YouTube id.
	genresJSON, _ := json.Marshal(meta.Genres)
	tagsJSON, _ := json.Marshal(meta.Tags)
	if err := s.metadata.Upsert(ctx, &db.Metadata{
		ItemID:      item.ID,
		Overview:    meta.Overview,
		Tagline:     meta.Tagline,
		Studio:      meta.Studio,
		GenresJSON:  string(genresJSON),
		TagsJSON:    string(tagsJSON),
		TrailerKey:  meta.TrailerKey,
		TrailerSite: meta.TrailerSite,
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

	// Persist cast/crew. Best-effort like every other enrichment step;
	// failures inside syncPeople are logged but never stop the scan.
	s.syncPeople(ctx, item.ID, meta.People)

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


// enrichSeason fetches per-season metadata (clean title, overview,
// premiere date, rating, poster) from the configured provider via the
// parent series' TMDb id. Same best-effort discipline as enrichEpisode:
// missing series tmdb id, no provider, or a TMDb 404 leaves the row
// untouched and the next scan retries via checkAndEnrichSeason.
//
// Title overwrite policy: we always replace the placeholder "Season N"
// with whatever TMDb returns, including back to "Season N" when that's
// the canonical title — this fixes the user-visible "Season 1 / Season
// 1" duplicate label in shows where the placeholder slipped through
// without the TMDb id at first scan.
func (s *Scanner) enrichSeason(ctx context.Context, item *db.Item, seriesID string, seasonNum int) {
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
		if err := s.metadata.Upsert(ctx, &db.Metadata{
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

// fetchAndStoreSeasonPoster ingests one TMDb season poster URL into the
// local image store and writes a single primary `primary` (poster) row
// for the season. Mirrors fetchAndStoreEpisodeStill — same SSRF / size /
// blurhash pipeline, different `Type` and target item.
func (s *Scanner) fetchAndStoreSeasonPoster(ctx context.Context, itemID, posterURL string) {
	dir := filepath.Join(s.imageDir, itemID)
	ing, err := imaging.IngestRemoteImage(dir, "primary", posterURL, s.logger)
	if err != nil {
		s.logger.Warn("scanner: season poster ingest failed", "id", itemID, "error", err)
		return
	}

	imgID := uuid.NewString()
	dbImg := &db.Image{
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

// enrichEpisode fetches per-episode metadata (overview, air date, rating,
// runtime, still image) from the configured provider via the parent
// series' TMDb id. Best-effort: missing series tmdb id, missing provider,
// or a 404 from TMDb leave the row visible without metadata, and the
// next scan re-tries automatically (enrichIfMissing keys off the absence
// of any image row, just like the series path).
//
// `seasonItemID` is the season row that owns the episode; we climb one
// link up to the series to read its external_ids. The walker already
// hands us this id when the show-hierarchy match succeeded.
func (s *Scanner) enrichEpisode(ctx context.Context, item *db.Item, seasonItemID string, seasonNum, episodeNum int) {
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
		// Series wasn't enriched yet (no TMDb match or API key absent
		// at series-creation time). The next scan will retry once the
		// series picks up its tmdb id.
		return
	}

	meta, err := s.providers.FetchEpisodeMetadata(ctx, tmdbID, seasonNum, episodeNum)
	if err != nil || meta == nil {
		s.logger.Debug("episode enrich: provider returned nothing", "tmdb_id", tmdbID, "season", seasonNum, "episode", episodeNum, "error", err)
		return
	}

	// Update item fields. Title swap is conditional: TMDb's title is
	// usually cleaner than the file-derived one ("S01E01" → "Pilot"),
	// but we don't want to overwrite a name the user might have curated.
	// The file-derived title only sticks when it isn't a generic
	// SxxExx code.
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
	// RuntimeMinutes from TMDb is rarely accurate enough to overwrite
	// the probe-derived DurationTicks (TMDb rounds to whole minutes,
	// the probe knows the file to the millisecond). We only fill it
	// when the probe didn't get a duration at all — better than zero.
	if item.DurationTicks == 0 && meta.RuntimeMinutes > 0 {
		item.DurationTicks = int64(meta.RuntimeMinutes) * 60 * 10_000_000
	}
	item.UpdatedAt = time.Now()
	if err := s.items.Update(ctx, item); err != nil {
		s.logger.Warn("update episode with metadata", "id", item.ID, "error", err)
	}

	if meta.Overview != "" {
		if err := s.metadata.Upsert(ctx, &db.Metadata{
			ItemID:   item.ID,
			Overview: meta.Overview,
		}); err != nil {
			s.logger.Warn("store episode metadata", "id", item.ID, "error", err)
		}
	}

	// Persist the still as the episode's "backdrop" so the existing
	// item-detail handler (which keys off type=backdrop for the hero
	// image) renders it without any client-side knowledge of episode
	// vs. series visuals. SSRF + size + blurhash all flow through the
	// same imaging.IngestRemoteImage path the series enrichment uses.
	if meta.StillURL != "" && s.imageDir != "" && s.pathmap != nil {
		s.fetchAndStoreEpisodeStill(ctx, item.ID, meta.StillURL)
	}

	s.logger.Info("enriched episode metadata", "title", item.Title, "id", item.ID, "tmdb_show", tmdbID, "season", seasonNum, "episode", episodeNum)
}

// fetchAndStoreEpisodeStill ingests one TMDb still URL into the local
// image store and writes a single primary `backdrop` row for the episode.
// Single-image counterpart to fetchAndStoreImages — episodes don't have
// posters or logos on TMDb, so we skip the per-kind selection loop.
func (s *Scanner) fetchAndStoreEpisodeStill(ctx context.Context, itemID, stillURL string) {
	dir := filepath.Join(s.imageDir, itemID)
	ing, err := imaging.IngestRemoteImage(dir, "backdrop", stillURL, s.logger)
	if err != nil {
		s.logger.Warn("scanner: episode still ingest failed", "id", itemID, "error", err)
		return
	}

	imgID := uuid.NewString()
	dbImg := &db.Image{
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

// syncPeople persists the cast/crew the metadata provider returned
// for an item. Idempotent: existing item_people rows are wiped before
// re-insert so re-scans pick up cast changes (e.g. an episode's
// guest-star list updating in TMDb) without leaving stale rows.
//
// Photos: people are deduplicated by name, so the FIRST item that
// surfaces a person triggers the profile photo download. Subsequent
// items reuse the existing row and skip the network. Failed photo
// downloads are logged and swallowed — the cast row still gets
// persisted with an empty thumb_path so the UI can fall back to the
// initial-letter chip.
//
// People are stored under <imageDir>/.people/<personID>/ — the dot
// prefix keeps them out of regular per-item directories during
// listing, and the per-person subdir means delete-by-person is a
// single os.RemoveAll.
func (s *Scanner) syncPeople(ctx context.Context, itemID string, people []provider.Person) {
	if s.people == nil || len(people) == 0 {
		return
	}

	credits := make([]db.ItemPersonCredit, 0, len(people))
	for _, p := range people {
		if p.Name == "" {
			continue
		}
		personID, created, err := s.people.EnsureByName(ctx, p.Name, p.Role)
		if err != nil {
			s.logger.Warn("person upsert failed", "name", p.Name, "error", err)
			continue
		}
		// Photo download: only for newly created people, and only when
		// the provider supplied a URL. The IngestRemoteImage helper
		// runs the same SSRF / max-bytes / atomic-write pipeline used
		// by item posters so we don't re-implement safety here.
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
		credits = append(credits, db.ItemPersonCredit{
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
