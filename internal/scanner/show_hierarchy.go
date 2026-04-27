package scanner

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"hubplay/internal/db"
)

// showCache holds the series + season `db.Item` rows already known to
// the scanner during one ScanLibrary run. Pre-populated from the DB
// at the start of the scan (same pass as `existingPaths`), then
// extended in-place as the walker discovers new series.
//
// Concurrency: ScanLibrary runs on a single goroutine per library
// today, but the Service.Scan layer can fire two scans for two
// different libraries in parallel. Each gets its own cache instance
// so the maps don't interleave; the mutex inside is for symmetry
// with the iteration callback (which is ALSO single-threaded today
// but cheap to harden).
type showCache struct {
	mu     sync.Mutex
	series map[string]string // seriesName → series item id
	season map[string]string // "<seriesID>|<seasonNum>" → season item id
}

func newShowCache() *showCache {
	return &showCache{
		series: make(map[string]string),
		season: make(map[string]string),
	}
}

// rememberSeries / rememberSeason are called during the initial DB
// pass to seed the cache from existing rows. The walker then mutates
// the same maps via ensure*Row.
func (c *showCache) rememberSeries(name, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.series[name] = id
}

func (c *showCache) rememberSeason(seriesID string, seasonNum int, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.season[seasonKey(seriesID, seasonNum)] = id
}

func seasonKey(seriesID string, seasonNum int) string {
	return seriesID + "|" + strconv.Itoa(seasonNum)
}

// ensureSeriesRow returns the id of the series row matching this
// name, creating one if it doesn't exist yet for the library. Pure
// in-memory cache lookup most of the time — only the very first
// encounter per series during the scan (or after a fresh server
// boot) writes to the DB.
func (s *Scanner) ensureSeriesRow(ctx context.Context, lib *db.Library, cache *showCache, seriesName string) (string, error) {
	cache.mu.Lock()
	if id, ok := cache.series[seriesName]; ok {
		cache.mu.Unlock()
		return id, nil
	}
	cache.mu.Unlock()

	id := uuid.NewString()
	now := time.Now()
	item := &db.Item{
		ID:          id,
		LibraryID:   lib.ID,
		Type:        "series",
		Title:       seriesName,
		SortTitle:   strings.ToLower(seriesName),
		AddedAt:     now,
		UpdatedAt:   now,
		IsAvailable: true,
	}
	if err := s.items.Create(ctx, item); err != nil {
		return "", fmt.Errorf("create series row %q: %w", seriesName, err)
	}
	cache.mu.Lock()
	cache.series[seriesName] = id
	cache.mu.Unlock()
	return id, nil
}

// backfillShowHierarchy attaches an existing parent-less episode row
// to the series + season parents the scanner now expects. The whole
// flow piggy-backs on the same ParseEpisode + ensure*Row helpers used
// for fresh inserts; the only extra step is `items.Update` so the
// existing row picks up the new parent + S/E numbers in place.
//
// Why this matters: the user reported `/series` empty after re-scanning
// a library that had been scanned BEFORE the hierarchy code landed.
// `processFile` short-circuits to `enrichIfMissing` when the fingerprint
// matches, never reaching `createItem` where the new logic lives — so
// legacy rows would stay parent-less forever without this backfill.
//
// Safe to call repeatedly: ParseEpisode is pure, ensure*Row consult
// the cache before inserting, and Update is a no-op when the row is
// already correct.
func (s *Scanner) backfillShowHierarchy(ctx context.Context, lib *db.Library, libRoot string, item *db.Item, cache *showCache) error {
	match := ParseEpisode(libRoot, item.Path)
	if !match.OK {
		// Path doesn't fit the standard layout; we'd rather leave the
		// row visible (with no parent) than guess wrong.
		return nil
	}
	seriesID, err := s.ensureSeriesRow(ctx, lib, cache, match.SeriesName)
	if err != nil {
		return fmt.Errorf("ensure series: %w", err)
	}
	seasonID, err := s.ensureSeasonRow(ctx, lib, cache, seriesID, match.SeasonNumber)
	if err != nil {
		return fmt.Errorf("ensure season: %w", err)
	}

	sn := match.SeasonNumber
	en := match.EpisodeNumber
	item.SeasonNumber = &sn
	item.EpisodeNumber = &en
	if match.EpisodeTitle != "" && item.Title == "" {
		// Don't overwrite a title the matcher / TMDb pass already set;
		// only fill in the parsed title when nothing better is present.
		item.Title = match.EpisodeTitle
		item.SortTitle = strings.ToLower(match.EpisodeTitle)
	}
	item.UpdatedAt = time.Now()
	// Item.Update doesn't propagate parent_id (re-parenting is rare
	// elsewhere in the codebase). The dedicated SetParent endpoint is
	// the only path that does — call it BEFORE the regular Update so
	// a partial failure leaves the parent change visible (if Update
	// fails after, the user still sees the show in /series).
	if err := s.items.SetParent(ctx, item.ID, seasonID); err != nil {
		return fmt.Errorf("set parent: %w", err)
	}
	item.ParentID = seasonID
	if err := s.items.Update(ctx, item); err != nil {
		return fmt.Errorf("update episode row: %w", err)
	}
	s.logger.Info("backfilled show hierarchy",
		"item_id", item.ID, "series", match.SeriesName,
		"season", match.SeasonNumber, "episode", match.EpisodeNumber)
	return nil
}

// ensureSeasonRow does the same for a season under a given series.
// Title defaults to "Season N" — the metadata pass can override it
// later if the matcher has a friendlier name from TMDb.
func (s *Scanner) ensureSeasonRow(ctx context.Context, lib *db.Library, cache *showCache, seriesID string, seasonNum int) (string, error) {
	key := seasonKey(seriesID, seasonNum)
	cache.mu.Lock()
	if id, ok := cache.season[key]; ok {
		cache.mu.Unlock()
		return id, nil
	}
	cache.mu.Unlock()

	id := uuid.NewString()
	now := time.Now()
	title := fmt.Sprintf("Season %d", seasonNum)
	sn := seasonNum
	item := &db.Item{
		ID:           id,
		LibraryID:    lib.ID,
		ParentID:     seriesID,
		Type:         "season",
		Title:        title,
		SortTitle:    strings.ToLower(title),
		SeasonNumber: &sn,
		AddedAt:      now,
		UpdatedAt:    now,
		IsAvailable:  true,
	}
	if err := s.items.Create(ctx, item); err != nil {
		return "", fmt.Errorf("create season row %d: %w", seasonNum, err)
	}
	cache.mu.Lock()
	cache.season[key] = id
	cache.mu.Unlock()
	return id, nil
}
