package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"hubplay/internal/auth"
	"hubplay/internal/db"

	"github.com/go-chi/chi/v5"
)

type ItemHandler struct {
	lib      LibraryService
	images   ImageRepository
	metadata MetadataRepository
	userData UserDataRepository
	logger   *slog.Logger
}

func NewItemHandler(lib LibraryService, images ImageRepository, metadata MetadataRepository, userData UserDataRepository, logger *slog.Logger) *ItemHandler {
	return &ItemHandler{lib: lib, images: images, metadata: metadata, userData: userData, logger: logger}
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

	// Include images and set poster_url/backdrop_url
	images, _ := h.lib.GetItemImages(r.Context(), id)
	if len(images) > 0 {
		imgData := make([]map[string]any, len(images))
		for i, img := range images {
			imgData[i] = imageResponse(img)
		}
		resp["images"] = imgData

		for _, img := range images {
			if img.IsPrimary && img.Type == "primary" {
				resp["poster_url"] = img.Path
			}
			if img.IsPrimary && img.Type == "backdrop" {
				resp["backdrop_url"] = img.Path
			}
			if img.IsPrimary && img.Type == "logo" {
				resp["logo_url"] = img.Path
			}
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
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *ItemHandler) Children(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	children, err := h.lib.GetItemChildren(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	data := make([]map[string]any, len(children))
	for i, item := range children {
		data[i] = itemSummaryResponse(item)
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
						data[i]["poster_url"] = poster
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
		"year":           item.Year,
		"path":           item.Path,
		"size":           item.Size,
		"duration_ticks": item.DurationTicks,
		"container":      item.Container,
		"is_available":   item.IsAvailable,
		"added_at":       item.AddedAt,
		"updated_at":     item.UpdatedAt,
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

func imageResponse(img *db.Image) map[string]any {
	resp := map[string]any{
		"id":         img.ID,
		"type":       img.Type,
		"path":       img.Path,
		"is_primary": img.IsPrimary,
	}
	if img.Width > 0 {
		resp["width"] = img.Width
		resp["height"] = img.Height
	}
	if img.Blurhash != "" {
		resp["blurhash"] = img.Blurhash
	}
	return resp
}
