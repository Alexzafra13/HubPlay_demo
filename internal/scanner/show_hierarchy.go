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
// `checkedEnrichment` is the per-scan dedupe for the self-healing
// metadata path: when a series row exists in DB but has no metadata
// yet (because the previous scan ran without TMDb configured, or
// the matcher missed), we want to retry on the next scan. Without
// this map we'd run that retry once per episode of the series — a
// 100-episode show would burn 100 image-list lookups for no reason.
//
// Concurrency: ScanLibrary runs on a single goroutine per library
// today, but the Service.Scan layer can fire two scans for two
// different libraries in parallel. Each gets its own cache instance
// so the maps don't interleave; the mutex inside is for symmetry
// with the iteration callback (which is ALSO single-threaded today
// but cheap to harden).
type showCache struct {
	mu                sync.Mutex
	series            map[string]string // seriesName → series item id
	season            map[string]string // "<seriesID>|<seasonNum>" → season item id
	checkedEnrichment map[string]bool   // series item id → already checked-or-enriched this scan
}

func newShowCache() *showCache {
	return &showCache{
		series:            make(map[string]string),
		season:            make(map[string]string),
		checkedEnrichment: make(map[string]bool),
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
// name, creating one if it doesn't exist yet for the library, AND
// triggers metadata enrichment when it's missing (whether the row
// is brand new or pre-existing without metadata). Self-healing: a
// library scanned without TMDb configured will fill in posters
// automatically the next time it scans with TMDb available — no
// admin action required.
//
// Why metadata lives at the series level, not per-episode:
//
//   - The user sees `/series` first; it needs the poster + backdrop
//     to look like a real library, not a wall of placeholder letters.
//   - Per-episode TMDb search butchers the title
//     (`Breaking.Bad.S01E01.Pilot` → no match). Series-level search
//     uses the clean dir name (`Breaking Bad`) which the matcher
//     actually finds.
//   - The image refresher iterates root items (parent_id IS NULL =
//     series + movies) and looks up external_ids per item. Without
//     series-level enrichment external_ids stay empty and the
//     "Refresh images" button reports "0 actualizadas".
//
// Enrichment is best-effort: provider down / no API key / no match
// leave the row visible in DB without metadata, and the NEXT scan
// retries automatically.
func (s *Scanner) ensureSeriesRow(ctx context.Context, lib *db.Library, cache *showCache, seriesName string) (string, error) {
	cache.mu.Lock()
	if id, ok := cache.series[seriesName]; ok {
		alreadyChecked := cache.checkedEnrichment[id]
		cache.mu.Unlock()
		if !alreadyChecked {
			s.checkAndEnrichSeries(ctx, cache, id)
		}
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
	cache.checkedEnrichment[id] = true
	cache.mu.Unlock()

	s.enrichMetadata(ctx, item)
	return id, nil
}

// checkAndEnrichSeries runs at most once per series per scan. The
// shape of the check is "does this series already have any image?"
// — `enrichIfMissing` does that bookkeeping internally, so we just
// need the GetByID + the cache flag. With a 100-episode show, this
// goes from "100 enrichIfMissing calls per scan" to "1 per scan".
func (s *Scanner) checkAndEnrichSeries(ctx context.Context, cache *showCache, seriesID string) {
	cache.mu.Lock()
	cache.checkedEnrichment[seriesID] = true
	cache.mu.Unlock()
	item, err := s.items.GetByID(ctx, seriesID)
	if err != nil || item == nil {
		return
	}
	s.enrichIfMissing(ctx, item)
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
