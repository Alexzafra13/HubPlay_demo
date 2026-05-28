package handlers

import (
	librarymodel "hubplay/internal/library/model"
)

// AttachPosterPlaceholder folds the cheap loading-placeholder fields
// for the poster image into a listing entry. PosterCard renders the
// solid color as background while the real <img> decodes, so cards
// don't pop from grey to image. Exported for use across handler
// sub-packages (library, media, me).
func AttachPosterPlaceholder(entry map[string]any, ref librarymodel.PrimaryImageRef) {
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

// UserDataResponse renders a UserData row in the canonical client
// shape. `percentage` is computed server-side and clamped to [0, 100].
// Exported for use across handler sub-packages.
func UserDataResponse(ud *librarymodel.UserData, durationTicks int64) map[string]any {
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

// ItemSummaryResponse creates the compact wire shape for an item used
// in listing endpoints (library browse, children, search). Exported
// for use across handler sub-packages.
func ItemSummaryResponse(item *librarymodel.Item) map[string]any {
	resp := map[string]any{
		"id":         item.ID,
		"library_id": item.LibraryID,
		"type":       item.Type,
		"title":      item.Title,
		// `sort_title` is the lowercased + article-stripped variant the
		// backend stores for SQL ORDER BY (so "The Matrix" sorts as
		// "matrix"). The browse page also re-sorts client-side when
		// the user picks "title" — without this field on the wire it
		// did `undefined.localeCompare(...)` and crashed the grid.
		"sort_title":     item.SortTitle,
		"duration_ticks": item.DurationTicks,
		"is_available":   item.IsAvailable,
		"added_at":       item.AddedAt,
	}
	if item.Year > 0 {
		resp["year"] = item.Year
	}
	if item.ParentID != "" {
		resp["parent_id"] = item.ParentID
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
