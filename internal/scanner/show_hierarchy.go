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
//
// On the new-row branch this ALSO triggers metadata + image
// enrichment for the series. Why here and not at the episode level:
//
//   - The user sees `/series` first; it needs the poster + backdrop
//     to look like a real library, not a wall of placeholder letters.
//   - Per-episode TMDb search runs N times per series and butchers
//     the title (`Breaking.Bad.S01E01.Pilot` → no match). Series-
//     level search runs ONCE per series with the clean dir name
//     (`Breaking Bad`) and the matcher actually finds it.
//   - The image refresher iterates over series rows (parent_id IS
//     NULL filter) and looks up `external_ids` per item. Without
//     series-level enrichment those external_ids stay empty and the
//     "Refresh images" admin button reports "0 actualizadas" — the
//     exact symptom the user hit.
//
// Enrichment is best-effort: provider down / no API key / no match
// all leave the row in DB without metadata, and a future scan can
// retry via the `RefreshMetadata` admin endpoint.
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

	// Series-level metadata + images. Best-effort, never blocks the
	// scan — the series row exists either way; missing artwork is
	// recoverable via the manual refresh button.
	s.enrichMetadata(ctx, item)
	return id, nil
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
