package scanner

// Enrichment de seasons + episodes. A diferencia de movies/series
// (enrich.go) — que buscan por título en TMDb — aquí siempre llegamos
// con el tmdb id de la serie padre y pedimos directamente
// `/tv/{id}/season/{n}` o `/tv/{id}/season/{n}/episode/{m}`. Sin esto
// una serie de 100 capítulos quemaría 100 búsquedas para resultados
// que ni se enseñan.
//
// Cada función pública (`enrichSeason`, `enrichEpisode`) tiene su
// `fetchAndStore*` específico — episodes guardan un still como
// `backdrop`, seasons guardan un poster como `primary`. Se mantienen
// separados del flujo de movies/series (`fetchAndStoreImages`) porque
// el provider devuelve campos distintos (`StillURL` vs `PosterURL` vs
// `ExternalIDs`) y el bucket de Image type difiere.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hubplay/internal/imaging"
	librarymodel "hubplay/internal/library/model"

	"github.com/google/uuid"
)

// enrichSeason pide los metadatos de una temporada usando el id de TMDb
// de la serie padre. Si falla algo (sin id, sin provider, 404 en TMDb),
// se deja como está y el siguiente scan lo intenta de nuevo.
//
// Siempre reemplazamos el título "Temporada N" por lo que diga TMDb,
// para arreglar duplicados tipo "Season 1 / Season 1" que se colaban
// cuando el placeholder se creó sin id de TMDb.
func (s *Scanner) enrichSeason(ctx context.Context, item *librarymodel.Item, seriesID string, seasonNum int) {
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
		if err := s.metadata.Upsert(ctx, &librarymodel.Metadata{
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

// fetchAndStoreSeasonPoster descarga el póster de una temporada y lo
// guarda como imagen principal de esa temporada. Funciona igual que el
// equivalente para episodios.
func (s *Scanner) fetchAndStoreSeasonPoster(ctx context.Context, itemID, posterURL string) {
	dir := filepath.Join(s.imageDir, itemID)
	ing, err := imaging.IngestRemoteImage(dir, "primary", posterURL, s.logger)
	if err != nil {
		s.logger.Warn("scanner: season poster ingest failed", "id", itemID, "error", err)
		return
	}

	imgID := uuid.NewString()
	dbImg := &librarymodel.Image{
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

// enrichEpisode pide los metadatos de un episodio usando el id de TMDb
// de la serie padre. Si falla, el episodio queda visible sin metadatos y
// el siguiente scan lo reintenta.
//
// seasonItemID es la temporada del episodio; subimos un nivel para
// llegar a la serie y leer ahí el id de TMDb.
func (s *Scanner) enrichEpisode(ctx context.Context, item *librarymodel.Item, seasonItemID string, seasonNum, episodeNum int) {
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
		// La serie aún no tiene id de TMDb; lo intentamos en el siguiente scan.
		return
	}

	meta, err := s.providers.FetchEpisodeMetadata(ctx, tmdbID, seasonNum, episodeNum)
	if err != nil || meta == nil {
		s.logger.Debug("episode enrich: provider returned nothing", "tmdb_id", tmdbID, "season", seasonNum, "episode", episodeNum, "error", err)
		return
	}

	// Si TMDb da título, lo usamos: suele ser más limpio que algo tipo
	// "S01E01". No pisamos nombres curados a mano que ya estuvieran.
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
	// La duración de TMDb redondea a minutos y no es precisa. Sólo la
	// usamos si el análisis del fichero no devolvió duración.
	if item.DurationTicks == 0 && meta.RuntimeMinutes > 0 {
		item.DurationTicks = int64(meta.RuntimeMinutes) * 60 * 10_000_000
	}
	item.UpdatedAt = time.Now()
	if err := s.items.Update(ctx, item); err != nil {
		s.logger.Warn("update episode with metadata", "id", item.ID, "error", err)
	}

	if meta.Overview != "" {
		if err := s.metadata.Upsert(ctx, &librarymodel.Metadata{
			ItemID:   item.ID,
			Overview: meta.Overview,
		}); err != nil {
			s.logger.Warn("store episode metadata", "id", item.ID, "error", err)
		}
	}

	// Guardamos el still como "backdrop" del episode para que el item-detail
	// handler (que indexa por type=backdrop para el hero) lo pinte sin
	// distinguir episode/series client-side. SSRF/size/blurhash van por el
	// mismo IngestRemoteImage que series.
	if meta.StillURL != "" && s.imageDir != "" && s.pathmap != nil {
		s.fetchAndStoreEpisodeStill(ctx, item.ID, meta.StillURL)
	}

	s.logger.Info("enriched episode metadata", "title", item.Title, "id", item.ID, "tmdb_show", tmdbID, "season", seasonNum, "episode", episodeNum)
}

// fetchAndStoreEpisodeStill: ingest de 1 still TMDb + Image row `backdrop`
// del episode. Sin loop per-kind: episodes no tienen poster ni logo en TMDb.
func (s *Scanner) fetchAndStoreEpisodeStill(ctx context.Context, itemID, stillURL string) {
	dir := filepath.Join(s.imageDir, itemID)
	ing, err := imaging.IngestRemoteImage(dir, "backdrop", stillURL, s.logger)
	if err != nil {
		s.logger.Warn("scanner: episode still ingest failed", "id", itemID, "error", err)
		return
	}

	imgID := uuid.NewString()
	dbImg := &librarymodel.Image{
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
