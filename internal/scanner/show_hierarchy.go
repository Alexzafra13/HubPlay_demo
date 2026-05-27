package scanner

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"

	librarymodel "hubplay/internal/library/model"
)

// showCache guarda en memoria las series y temporadas que ya conocemos
// durante un scan, para no buscarlas en la BD una y otra vez.
//
// checkedEnrichment evita repetir el chequeo de metadatos faltantes: sin
// él, una serie de 100 capítulos haría 100 consultas a la BD para
// preguntar lo mismo.
type showCache struct {
	mu                      sync.Mutex
	series                  map[string]string // nombre de serie → id
	season                  map[string]string // "<seriesID>|<num>" → id
	checkedEnrichment       map[string]bool   // serie ya repasada en este scan
	checkedSeasonEnrichment map[string]bool   // temporada ya repasada en este scan
}

func newShowCache() *showCache {
	return &showCache{
		series:                  make(map[string]string),
		season:                  make(map[string]string),
		checkedEnrichment:       make(map[string]bool),
		checkedSeasonEnrichment: make(map[string]bool),
	}
}

// rememberSeries y rememberSeason rellenan el caché con lo que ya hay
// en la BD al arrancar el scan.
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

// ensureSeriesRow devuelve el id de la serie, creándola si no existe, y
// además dispara la búsqueda de metadatos si no los tiene. Esto hace que
// una biblioteca escaneada sin TMDb configurado se "cure sola" la
// siguiente vez que escanee con TMDb disponible.
//
// Los metadatos se buscan a nivel serie y no a nivel episodio porque la
// vista /series necesita el póster, y porque buscar episodio a episodio
// con títulos como "Breaking.Bad.S01E01" no encuentra nada en TMDb.
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
	now := s.clock.Now()
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

// checkAndEnrichSeries intenta rellenar metadatos como mucho una vez por
// serie en cada scan. Sin esto, una serie de 100 capítulos haría 100
// comprobaciones idénticas.
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

// ensureSeasonRow hace lo mismo que ensureSeriesRow pero para una
// temporada. El título por defecto es "Temporada N"; TMDb lo cambia por
// uno más bonito ("Especiales", "Capítulo final") si hay API key.
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
	now := s.clock.Now()
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

	// Pedimos los metadatos a TMDb ya al crearla: en una sola llamada
	// caen título, sinopsis, fecha, valoración y póster. Así la vista de
	// temporadas se ve bien ya en la primera carga tras el scan.
	s.enrichSeason(ctx, item, seriesID, seasonNum)
	return id, nil
}

// checkAndEnrichSeason es el equivalente para temporadas: como mucho una
// vez por temporada en cada scan. Sin esto, una temporada de 22
// capítulos pediría 22 veces los mismos metadatos a TMDb.
func (s *Scanner) checkAndEnrichSeason(ctx context.Context, cache *showCache, seasonID, seriesID string, seasonNum int) {
	cache.mu.Lock()
	cache.checkedSeasonEnrichment[seasonID] = true
	cache.mu.Unlock()
	item, err := s.items.GetByID(ctx, seasonID)
	if err != nil || item == nil {
		return
	}
	// Sólo lo reintentamos si la temporada sigue sin ninguna imagen.
	imgs, err := s.images.ListByItem(ctx, seasonID)
	if err == nil && len(imgs) > 0 {
		return
	}
	s.enrichSeason(ctx, item, seriesID, seasonNum)
}
