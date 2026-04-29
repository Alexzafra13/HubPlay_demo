package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/imaging"

	"github.com/go-chi/chi/v5"
)

type ItemHandler struct {
	lib      LibraryService
	images   ImageRepository
	metadata MetadataRepository
	userData UserDataRepository
	chapters ChapterRepository
	// trickplayDir is the root for generated trickplay sprites
	// (`<dir>/<itemID>/sprite.png` + `manifest.json`). Empty disables
	// the feature; the endpoint returns 503 in that case.
	trickplayDir string
	// trickplayLocks serialises generation per item so a second hover
	// while the first is still running waits instead of double-spawning
	// ffmpeg. The map grows by one entry per item that's ever been
	// generated; bounded by library size, fine in practice.
	trickplayLocks sync.Map
	logger         *slog.Logger
}

func NewItemHandler(lib LibraryService, images ImageRepository, metadata MetadataRepository, userData UserDataRepository, chapters ChapterRepository, trickplayDir string, logger *slog.Logger) *ItemHandler {
	return &ItemHandler{
		lib: lib, images: images, metadata: metadata, userData: userData,
		chapters: chapters, trickplayDir: trickplayDir, logger: logger,
	}
}

func (h *ItemHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, err := h.lib.GetItem(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	resp := itemDetailResponse(item)

	// Per-user state (favorite, watched, resume position) — only when
	// authenticated. Not fatal: if it fails, log and skip rather than
	// fail the whole detail response.
	if h.userData != nil {
		if claims := auth.GetClaims(r.Context()); claims != nil {
			ud, err := h.userData.Get(r.Context(), claims.UserID, id)
			if err != nil {
				h.logger.Warn("get user data", "item_id", id, "error", err)
			} else if ud != nil {
				resp["user_data"] = userDataResponse(ud, item.DurationTicks)
			}
		}
	}

	// Include streams
	streams, _ := h.lib.GetItemStreams(r.Context(), id)
	if len(streams) > 0 {
		streamData := make([]map[string]any, len(streams))
		for i, s := range streams {
			streamData[i] = streamResponse(s)
		}
		resp["media_streams"] = streamData
	}

	// Include images and set poster_url/backdrop_url + the pre-computed
	// dominant-colour palette for the SeriesHero gradient. We surface
	// the backdrop's palette as `backdrop_colors` (with poster as a
	// fallback when there's no backdrop), so the frontend gradient
	// paints on first render with no client-side image decode.
	images, _ := h.lib.GetItemImages(r.Context(), id)
	if len(images) > 0 {
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
			}
		}
		// Backdrop wins when present; falls back to poster so
		// poster-only items (movies without a downloaded backdrop)
		// still drive a colourful gradient on the detail page.
		if backdropColors != nil {
			resp["backdrop_colors"] = backdropColors
		} else if posterColors != nil {
			resp["backdrop_colors"] = posterColors
		}
	}

	// Include metadata (overview, tagline, genres)
	if h.metadata != nil {
		meta, err := h.metadata.GetByItemID(r.Context(), id)
		if err == nil && meta != nil {
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
			}
			// Trailer is two fields paired together — both must be
			// present for a valid embed URL. Keep them out of the
			// response when either is empty so the frontend's
			// nullable check is a single key, not a tuple.
			if meta.TrailerKey != "" && meta.TrailerSite != "" {
				resp["trailer"] = map[string]any{
					"key":  meta.TrailerKey,
					"site": meta.TrailerSite,
				}
			}
		}
	}

	// Chapters drive the seek-bar tick marks and the (future) skip-
	// intro affordance. Optional: a chapter-less file (most non-Blu-ray
	// rips) returns a nil slice and the JSON omits the field — clients
	// can treat absence and empty array identically.
	if h.chapters != nil {
		ch, err := h.chapters.ListByItem(r.Context(), id)
		if err != nil {
			h.logger.Warn("list chapters", "item_id", id, "error", err)
		} else if len(ch) > 0 {
			out := make([]map[string]any, len(ch))
			for i, c := range ch {
				out[i] = chapterResponse(c)
			}
			resp["chapters"] = out
		}
	}

	// Episode and season pages both need a "what show is this?" anchor
	// and the show's backdrop as a fallback hero image. Episodes climb
	// episode → season → series; seasons climb season → series. Both
	// surface `series_title` + `series_*_url` so the renderer doesn't
	// have to know the relationship depth.
	switch item.Type {
	case "episode":
		if item.ParentID != "" {
			h.attachSeriesContext(r.Context(), resp, item.ParentID)
		}
	case "season":
		if item.ParentID != "" {
			h.attachSeriesContextFromSeries(r.Context(), resp, item.ParentID)
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
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

// TrickplayManifest serves (and lazily generates) the sprite-sheet
// manifest for an item. The manifest tells the client how to compute
// which sub-image of the sprite covers a given playback time. See
// `imaging.TrickplayManifest` for the fields' precise contract.
//
// Generation is on-demand: the first hit triggers a synchronous
// ffmpeg run (one-shot, ~5–30 s for a 2 h movie), subsequent hits
// serve the cached file. A per-item mutex prevents two concurrent
// hovers from spawning duplicate ffmpeg processes.
func (h *ItemHandler) TrickplayManifest(w http.ResponseWriter, r *http.Request) {
	if h.trickplayDir == "" {
		respondError(w, r, http.StatusServiceUnavailable, "TRICKPLAY_DISABLED",
			"trickplay generation is not configured")
		return
	}
	id := chi.URLParam(r, "id")
	itemDir, err := h.ensureTrickplay(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")
	http.ServeFile(w, r, filepath.Join(itemDir, "manifest.json"))
}

// TrickplaySprite serves the sprite PNG. Same lazy-generate-on-first-
// hit semantics as the manifest endpoint above. Browsers cache the
// PNG aggressively (the sprite is content-addressable per item — same
// runtime + same params produces byte-identical output), so the
// hover-scroll experience after the first miss is a single fetch
// per item per long-term cache window.
func (h *ItemHandler) TrickplaySprite(w http.ResponseWriter, r *http.Request) {
	if h.trickplayDir == "" {
		respondError(w, r, http.StatusServiceUnavailable, "TRICKPLAY_DISABLED",
			"trickplay generation is not configured")
		return
	}
	id := chi.URLParam(r, "id")
	itemDir, err := h.ensureTrickplay(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")
	http.ServeFile(w, r, filepath.Join(itemDir, "sprite.png"))
}

// ensureTrickplay returns the per-item directory containing
// `sprite.png` + `manifest.json`, generating them via ffmpeg on first
// call. Per-item locking prevents concurrent generation; once the
// files exist the subsequent calls are O(stat).
func (h *ItemHandler) ensureTrickplay(ctx context.Context, itemID string) (string, error) {
	itemDir := filepath.Join(h.trickplayDir, itemID)
	spritePath := filepath.Join(itemDir, "sprite.png")
	manifestPath := filepath.Join(itemDir, "manifest.json")

	// Fast path: both files already cached.
	if _, err := os.Stat(spritePath); err == nil {
		if _, err := os.Stat(manifestPath); err == nil {
			return itemDir, nil
		}
	}

	// Per-item mutex. Two concurrent first-hits collapse to one
	// ffmpeg process; the loser blocks until the winner publishes
	// the files and then returns from the fast path on retry below.
	mu, _ := h.trickplayLocks.LoadOrStore(itemID, &sync.Mutex{})
	lock := mu.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	// Re-check under the lock — the previous holder may have just
	// finished writing.
	if _, err := os.Stat(spritePath); err == nil {
		if _, err := os.Stat(manifestPath); err == nil {
			return itemDir, nil
		}
	}

	item, err := h.lib.GetItem(ctx, itemID)
	if err != nil {
		return "", err
	}
	if item.Path == "" {
		return "", errors.New("item has no playable file path")
	}

	if _, err := imaging.GenerateTrickplayWithDeadline(ctx, item.Path, itemDir, imaging.TrickplayParams{}, 0); err != nil {
		h.logger.Warn("trickplay generation failed", "item_id", itemID, "error", err)
		return "", err
	}
	return itemDir, nil
}

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

func (h *ItemHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "query parameter 'q' is required")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	libraryID := r.URL.Query().Get("library_id")

	items, total, err := h.lib.ListItems(r.Context(), db.ItemFilter{
		LibraryID: libraryID,
		Query:     query,
		Limit:     limit,
	})
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = itemSummaryResponse(item)
	}

	// Enrich with poster URLs
	if h.images != nil && len(items) > 0 {
		itemIDs := make([]string, len(items))
		for i, item := range items {
			itemIDs[i] = item.ID
		}
		if imageURLs, err := h.images.GetPrimaryURLs(r.Context(), itemIDs); err == nil {
			for i, item := range items {
				if urls, ok := imageURLs[item.ID]; ok {
					if poster, ok := urls["primary"]; ok {
						data[i]["poster_url"] = poster.Path
						attachPosterPlaceholder(data[i], poster)
					}
				}
			}
		}
	}

	// Per-user state for the search results (watched/in-progress badges).
	if h.userData != nil && len(items) > 0 {
		if claims := auth.GetClaims(r.Context()); claims != nil {
			itemIDs := make([]string, len(items))
			for i, item := range items {
				itemIDs[i] = item.ID
			}
			if userDataByID, err := h.userData.GetBatch(r.Context(), claims.UserID, itemIDs); err != nil {
				h.logger.Warn("get user data batch", "error", err)
			} else if len(userDataByID) > 0 {
				for i, item := range items {
					if ud, ok := userDataByID[item.ID]; ok {
						data[i]["user_data"] = userDataResponse(ud, item.DurationTicks)
					}
				}
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data":  data,
		"total": total,
	})
}

func itemDetailResponse(item *db.Item) map[string]any {
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

func streamResponse(s *db.MediaStream) map[string]any {
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
func chapterResponse(c *db.Chapter) map[string]any {
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

func imageResponse(img *db.Image) map[string]any {
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
func attachPosterPlaceholder(entry map[string]any, ref db.PrimaryImageRef) {
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
