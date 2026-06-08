package handlers

import (
	librarymodel "hubplay/internal/library/model"
)

// AttachPosterPlaceholder incorpora a una entrada de listado los campos
// baratos de placeholder de carga para la imagen de póster. PosterCard
// renderiza el color sólido como fondo mientras el <img> real decodifica,
// para que las tarjetas no salten de gris a imagen. Exportada para uso
// entre los sub-paquetes de handlers (library, media, me).
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

// UserDataResponse renderiza una fila de UserData en la shape canónica del
// cliente. `percentage` se calcula server-side y se acota a [0, 100].
// Exportada para uso entre los sub-paquetes de handlers.
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

// ItemSummaryResponse crea la wire shape compacta de un item usada en los
// endpoints de listado (browse de biblioteca, children, búsqueda). Exportada
// para uso entre los sub-paquetes de handlers.
func ItemSummaryResponse(item *librarymodel.Item) map[string]any {
	resp := map[string]any{
		"id":         item.ID,
		"library_id": item.LibraryID,
		"type":       item.Type,
		"title":      item.Title,
		// `sort_title` es la variante en minúsculas + sin artículo que el
		// backend almacena para el ORDER BY de SQL (de modo que "The Matrix"
		// ordena como "matrix"). La página de browse también re-ordena
		// client-side cuando el usuario elige "title" — sin este campo en el
		// wire hacía `undefined.localeCompare(...)` y crasheaba el grid.
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
