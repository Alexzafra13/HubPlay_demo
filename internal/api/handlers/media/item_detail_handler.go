package media

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/library"
	librarymodel "hubplay/internal/library/model"
)

// itemDetailFetcher es el contrato mínimo que ItemDetailHandler necesita
// del library service. 5 métodos de 25. Cierra NN para este consumer.
type itemDetailFetcher interface {
	GetItem(ctx context.Context, id string) (*librarymodel.Item, error)
	GetItemChildren(ctx context.Context, id string) ([]*librarymodel.Item, error)
	GetItemChildCounts(ctx context.Context, parentIDs []string) (map[string]int, error)
	GetItemStreams(ctx context.Context, itemID string) ([]*librarymodel.MediaStream, error)
	GetItemImages(ctx context.Context, itemID string) ([]*librarymodel.Image, error)
}

// ItemDetailHandler aísla las rutas del detalle de un item (la mayor
// responsabilidad de los sub-handlers del split de olor P):
//
//	GET /items/{id}            → Get
//	GET /items/{id}/children   → Children
type ItemDetailHandler struct {
	lib         itemDetailFetcher
	images      handlers.ImageRepository
	metadata    handlers.MetadataRepository
	userData    handlers.UserDataRepository
	users       userProfileLookup
	chapters    handlers.ChapterRepository
	segments    handlers.EpisodeSegmentRepository
	externalIDs handlers.ExternalIDsRepository
	people      handlers.PeopleRepoForItems
	collections handlers.CollectionRepoForItems
	// identifier es READ-ONLY aquí — sólo usamos IsMetadataLocked
	// en buildItemDetail para colorear el candado en el kebab del
	// detalle. La gestión real (Identify*, UpdateItemMetadata, etc.)
	// vive en MetadataHandler. La referencia es el mismo puntero
	// inyectado en el constructor de ambos handlers.
	identifier MetadataIdentifier
	access     handlers.LibraryAccessService
	logger     *slog.Logger
}

func newItemDetailHandler(
	lib itemDetailFetcher,
	images handlers.ImageRepository,
	metadata handlers.MetadataRepository,
	userData handlers.UserDataRepository,
	users userProfileLookup,
	chapters handlers.ChapterRepository,
	segments handlers.EpisodeSegmentRepository,
	externalIDs handlers.ExternalIDsRepository,
	people handlers.PeopleRepoForItems,
	collections handlers.CollectionRepoForItems,
	identifier MetadataIdentifier,
	access handlers.LibraryAccessService,
	logger *slog.Logger,
) *ItemDetailHandler {
	return &ItemDetailHandler{
		lib:         lib,
		images:      images,
		metadata:    metadata,
		userData:    userData,
		users:       users,
		chapters:    chapters,
		segments:    segments,
		externalIDs: externalIDs,
		people:      people,
		collections: collections,
		identifier:  identifier,
		access:      access,
		logger:      logger,
	}
}

// authorizeItem resolves the item's library and enforces the per-library
// ACL. On denial it writes a 404 (enumeration-safe) and returns false.
// nil access (test rigs) passes through; the lookup is skipped entirely
// in that case so the detail path pays no extra fetch when the ACL isn't
// wired.
func (h *ItemDetailHandler) authorizeItem(w http.ResponseWriter, r *http.Request, id string) bool {
	if h.access == nil {
		return true
	}
	item, err := h.lib.GetItem(r.Context(), id)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return false
	}
	if itemLibraryAuthorized(r, h.access, h.logger, item.LibraryID) {
		return true
	}
	handlers.RespondError(w, r, http.StatusNotFound, "NOT_FOUND", "item not found")
	return false
}

// callerCapRating mirrors the LibraryHandler helper. nil-safe: cuando
// el handler se cablea sin handlers.UserService (test rigs que no se preocupan
// del rating gate), el cap colapsa a "" y AllowedRating devuelve true
// para todo.
func (h *ItemDetailHandler) callerCapRating(ctx context.Context) string {
	if h.users == nil {
		return ""
	}
	claims := auth.GetClaims(ctx)
	if claims == nil {
		return ""
	}
	u, err := h.users.GetByID(ctx, claims.UserID)
	if err != nil || u == nil {
		return ""
	}
	return u.MaxContentRating
}

// Get renderea el item detail JSON que consume la página detail de
// React. El ensamblado del body se delega a buildItemDetail para que
// la orquestación across siete repositorios sea una función self-
// contained con firma única (ctx, id, userID) → map. El handler
// mantiene sólo las preocupaciones de HTTP: parsing de param,
// extracción de claims, status code, envelope.
func (h *ItemDetailHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := handlers.RequireParam(w, r, "id")
	if id == "" {
		return
	}
	if !h.authorizeItem(w, r, id) {
		return
	}
	userID := ""
	if claims := auth.GetClaims(r.Context()); claims != nil {
		userID = claims.UserID
	}
	detail, err := h.buildItemDetail(r.Context(), id, userID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	// Gate per-profile de content-rating: cuando el caller tiene un
	// cap set y el item lo excede, devuelve 404 (NO 403) — misma
	// shape que si el item no existiera. Un 403 leakearía la
	// existencia de contenido bloqueado a un profile kid y le
	// dejaría sondear qué tiene su padre en la library.
	if cap := h.callerCapRating(r.Context()); cap != "" {
		rating, _ := detail["content_rating"].(string)
		if !library.AllowedRating(rating, cap) {
			handlers.RespondError(w, r, http.StatusNotFound, "NOT_FOUND", "item not found")
			return
		}
	}
	handlers.RespondData(w, http.StatusOK, detail)
}

// buildItemDetail orquesta el seven-repo fan-out para la respuesta
// del item detail. Inputs ctx/id/userID puros y un único return —
// sin http.ResponseWriter, sin chi, sin claims plumbing. Fácil de
// testear en aislado y de migrar a un DTO tipado + service method
// en un follow-up sin tocar la firma del handler.
//
// userID es "" para requests anónimos; los bloques per-user
// (user_data, episode_progress) se skipean en ese caso. Sub-fetch
// errors se loggean y skipean — la response degrada gracefully en
// lugar de 500ear porque (e.g.) la tabla chapters está unreachable.
func (h *ItemDetailHandler) buildItemDetail(ctx context.Context, id, userID string) (map[string]any, error) {
	item, err := h.lib.GetItem(ctx, id)
	if err != nil {
		return nil, err
	}
	resp := itemDetailResponse(item)

	if h.userData != nil && userID != "" {
		ud, err := h.userData.Get(ctx, userID, id)
		if err != nil {
			h.logger.Warn("get user data", "item_id", id, "error", err)
		} else if ud != nil {
			resp["user_data"] = userDataResponse(ud, item.DurationTicks)
		}
	}

	if streams, err := h.lib.GetItemStreams(ctx, id); err == nil && len(streams) > 0 {
		streamData := make([]map[string]any, len(streams))
		for i, s := range streams {
			streamData[i] = streamResponse(s)
		}
		resp["media_streams"] = streamData
	}

	h.attachImages(ctx, resp, id)
	h.attachMetadata(ctx, resp, id)
	h.attachChapters(ctx, resp, id)
	h.attachSegments(ctx, resp, id)
	h.attachPeople(ctx, resp, id)
	h.attachExternalIDs(ctx, resp, id)

	// metadata_locked: el frontend pinta un candado en el detalle y
	// usa el flag para alternar entre "Bloquear" y "Desbloquear" en
	// el kebab. nil-safe: si el identifier no está cableado el campo
	// se omite y el kebab oculta la entrada del toggle.
	if h.identifier != nil {
		if locked, err := h.identifier.IsMetadataLocked(ctx, id); err == nil {
			resp["metadata_locked"] = locked
		}
	}

	// Episodio y season pages ambas necesitan un "what show is this?"
	// anchor y el backdrop del show como fallback hero image.
	// Episodios trepan episode → season → series; seasons trepan
	// season → series.
	switch item.Type {
	case "episode":
		if item.ParentID != "" {
			h.attachSeriesContext(ctx, resp, item.ParentID)
		}
	case "season":
		if item.ParentID != "" {
			h.attachSeriesContextFromSeries(ctx, resp, item.ParentID)
		}
	case "series":
		if h.userData != nil && userID != "" {
			if total, watched, err := h.userData.SeriesEpisodeProgress(ctx, userID, item.ID); err == nil && total > 0 {
				resp["episode_progress"] = map[string]any{
					"total":   total,
					"watched": watched,
				}
			}
		}
	}

	return resp, nil
}

// attachImages escribe las imágenes del item + los URLs surfaceados
// poster / backdrop / logo y la palette de colores dominantes en
// resp. Backdrop palette gana; poster es el fallback para que items
// poster-only sigan teniendo gradient hero colorido.
func (h *ItemDetailHandler) attachImages(ctx context.Context, resp map[string]any, id string) {
	images, _ := h.lib.GetItemImages(ctx, id)
	if len(images) == 0 {
		return
	}
	imgData := make([]map[string]any, len(images))
	for i, img := range images {
		imgData[i] = imageResponse(img)
	}
	resp["images"] = imgData

	var (
		backdropColors map[string]any
		posterColors   map[string]any
	)
	for _, img := range images {
		if !img.IsPrimary {
			continue
		}
		switch img.Type {
		case "primary":
			resp["poster_url"] = img.Path
			if img.DominantColor != "" || img.DominantColorMuted != "" {
				posterColors = paletteResponse(img.DominantColor, img.DominantColorMuted)
			}
		case "backdrop":
			resp["backdrop_url"] = img.Path
			if img.DominantColor != "" || img.DominantColorMuted != "" {
				backdropColors = paletteResponse(img.DominantColor, img.DominantColorMuted)
			}
		case "logo":
			resp["logo_url"] = img.Path
		case "thumb":
			// "Miniatura" en la UI del image manager. 16:9 still que
			// providers (TMDb / Fanart) suministran junto al pair
			// poster/backdrop — purpose-built para listing cards
			// landscape. La rail Continue Watching lo usa para
			// películas para que el cartel-thumb reconocible se vea
			// con la misma forma que los screencaps de episodios.
			resp["thumb_url"] = img.Path
		}
	}
	if backdropColors != nil {
		resp["backdrop_colors"] = backdropColors
	} else if posterColors != nil {
		resp["backdrop_colors"] = posterColors
	}
}

// attachMetadata escribe overview / tagline / genres / studio /
// trailer cuando el item tiene una fila metadata. El trailer son dos
// fields paired juntos — ambos deben estar presentes para una URL
// embed válida.
func (h *ItemDetailHandler) attachMetadata(ctx context.Context, resp map[string]any, id string) {
	if h.metadata == nil {
		return
	}
	meta, err := h.metadata.GetByItemID(ctx, id)
	if err != nil || meta == nil {
		return
	}
	if meta.Overview != "" {
		resp["overview"] = meta.Overview
	}
	if meta.Tagline != "" {
		resp["tagline"] = meta.Tagline
	}
	if meta.GenresJSON != "" {
		var genres []string
		if err := json.Unmarshal([]byte(meta.GenresJSON), &genres); err == nil && len(genres) > 0 {
			resp["genres"] = genres
		}
	}
	if meta.Studio != "" {
		resp["studio"] = meta.Studio
		// Slug para el click-through a /studios/{slug}. Derivado del
		// mismo recipe Slugify que el scanner usa para insertar la
		// fila canónica, así el link siempre es válido para studios
		// que produjeron cualquier item del catálogo (la tabla
		// studios está keyed sobre este slug). Studio vacío → sin
		// slug, sin chip link en el frontend.
		if slug := db.Slugify(meta.Studio); slug != "" {
			resp["studio_slug"] = slug
		}
	}
	// Logo del studio (TMDb production-company brand mark) es
	// opcional — studios más viejos sin logo on file producen
	// strings vacíos y el frontend cae al texto `studio`. Persistido
	// como URL absoluta por el scanner así sólo pasamos through.
	if meta.StudioLogoURL != "" {
		resp["studio_logo_url"] = meta.StudioLogoURL
	}
	if meta.TrailerKey != "" && meta.TrailerSite != "" {
		resp["trailer"] = map[string]any{
			"key":  meta.TrailerKey,
			"site": meta.TrailerSite,
		}
	}
	// Link movie-saga (Jellyfin-style "Movie Collection"). Sólo el
	// id + name van por el wire; el frontend renderea "Part of: X"
	// y linkea a /collections/{id}, que fetchea el hero completo
	// (poster, backdrop, member list) por sí mismo. Skipea el
	// lookup entero cuando no hay dep collections cableada o la
	// fila metadata no tiene link.
	if h.collections != nil && meta.CollectionID != "" {
		if col, cErr := h.collections.GetByID(ctx, meta.CollectionID); cErr == nil && col != nil {
			resp["collection"] = map[string]any{
				"id":   col.ID,
				"name": col.Name,
			}
		}
	}
}

// attachChapters escribe la lista per-item de chapters cuando presente.
// Un fichero chapter-less yields no field at all; clientes tratan
// absence y empty array idénticos.
func (h *ItemDetailHandler) attachChapters(ctx context.Context, resp map[string]any, id string) {
	if h.chapters == nil {
		return
	}
	ch, err := h.chapters.ListByItem(ctx, id)
	if err != nil {
		h.logger.Warn("list chapters", "item_id", id, "error", err)
		return
	}
	if len(ch) == 0 {
		return
	}
	out := make([]map[string]any, len(ch))
	for i, c := range ch {
		out[i] = chapterResponse(c)
	}
	resp["chapters"] = out
}

// attachSegments escribe markers intro / outro / recap cuando el
// segment detector ha corrido para este item. El repo puede devolver
// múltiples filas del mismo kind (sources distintos — chapter y
// fingerprint pueden ambos disparar); colapsamos a una fila por kind
// pickeando el highest-confidence source. Ordering estable: recap →
// intro → outro, así el player puede iterar en orden de playback.
//
// Mismo contrato nil/empty que attachChapters: no field at all cuando
// nada aplica. Devuelve ticks-as-seconds (float) para el frontend,
// que habla `video.currentTime` nativo.
func (h *ItemDetailHandler) attachSegments(ctx context.Context, resp map[string]any, id string) {
	if h.segments == nil {
		return
	}
	rows, err := h.segments.ListByItem(ctx, id)
	if err != nil {
		h.logger.Warn("list segments", "item_id", id, "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	bestByKind := make(map[librarymodel.EpisodeSegmentKind]librarymodel.EpisodeSegment, 3)
	for _, r := range rows {
		prev, seen := bestByKind[r.Kind]
		if !seen || r.Confidence > prev.Confidence {
			bestByKind[r.Kind] = r
		}
	}
	order := []librarymodel.EpisodeSegmentKind{
		librarymodel.EpisodeSegmentRecap,
		librarymodel.EpisodeSegmentIntro,
		librarymodel.EpisodeSegmentOutro,
	}
	out := make([]map[string]any, 0, len(bestByKind))
	for _, kind := range order {
		seg, ok := bestByKind[kind]
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"kind":          string(seg.Kind),
			"source":        string(seg.Source),
			"start_seconds": float64(seg.StartTicks) / 10_000_000,
			"end_seconds":   float64(seg.EndTicks) / 10_000_000,
			"confidence":    seg.Confidence,
		})
	}
	resp["segments"] = out
}

// attachPeople escribe cast / crew cuando presente. image_url
// apunta al per-person thumb endpoint cuando una foto de profile se
// downloadeó; null en otro caso para que el cliente renderee un
// placeholder de letra inicial.
func (h *ItemDetailHandler) attachPeople(ctx context.Context, resp map[string]any, id string) {
	if h.people == nil {
		return
	}
	credits, err := h.people.ListByItem(ctx, id)
	if err != nil {
		h.logger.Warn("list item people", "item_id", id, "error", err)
		return
	}
	if len(credits) == 0 {
		return
	}
	peopleData := make([]map[string]any, len(credits))
	for i, c := range credits {
		entry := map[string]any{
			"id":         c.PersonID,
			"name":       c.Name,
			"role":       c.Role,
			"sort_order": c.SortOrder,
		}
		if c.CharacterName != "" {
			entry["character"] = c.CharacterName
		}
		if c.ThumbPath != "" {
			entry["image_url"] = "/api/v1/people/" + c.PersonID + "/thumb"
		}
		peopleData[i] = entry
	}
	resp["people"] = peopleData
}

// attachExternalIDs escribe un flat map provider→external-id (IMDb,
// TMDb, TVDB, ...) para que el cliente pueda construir "Open in X"
// links sin saber la lista de providers en build time.
func (h *ItemDetailHandler) attachExternalIDs(ctx context.Context, resp map[string]any, id string) {
	if h.externalIDs == nil {
		return
	}
	extIDs, err := h.externalIDs.ListByItem(ctx, id)
	if err != nil || len(extIDs) == 0 {
		return
	}
	ids := make(map[string]string, len(extIDs))
	for _, e := range extIDs {
		ids[e.Provider] = e.ExternalID
	}
	resp["external_ids"] = ids
}

// attachSeriesContext walks episode → season → series y folds los
// breadcrumb fields del show y (cuando el episode no tiene) los
// image URLs en la respuesta del detail. Best-effort: cualquier DB
// error along the way deja resp untouched — la página sigue
// renderizando, sólo con la bare episode data que el caller ya tenía.
func (h *ItemDetailHandler) attachSeriesContext(ctx context.Context, resp map[string]any, seasonID string) {
	season, err := h.lib.GetItem(ctx, seasonID)
	if err != nil || season == nil || season.ParentID == "" {
		return
	}
	h.attachSeriesContextFromSeries(ctx, resp, season.ParentID)
}

// attachSeriesContextFromSeries es la mitad interna de
// attachSeriesContext: dado el series id directamente, populate los
// breadcrumb + image fallbacks. Lifted out así la season-detail
// page (un hop más cerca de la series que un episode) puede reusar
// el mismo enrichment sin hacer el climb episode→season que dead-
// endearía inmediatamente.
func (h *ItemDetailHandler) attachSeriesContextFromSeries(ctx context.Context, resp map[string]any, seriesID string) {
	series, err := h.lib.GetItem(ctx, seriesID)
	if err != nil || series == nil {
		return
	}
	resp["series_id"] = series.ID
	resp["series_title"] = series.Title

	// Pull las primary images de la series para que la página
	// episode/season pueda fall back a ellas cuando su propio still
	// / poster está missing. Misma wire shape que `poster_url` /
	// `backdrop_url` / `logo_url` — el cliente trata `series_*`
	// como la "usa esta si la mía está empty" alternativa.
	// También fold el backdrop palette de la series en
	// `backdrop_colors` cuando el item actual no tiene palette
	// propio (típico para filas season: TMDb les da poster pero no
	// backdrop, así el gradient leans en la series).
	seriesImgs, err := h.lib.GetItemImages(ctx, series.ID)
	if err != nil {
		return
	}
	_, hasBackdropColors := resp["backdrop_colors"]
	for _, img := range seriesImgs {
		if !img.IsPrimary {
			continue
		}
		switch img.Type {
		case "primary":
			resp["series_poster_url"] = img.Path
		case "backdrop":
			resp["series_backdrop_url"] = img.Path
			if !hasBackdropColors && (img.DominantColor != "" || img.DominantColorMuted != "") {
				resp["backdrop_colors"] = paletteResponse(img.DominantColor, img.DominantColorMuted)
			}
		case "logo":
			resp["series_logo_url"] = img.Path
		}
	}

	// Inherit genres para episodes/seasons que no tienen su propia
	// fila metadata — genres están guardados en la series, pero el
	// hero los necesita para la meta line. Sólo heredamos cuando el
	// lookup item-level produjo nada.
	if _, hasGenres := resp["genres"]; !hasGenres && h.metadata != nil {
		if meta, err := h.metadata.GetByItemID(ctx, series.ID); err == nil && meta != nil && meta.GenresJSON != "" {
			var genres []string
			if err := json.Unmarshal([]byte(meta.GenresJSON), &genres); err == nil && len(genres) > 0 {
				resp["genres"] = genres
			}
		}
	}
}

// Children renderea la lista hija de un item (episodios de una
// series/season, etc.). Hace 3 batched lookups para enriquecer la
// respuesta sin N+1: primary URLs de imágenes (backdrop + poster
// para cards), episode counts en filas season, overview metadata
// para hover/expanded en SeasonGrid.
func (h *ItemDetailHandler) Children(w http.ResponseWriter, r *http.Request) {
	id := handlers.RequireParam(w, r, "id")
	if id == "" {
		return
	}
	if !h.authorizeItem(w, r, id) {
		return
	}
	children, err := h.lib.GetItemChildren(r.Context(), id)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}

	// Nota: un step previo `DedupeSeasonsByChildCount` read-time
	// vivía aquí para installs que aún tenían legacy duplicate
	// season rows. La migración 018 añadió partial UNIQUE indexes
	// sobre (parent_id, season_number) así los duplicates son ya
	// estructuralmente imposibles — el dedupe runtime era dead
	// defence y fue eliminado. Si surge un constraint failure, ese
	// es el outcome correcto: arreglar el scanner regression en
	// lugar de papered over aquí.

	data := make([]map[string]any, len(children))
	for i, item := range children {
		data[i] = handlers.ItemSummaryResponse(item)
	}

	// Episode-list previews: las cards renderizadas sobre cada
	// episode usan `backdrop_url` (el still per-episode) como
	// thumbnail y caen a `poster_url`. La summary response queda
	// image-free intencionalmente, así fold-in BOTH primaries via
	// un batched query — evita lookups N+1 cuando una season
	// tiene 22 episodes.
	if h.images != nil && len(children) > 0 {
		itemIDs := make([]string, len(children))
		for i, item := range children {
			itemIDs[i] = item.ID
		}
		if imageURLs, err := h.images.GetPrimaryURLs(r.Context(), itemIDs); err == nil {
			for i, item := range children {
				urls, ok := imageURLs[item.ID]
				if !ok {
					continue
				}
				if backdrop, ok := urls["backdrop"]; ok {
					data[i]["backdrop_url"] = backdrop.Path
				}
				if poster, ok := urls["primary"]; ok {
					data[i]["poster_url"] = poster.Path
					attachPosterPlaceholder(data[i], poster)
				}
			}
		}
	}

	// Episode counts en season cards. La SeasonGrid renderea
	// "9 eps" junto al título; computar aquí evita un N+1 del
	// frontend prefetcheando los children de cada season
	// puramente para un count. Skipeado cuando no hay seasons
	// presentes así movies/episodes no pagan el extra query.
	var seasonIDs []string
	for _, item := range children {
		if item.Type == "season" {
			seasonIDs = append(seasonIDs, item.ID)
		}
	}
	if len(seasonIDs) > 0 {
		if counts, err := h.lib.GetItemChildCounts(r.Context(), seasonIDs); err == nil {
			for i, item := range children {
				if item.Type != "season" {
					continue
				}
				if n, ok := counts[item.ID]; ok {
					data[i]["episode_count"] = n
				}
			}
		}
	}

	// Per-item metadata (overview etc.) para season cards. Mismo
	// batch pattern que el search handler usa; folds in `overview`
	// para que el hover/expanded state de SeasonGrid pueda
	// previewearlo sin pegarle al endpoint per-item detail.
	if h.metadata != nil && len(children) > 0 {
		itemIDs := make([]string, len(children))
		for i, item := range children {
			itemIDs[i] = item.ID
		}
		if metaByID, err := h.metadata.GetMetadataBatch(r.Context(), itemIDs); err == nil {
			for i, item := range children {
				meta, ok := metaByID[item.ID]
				if !ok || meta == nil {
					continue
				}
				if meta.Overview != "" {
					data[i]["overview"] = meta.Overview
				}
			}
		}
	}

	handlers.RespondData(w, http.StatusOK, data)
}
