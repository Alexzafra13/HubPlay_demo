package scanner

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	librarymodel "hubplay/internal/library/model"
)

// showCache: series + season rows ya conocidos durante un ScanLibrary.
// Pre-poblado desde DB al arrancar y extendido on-the-fly por el walker.
//
// checkedEnrichment: dedupe per-scan del self-heal — sin él, una serie de
// 100 caps quemaría 100 image-list lookups (uno por episode).
//
// Concurrencia: el cache es per-library, así dos scans paralelos en
// libraries distintas no interleavan. El mutex es por simetría con el
// callback de iteración (también single-threaded hoy, barato endurecerlo).
type showCache struct {
	mu                      sync.Mutex
	series                  map[string]string // seriesName → series item id
	season                  map[string]string // "<seriesID>|<seasonNum>" → season item id
	checkedEnrichment       map[string]bool   // series id → ya chequeado este scan
	checkedSeasonEnrichment map[string]bool   // season id → ya chequeado este scan
}

func newShowCache() *showCache {
	return &showCache{
		series:                  make(map[string]string),
		season:                  make(map[string]string),
		checkedEnrichment:       make(map[string]bool),
		checkedSeasonEnrichment: make(map[string]bool),
	}
}

// rememberSeries / rememberSeason: las llama la pasada inicial de DB para
// sembrar el cache; el walker luego muta los mismos maps vía ensure*Row.
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

// ensureSeriesRow: id del row series matching, creándolo si falta, Y dispara
// enrichment cuando falta metadata (brand new o pre-existente sin metadata).
// Self-healing: una library scanneada sin TMDb completa posters automático
// en el siguiente scan con TMDb — sin acción de admin.
//
// Por qué metadata va a nivel series, no per-episode:
//   - El usuario abre /series primero; necesita poster + backdrop.
//   - Search TMDb per-episode destroza títulos ("Breaking.Bad.S01E01.Pilot"
//     → 0 matches). Search a nivel series usa el dir limpio ("Breaking Bad").
//   - El image refresher itera root items (parent_id IS NULL); sin enrichment
//     a nivel series, external_ids quedan vacíos y "Refresh images" reporta 0.
//
// Best-effort: provider caído / sin API key / sin match → row visible sin
// metadata, el siguiente scan retry.
func (s *Scanner) ensureSeriesRow(ctx context.Context, lib *librarymodel.Library, cache *showCache, seriesName string) (string, error) {
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
	item := &librarymodel.Item{
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

// checkAndEnrichSeries: como máximo 1 vez per series per scan. Convierte
// "100 enrichIfMissing per scan" en "1 per scan" en series largas.
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

// ensureSeasonRow: idem para season bajo una series. Title default
// "Season N" — TMDb lo sobreescribe en la misma call si hay API key
// (nombres más amigables: "Specials", "The Final Chapter"). Self-heal
// captura seasons creadas en scans previos sin TMDb.
func (s *Scanner) ensureSeasonRow(ctx context.Context, lib *librarymodel.Library, cache *showCache, seriesID string, seasonNum int) (string, error) {
	key := seasonKey(seriesID, seasonNum)
	cache.mu.Lock()
	if id, ok := cache.season[key]; ok {
		alreadyChecked := cache.checkedSeasonEnrichment[id]
		cache.mu.Unlock()
		if !alreadyChecked {
			s.checkAndEnrichSeason(ctx, cache, id, seriesID, seasonNum)
		}
		return id, nil
	}
	cache.mu.Unlock()

	id := uuid.NewString()
	now := time.Now()
	title := fmt.Sprintf("Season %d", seasonNum)
	sn := seasonNum
	item := &librarymodel.Item{
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
	cache.checkedSeasonEnrichment[id] = true
	cache.mu.Unlock()

	// Enrich síncrono al crear: TMDb da título limpio, overview, premiere,
	// rating y poster en 1 sola call. La SeasonGrid renderiza bien ya en
	// el primer hit tras el scan.
	s.enrichSeason(ctx, item, seriesID, seasonNum)
	return id, nil
}

// checkAndEnrichSeason: espejo de checkAndEnrichSeries — máx. 1 enrichment
// per season per scan. Sin esto, una season de 22 caps refetchearía TMDb
// 22 veces.
func (s *Scanner) checkAndEnrichSeason(ctx context.Context, cache *showCache, seasonID, seriesID string, seasonNum int) {
	cache.mu.Lock()
	cache.checkedSeasonEnrichment[seasonID] = true
	cache.mu.Unlock()
	item, err := s.items.GetByID(ctx, seasonID)
	if err != nil || item == nil {
		return
	}
	// Solo re-enrich si la season sigue sin imágenes.
	imgs, err := s.images.ListByItem(ctx, seasonID)
	if err == nil && len(imgs) > 0 {
		return
	}
	s.enrichSeason(ctx, item, seriesID, seasonNum)
}
