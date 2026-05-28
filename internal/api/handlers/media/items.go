package media

import (
	"log/slog"

	"hubplay/internal/api/handlers"
	librarymodel "hubplay/internal/library/model"
)

// ItemHandler es el facade que ensambla los 5 sub-handlers que cubren
// el surface completo del item (cierre del olor P del audit
// 2026-05-14: god-handler 1186 LoC, 13 deps, 4 responsabilidades).
// Embedding por puntero promueve todos los métodos al facade así
// router.go y los tests llaman `itemHandler.Method(...)` exacto
// como antes.
//
// Sub-handlers:
//
//	ItemDetailHandler       → Get, Children, buildItemDetail (priv),
//	                          attach×8 helpers, callerCapRating (priv)
//	TrickplayHandler        → TrickplayManifest, TrickplaySprite,
//	                          WaitTrickplayInflight + estado mutex/WG
//	                          propio
//	SearchHandler           → Search (con filter surface + cap rating)
//	RecommendationsHandler  → Recommendations (TMDb passthrough +
//	                          in-library cross-reference)
//	MetadataHandler         → IdentifyCandidates, Identify,
//	                          UpdateItemMetadata, SetMetadataLock,
//	                          RefreshItemMetadata + MetadataIdentifier
//	                          interface
//
// El facade no lleva fields propios — todo el estado está en los
// sub-handlers. NewItemHandler distribuye las 13 deps del input a
// los constructores específicos de cada sub-handler. La firma
// pública de NewItemHandler no cambia respecto al monolítico
// pre-split para que router.go + tests externos sigan funcionando
// sin modificar nada.
type ItemHandler struct {
	*ItemDetailHandler
	*TrickplayHandler
	*SearchHandler
	*RecommendationsHandler
	*MetadataHandler
}

// itemLibrary es la unión de las micro-interfaces que los sub-handlers
// del item facade necesitan. *library.Service la satisface.
type itemLibrary interface {
	itemDetailFetcher
	itemSearcher
	itemGetter
	trickplayItemLookup
}

// NewItemHandler construye el facade + cada uno de los 5 sub-handlers.
func NewItemHandler(lib itemLibrary, images handlers.ImageRepository, metadata handlers.MetadataRepository, userData handlers.UserDataRepository, users userProfileLookup, chapters handlers.ChapterRepository, segments handlers.EpisodeSegmentRepository, externalIDs handlers.ExternalIDsRepository, people handlers.PeopleRepoForItems, collections handlers.CollectionRepoForItems, providers handlers.ProviderManager, identifier MetadataIdentifier, trickplayDir string, audit handlers.AuditEmitter, logger *slog.Logger) *ItemHandler {
	return &ItemHandler{
		ItemDetailHandler:      newItemDetailHandler(lib, images, metadata, userData, users, chapters, segments, externalIDs, people, collections, identifier, logger),
		TrickplayHandler:       newTrickplayHandler(lib, trickplayDir, logger),
		SearchHandler:          newSearchHandler(lib, images, userData, users, logger),
		RecommendationsHandler: newRecommendationsHandler(lib, externalIDs, providers, logger),
		MetadataHandler:        newMetadataHandler(identifier, audit, logger),
	}
}

// ─── Free helpers ───────────────────────────────────────────────────────────
//
// Funciones de shape compartidas entre todos los sub-handlers. Viven
// como package-level free functions porque sirven a múltiples
// sub-handlers (itemDetailResponse / streamResponse en
// ItemDetailHandler, attachPosterPlaceholder en
// ItemDetailHandler.Children + SearchHandler.Search, userDataResponse
// en ItemDetailHandler + SearchHandler, etc.) — promoverlas a métodos
// de un sub-handler concreto crearía falsas dependencias entre
// sub-handlers que en realidad sólo comparten estos pure helpers.

func itemDetailResponse(item *librarymodel.Item) map[string]any {
	resp := map[string]any{
		"id":             item.ID,
		"library_id":     item.LibraryID,
		"type":           item.Type,
		"title":          item.Title,
		"sort_title":     item.SortTitle,
		"path":           item.Path,
		"size":           item.Size,
		"duration_ticks": item.DurationTicks,
		"container":      item.Container,
		"is_available":   item.IsAvailable,
		"added_at":       item.AddedAt,
		"updated_at":     item.UpdatedAt,
	}
	// `year` se omite cuando es cero para que clientes pinten
	// absence cleanly (la shape previa leakeaba el zero-value de Go
	// como `"year": 0`, que la UI renderizaba literal — ver el
	// empty-episode hero).
	if item.Year > 0 {
		resp["year"] = item.Year
	}
	if item.ParentID != "" {
		resp["parent_id"] = item.ParentID
	}
	if item.OriginalTitle != "" {
		resp["original_title"] = item.OriginalTitle
	}
	if item.SeasonNumber != nil {
		resp["season_number"] = *item.SeasonNumber
	}
	if item.EpisodeNumber != nil {
		resp["episode_number"] = *item.EpisodeNumber
	}
	if item.CommunityRating != nil {
		resp["community_rating"] = *item.CommunityRating
	}
	if item.ContentRating != "" {
		resp["content_rating"] = item.ContentRating
	}
	if item.PremiereDate != nil {
		resp["premiere_date"] = item.PremiereDate
	}
	return resp
}

func streamResponse(s *librarymodel.MediaStream) map[string]any {
	resp := map[string]any{
		"stream_index": s.StreamIndex,
		"stream_type":  s.StreamType,
		"codec":        s.Codec,
		"is_default":   s.IsDefault,
	}
	if s.Profile != "" {
		resp["profile"] = s.Profile
	}
	if s.Bitrate > 0 {
		resp["bitrate"] = s.Bitrate
	}
	if s.Width > 0 {
		resp["width"] = s.Width
		resp["height"] = s.Height
	}
	if s.FrameRate > 0 {
		resp["frame_rate"] = s.FrameRate
	}
	if s.HDRType != "" {
		resp["hdr_type"] = s.HDRType
	}
	if s.Channels > 0 {
		resp["channels"] = s.Channels
		resp["sample_rate"] = s.SampleRate
	}
	if s.Language != "" {
		resp["language"] = s.Language
	}
	if s.Title != "" {
		resp["title"] = s.Title
	}
	return resp
}

// userDataResponse delegates to the exported handlers.UserDataResponse.
func userDataResponse(ud *librarymodel.UserData, durationTicks int64) map[string]any {
	return handlers.UserDataResponse(ud, durationTicks)
}

// chapterResponse es la wire shape para un marker de timeline.
// `title` se emite siempre (empty string cuando unknown) para que
// clientes puedan renderizar o placeholder "Chapter 3" o el nombre
// real sin un presence check; `image_path` se omite cuando ausente
// — los chapter thumbnails Plex-style (BIF) aún no se generan.
func chapterResponse(c *librarymodel.Chapter) map[string]any {
	r := map[string]any{
		"start_ticks": c.StartTicks,
		"end_ticks":   c.EndTicks,
		"title":       c.Title,
	}
	if c.ImagePath != "" {
		r["image_path"] = c.ImagePath
	}
	return r
}

func imageResponse(img *librarymodel.Image) map[string]any {
	resp := map[string]any{
		"id":         img.ID,
		"type":       img.Type,
		"path":       img.Path,
		"is_primary": img.IsPrimary,
		"is_locked":  img.IsLocked,
	}
	if img.Width > 0 {
		resp["width"] = img.Width
		resp["height"] = img.Height
	}
	if img.Blurhash != "" {
		resp["blurhash"] = img.Blurhash
	}
	if img.DominantColor != "" {
		resp["dominant_color"] = img.DominantColor
	}
	if img.DominantColorMuted != "" {
		resp["dominant_color_muted"] = img.DominantColorMuted
	}
	return resp
}

// paletteResponse renderea los colores dominante + muted pre-
// computados en la wire shape que el frontend espera: `{ vibrant,
// muted }`. Cualquier field puede estar ausente (extracción no pudo
// classify un swatch en ese rol); el consumer trata absence igual
// que palette missing.
func paletteResponse(vibrant, muted string) map[string]any {
	resp := map[string]any{}
	if vibrant != "" {
		resp["vibrant"] = vibrant
	}
	if muted != "" {
		resp["muted"] = muted
	}
	return resp
}

// attachPosterPlaceholder folda los fields baratos de loading
// placeholder para la image poster en una listing entry. PosterCard
// renderea el color sólido como background mientras la `<img>` real
// decodifica, así las cards no pop de gris a image. Callers pasan el
// PrimaryImageRef primary-typed que pulled de images.GetPrimaryURLs.
func attachPosterPlaceholder(entry map[string]any, ref librarymodel.PrimaryImageRef) {
	if ref.DominantColor != "" {
		entry["poster_color"] = ref.DominantColor
	}
	if ref.DominantColorMuted != "" {
		entry["poster_color_muted"] = ref.DominantColorMuted
	}
	if ref.Blurhash != "" {
		entry["poster_blurhash"] = ref.Blurhash
	}
}
