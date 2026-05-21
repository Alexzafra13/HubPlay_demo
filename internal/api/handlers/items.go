package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/library"

	"github.com/go-chi/chi/v5"
)

type ItemHandler struct {
	lib         LibraryService
	images      ImageRepository
	metadata    MetadataRepository
	userData    UserDataRepository
	// users resolves the caller's max_content_rating for the
	// per-item rating gate on Get. Optional — nil disables the gate
	// (admin / unauthenticated context, same fail-open default the
	// browse / latest paths use).
	users       UserService
	chapters    ChapterRepository
	// segments holds intro / outro / recap markers. Optional — nil
	// disables the segments field on item detail; clients treat
	// absence and an empty array identically.
	segments    EpisodeSegmentRepository
	externalIDs ExternalIDsRepository
	people      PeopleRepoForItems
	// collections powers the "Part of: X" affordance on a movie's
	// detail page. nil-safe — handler skips the field entirely when
	// the dep wasn't wired, matching the legacy shape.
	collections CollectionRepoForItems
	// providers powers the "more like this" rail by calling the
	// metadata provider's recommendations endpoint (TMDb today). nil
	// disables the feature — the endpoint returns 503 in that case.
	providers ProviderManager
	// identifier powers the admin-only "Identify" rematch flow. Wraps
	// the scanner (which already knows how to apply a TMDb metadata
	// result end-to-end including images). nil disables the endpoints
	// with a 503 — the rest of the handler keeps working.
	identifier MetadataIdentifier
	// Sub-handlers extraídos para cerrar el olor P del audit
	// 2026-05-14 (ItemHandler god-handler, 1186 LoC, 13 deps, 4
	// responsabilidades). Embedding por puntero → los métodos se
	// promueven y los call sites externos (router, tests) siguen
	// llamando `itemHandler.Method(...)` sin cambios.
	//
	// Fase 1: TrickplayHandler. Fase 2: Search + Recommendations.
	// Pendiente: Detail (Get/Children/attach×8/buildItemDetail) +
	// Metadata (Identify*/UpdateItemMetadata/SetMetadataLock/Refresh).
	*TrickplayHandler
	*SearchHandler
	*RecommendationsHandler
	audit  AuditEmitter
	logger *slog.Logger
}

func NewItemHandler(lib LibraryService, images ImageRepository, metadata MetadataRepository, userData UserDataRepository, users UserService, chapters ChapterRepository, segments EpisodeSegmentRepository, externalIDs ExternalIDsRepository, people PeopleRepoForItems, collections CollectionRepoForItems, providers ProviderManager, identifier MetadataIdentifier, trickplayDir string, audit AuditEmitter, logger *slog.Logger) *ItemHandler {
	return &ItemHandler{
		lib: lib, images: images, metadata: metadata, userData: userData,
		users:    users,
		chapters: chapters, segments: segments, externalIDs: externalIDs, people: people,
		collections: collections,
		providers:   providers,
		identifier:  identifier,
		// Sub-handlers con sus deps específicas. Cada constructor toma
		// el subconjunto que su responsabilidad realmente usa, no las
		// 13 que tomaba el ItemHandler monolítico.
		TrickplayHandler:       newTrickplayHandler(lib, trickplayDir, logger),
		SearchHandler:          newSearchHandler(lib, images, userData, users, logger),
		RecommendationsHandler: newRecommendationsHandler(lib, externalIDs, providers, logger),
		audit:                  audit,
		logger:                 logger,
	}
}

func (h *ItemHandler) auditEmit() AuditEmitter {
	if h.audit != nil {
		return h.audit
	}
	return noopAudit{}
}

// callerCapRating mirrors the LibraryHandler helper. nil-safe: when
// the handler is wired without UserService (e.g. test rigs that don't
// care about rating gates), the cap collapses to "" and AllowedRating
// returns true for everything.
func (h *ItemHandler) callerCapRating(ctx context.Context) string {
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

// Get renders the item detail JSON used by the React detail page.
// Body assembly is delegated to buildItemDetail so the orchestration
// across seven repositories is one self-contained function with a
// single signature (ctx, id, userID) → map. The handler keeps only
// the http-level concerns: param parsing, auth claim extraction,
// status code, envelope.
func (h *ItemHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := ""
	if claims := auth.GetClaims(r.Context()); claims != nil {
		userID = claims.UserID
	}
	detail, err := h.buildItemDetail(r.Context(), id, userID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	// Per-profile content-rating gate: when the caller has a cap
	// set and the item exceeds it, return 404 (NOT 403) — same shape
	// as if the item didn't exist. A 403 would leak the existence of
	// blocked content to a kid profile and let them probe what their
	// parent has in the library.
	if cap := h.callerCapRating(r.Context()); cap != "" {
		rating, _ := detail["content_rating"].(string)
		if !library.AllowedRating(rating, cap) {
			respondError(w, r, http.StatusNotFound, "NOT_FOUND", "item not found")
			return
		}
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": detail})
}

// buildItemDetail orchestrates the seven-repo fan-out for the item
// detail response. Pure ctx/id/userID inputs and a single return —
// no http.ResponseWriter, no chi, no claims plumbing. Easy to test
// in isolation and to migrate to a typed DTO + service method in a
// follow-up without touching the handler signature.
//
// userID is "" for anonymous requests; the per-user blocks (user_data,
// episode_progress) are skipped in that case. Sub-fetch errors are
// logged and skipped — the detail response degrades gracefully rather
// than 500ing because (e.g.) the chapters table is unreachable.
func (h *ItemHandler) buildItemDetail(ctx context.Context, id, userID string) (map[string]any, error) {
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

	// Episode and season pages both need a "what show is this?" anchor
	// and the show's backdrop as a fallback hero image. Episodes climb
	// episode → season → series; seasons climb season → series.
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

// attachImages writes the items' images plus surfaced poster /
// backdrop / logo URLs and the dominant-colour palette into resp.
// Backdrop palette wins; poster is the fallback so poster-only
// items still drive a colourful hero gradient.
func (h *ItemHandler) attachImages(ctx context.Context, resp map[string]any, id string) {
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
			// "Miniatura" in the image-manager UI. 16:9 still that
			// providers (TMDb / Fanart) supply alongside the
			// poster/backdrop pair — purpose-built for landscape
			// listing cards. The Continue Watching rail uses it for
			// movies so the recognisable cartel-thumb shows up at
			// the same shape as episode screencaps.
			resp["thumb_url"] = img.Path
		}
	}
	if backdropColors != nil {
		resp["backdrop_colors"] = backdropColors
	} else if posterColors != nil {
		resp["backdrop_colors"] = posterColors
	}
}

// attachMetadata writes overview / tagline / genres / studio / trailer
// when the item has a metadata row. Trailer is two fields paired
// together — both must be present for a valid embed URL.
func (h *ItemHandler) attachMetadata(ctx context.Context, resp map[string]any, id string) {
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
		// Slug for the click-through to /studios/{slug}. Derived from
		// the same Slugify recipe the scanner uses to insert the
		// canonical row, so the link is always valid for studios that
		// produced any item in the catalogue (the studios table itself
		// is keyed on this slug). Empty studio → no slug, no chip
		// link on the frontend.
		if slug := db.Slugify(meta.Studio); slug != "" {
			resp["studio_slug"] = slug
		}
	}
	// Studio logo (TMDb production-company brand mark) is optional —
	// older studios with no logo on file produce empty strings here
	// and the frontend falls back to the `studio` text. Persisted as
	// an absolute URL by the scanner so we just pass it through.
	if meta.StudioLogoURL != "" {
		resp["studio_logo_url"] = meta.StudioLogoURL
	}
	if meta.TrailerKey != "" && meta.TrailerSite != "" {
		resp["trailer"] = map[string]any{
			"key":  meta.TrailerKey,
			"site": meta.TrailerSite,
		}
	}
	// Movie-saga link (Jellyfin-style "Movie Collection"). Only the
	// id + name go on the wire; the frontend renders "Part of: X"
	// and links to /collections/{id}, which fetches the full hero
	// (poster, backdrop, member list) on its own. Skip the lookup
	// entirely when no collections dep is wired or the metadata
	// row has no link.
	if h.collections != nil && meta.CollectionID != "" {
		if col, cErr := h.collections.GetByID(ctx, meta.CollectionID); cErr == nil && col != nil {
			resp["collection"] = map[string]any{
				"id":   col.ID,
				"name": col.Name,
			}
		}
	}
}

// attachChapters writes the per-item chapter list when present.
// A chapter-less file yields no field at all; clients treat absence
// and empty array identically.
func (h *ItemHandler) attachChapters(ctx context.Context, resp map[string]any, id string) {
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

// attachSegments writes intro / outro / recap markers when the
// segment detector has run for this item. The repo can return
// multiple rows of the same kind (different sources — chapter and
// fingerprint may both fire); we collapse to one row per kind by
// picking the highest-confidence source. Stable ordering: recap →
// intro → outro, so the player can iterate in playback order.
//
// Same nil/empty contract as attachChapters: no field at all when
// nothing applies. Returns ticks-as-seconds (float) for the
// frontend, which speaks `video.currentTime` natively.
func (h *ItemHandler) attachSegments(ctx context.Context, resp map[string]any, id string) {
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

// attachPeople writes cast / crew when present. image_url points at
// the per-person thumb endpoint when a profile photo was downloaded;
// null otherwise so the client renders an initial-letter placeholder.
func (h *ItemHandler) attachPeople(ctx context.Context, resp map[string]any, id string) {
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

// attachExternalIDs writes a flat provider→external-id map (IMDb,
// TMDb, TVDB, ...) so the client can build "Open in X" links without
// knowing the provider list at build time.
func (h *ItemHandler) attachExternalIDs(ctx context.Context, resp map[string]any, id string) {
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

// attachSeriesContext walks episode → season → series and folds the show's
// breadcrumb fields and (when the episode has none) image URLs into the
// detail response. Best-effort: any DB error along the way leaves resp
// untouched — the page still renders, just with the bare episode data
// the caller already had.
func (h *ItemHandler) attachSeriesContext(ctx context.Context, resp map[string]any, seasonID string) {
	season, err := h.lib.GetItem(ctx, seasonID)
	if err != nil || season == nil || season.ParentID == "" {
		return
	}
	h.attachSeriesContextFromSeries(ctx, resp, season.ParentID)
}

// attachSeriesContextFromSeries is the inner half of attachSeriesContext:
// given the series id directly, populate the breadcrumb + image fallbacks.
// Lifted out so the season-detail page (one hop closer to the series than
// an episode) can reuse the same enrichment without doing the
// episode→season climb that would dead-end immediately.
func (h *ItemHandler) attachSeriesContextFromSeries(ctx context.Context, resp map[string]any, seriesID string) {
	series, err := h.lib.GetItem(ctx, seriesID)
	if err != nil || series == nil {
		return
	}
	resp["series_id"] = series.ID
	resp["series_title"] = series.Title

	// Pull the series' primary images so the episode/season page can
	// fall back to them when its own still / poster is missing. Same
	// wire shape as `poster_url` / `backdrop_url` / `logo_url` — the
	// client treats `series_*` as the "use this if my own is empty"
	// alternative. Also fold the series' backdrop palette into
	// `backdrop_colors` when the current item has no palette of its
	// own (typical for season rows: TMDb gives them a poster but no
	// backdrop, so the gradient leans on the series).
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

	// Inherit genres for episodes/seasons that lack their own metadata
	// row — genres are stored on the series, but the hero needs them
	// for the meta line. Only inherit when the item-level lookup
	// produced nothing.
	if _, hasGenres := resp["genres"]; !hasGenres && h.metadata != nil {
		if meta, err := h.metadata.GetByItemID(ctx, series.ID); err == nil && meta != nil && meta.GenresJSON != "" {
			var genres []string
			if err := json.Unmarshal([]byte(meta.GenresJSON), &genres); err == nil && len(genres) > 0 {
				resp["genres"] = genres
			}
		}
	}
}

// (Recommendations vive en item_recommendations_handler.go. El
// embedding del *RecommendationsHandler en ItemHandler promueve el
// método para que router y tests sigan funcionando sin cambios.)

// (Trickplay vive en item_trickplay_handler.go. El embedding del
// *TrickplayHandler en ItemHandler promueve TrickplayManifest,
// TrickplaySprite y WaitTrickplayInflight para que el router y los
// tests sigan llamando `itemHandler.TrickplayManifest` sin cambios.)

func (h *ItemHandler) Children(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	children, err := h.lib.GetItemChildren(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	// Note: a previous read-time `DedupeSeasonsByChildCount` step lived
	// here for installs that still had legacy duplicate season rows.
	// Migration 018 added partial UNIQUE indexes on
	// (parent_id, season_number) so duplicates are now structurally
	// impossible — the runtime dedupe was dead defence and was removed.
	// If a constraint failure ever surfaces, that's the right outcome:
	// fix the scanner regression rather than paper over it here.

	data := make([]map[string]any, len(children))
	for i, item := range children {
		data[i] = itemSummaryResponse(item)
	}

	// Episode-list previews: the cards rendered above each episode use
	// `backdrop_url` (the per-episode still) as the thumbnail and fall
	// back to `poster_url`. The summary response intentionally stays
	// image-free, so fold in BOTH primaries via one batched query —
	// avoids N+1 lookups when a season has 22 episodes.
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

	// Episode counts on season cards. The SeasonGrid renders "9 eps"
	// next to the title; computing it here avoids an N+1 from the
	// frontend prefetching each season's children purely for a count.
	// Skipped when no seasons are present so movies/episodes don't
	// pay the extra query.
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

	// Per-item metadata (overview etc.) for season cards. Same batch
	// pattern the search handler uses; folds in `overview` so the
	// SeasonGrid hover/expanded state can preview it without hitting
	// the per-item detail endpoint.
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

	respondJSON(w, http.StatusOK, map[string]any{"data": data})
}

// (Search vive en item_search_handler.go — el método se promueve
// vía embedding del *SearchHandler en ItemHandler.)

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
	// `year` is omitted when zero so clients can render absence cleanly
	// (the previous shape leaked Go's int zero-value as `"year": 0`,
	// which the UI rendered literally — see the empty-episode hero).
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

// userDataResponse renders a UserData row in the canonical client shape:
//
//	{
//	  progress: { position_ticks, percentage, audio_stream_index, subtitle_stream_index },
//	  is_favorite, played, play_count, last_played_at,
//	}
//
// `percentage` is computed server-side so every client (web, future native)
// shows the same value, and is clamped to [0, 100] so badly-clamped position
// data (e.g. resume past EOF after a re-encode) can't render >100% UI.
func userDataResponse(ud *db.UserData, durationTicks int64) map[string]any {
	if ud == nil {
		return nil
	}
	var pct float64
	if durationTicks > 0 {
		pct = float64(ud.PositionTicks) / float64(durationTicks) * 100
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
	}
	resp := map[string]any{
		"progress": map[string]any{
			"position_ticks":        ud.PositionTicks,
			"percentage":            pct,
			"audio_stream_index":    ud.AudioStreamIndex,
			"subtitle_stream_index": ud.SubtitleStreamIndex,
		},
		"is_favorite":    ud.IsFavorite,
		"played":         ud.Completed,
		"play_count":     ud.PlayCount,
		"last_played_at": ud.LastPlayedAt,
	}
	return resp
}

// chapterResponse is the wire shape for one timeline marker. `title`
// is always emitted (empty string when unknown) so clients can render
// either "Chapter 3" placeholder or the real name without a presence
// check; `image_path` is omitted when absent — Plex-style chapter
// thumbnails (BIF) aren't generated yet.
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

// paletteResponse renders the pre-computed dominant + muted colours in
// the wire shape the frontend expects: `{ vibrant, muted }`. Either
// field may be absent (extraction couldn't classify a swatch in that
// role); the consumer treats absence the same as missing palette.
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

// attachPosterPlaceholder folds the cheap loading-placeholder fields
// for the poster image into a listing entry. PosterCard renders the
// solid colour as background while the real <img> decodes, so cards
// don't pop from grey to image. Callers pass the primary-typed
// PrimaryImageRef they pulled from images.GetPrimaryURLs.
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
