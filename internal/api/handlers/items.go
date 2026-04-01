package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"hubplay/internal/db"

	"github.com/go-chi/chi/v5"
)

type ItemHandler struct {
	lib      LibraryService
	images   ImageRepository
	metadata MetadataRepository
	logger   *slog.Logger
}

func NewItemHandler(lib LibraryService, images ImageRepository, metadata MetadataRepository, logger *slog.Logger) *ItemHandler {
	return &ItemHandler{lib: lib, images: images, metadata: metadata, logger: logger}
}

func (h *ItemHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, err := h.lib.GetItem(r.Context(), id)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	resp := itemDetailResponse(item)

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
		handleServiceError(w, err)
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
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "query parameter 'q' is required")
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
		handleServiceError(w, err)
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
