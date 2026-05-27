package db

import (
	"database/sql"
	"fmt"

	librarymodel "hubplay/internal/library/model"
)

// HomeRepository sirve las queries cross-cutting de la home page
// configurable: "latest" per-library, "trending" server-wide, y el
// mini-rail "live now" que une canales con su programa EPG actual.
//
// Dual-dialect (Pattern B, raw SQL). Bits dialect-specific se baked
// en los query strings al construir: GROUP_CONCAT vs STRING_AGG,
// predicados booleanos truthy, CAST(year AS BIGINT), y rewrite de
// placeholders ?->$N.
type HomeRepository struct {
	db     *sql.DB
	driver string

	trendingSQL              string
	recommendedGenresSQL     string
	recommendedItemsTemplate string // %s = IN-list placeholders (raw `?`s before rewrite)
	becauseSeedSQL           string
	becauseSeedMetaSQL       string
	becauseItemsTemplate     string // %s = IN-list placeholders (raw `?`s before rewrite)
	liveNowSQL               string
}

// groupConcatExpr devuelve el aggregate dialect-specific que concatena
// valores de columna con coma. SQLite: GROUP_CONCAT; Postgres: STRING_AGG.
func groupConcatExpr(driver, col string) string {
	if IsPostgres(driver) {
		return fmt.Sprintf("STRING_AGG(DISTINCT %s, ',')", col)
	}
	return fmt.Sprintf("GROUP_CONCAT(DISTINCT %s)", col)
}

// ── SQL raw (pre-rewrite) para cada rail de la home ─────────────────

// sqlTrending — rail "Trending": items más reproducidos cross-user.
// CAST(year AS BIGINT) necesario para Postgres (INTEGER 32-bit);
// no-op en SQLite.
const sqlTrending = `
WITH plays AS (
	SELECT
		ud.user_id,
		CASE
			WHEN i.type = 'episode' AND i.parent_id IS NOT NULL
				THEN COALESCE(
					(SELECT s.parent_id FROM items s WHERE s.id = i.parent_id),
					i.parent_id
				)
			ELSE i.id
		END AS rollup_id,
		ud.last_played_at
	FROM user_data ud
	JOIN items i ON i.id = ud.item_id
	WHERE ud.last_played_at >= ?
	  AND i.is_available
)
SELECT
	i.id, i.type, i.title, CAST(i.year AS BIGINT) AS year, i.community_rating, i.library_id,
	COUNT(DISTINCT p.user_id) AS votes,
	MAX(p.last_played_at)     AS last_played_at,
	COALESCE(i.content_rating, '') AS content_rating
FROM plays p
JOIN items i ON i.id = p.rollup_id
WHERE i.is_available
  AND EXISTS (
	SELECT 1 FROM library_access la
	JOIN users u ON u.id = ?
	WHERE la.library_id = i.library_id
	  AND la.user_id = COALESCE(u.parent_user_id, u.id)
  )
GROUP BY i.id
ORDER BY votes DESC, last_played_at DESC
LIMIT ?`

// sqlRecommendedGenres — top 3 géneros del usuario por play-count.
const sqlRecommendedGenres = `
WITH played AS (
	SELECT
		CASE
			WHEN i.type = 'episode' AND i.parent_id IS NOT NULL
				THEN COALESCE(
					(SELECT s.parent_id FROM items s WHERE s.id = i.parent_id),
					i.parent_id
				)
			ELSE i.id
		END AS rollup_id
	FROM user_data ud
	JOIN items i ON i.id = ud.item_id
	WHERE ud.user_id = ? AND ud.position_ticks > 0
)
SELECT iv.value, COUNT(DISTINCT p.rollup_id) AS weight
FROM played p
JOIN item_value_map ivm ON ivm.item_id = p.rollup_id
JOIN item_values iv ON iv.id = ivm.value_id AND iv.type = 'genre'
GROUP BY iv.value
ORDER BY weight DESC, iv.value ASC
LIMIT 3`

// sqlRecommendedItemsTpl — template con %%s para IN-list de géneros y
// %s para groupConcatExpr (inyectado al construir).
const sqlRecommendedItemsTpl = `
SELECT
	i.id, i.type, i.title, CAST(i.year AS BIGINT) AS year, i.community_rating, i.library_id,
	COALESCE(i.content_rating, '') AS content_rating,
	COUNT(DISTINCT iv.value) AS genre_hits,
	%s AS matched_genres
FROM items i
JOIN item_value_map ivm ON ivm.item_id = i.id
JOIN item_values iv ON iv.id = ivm.value_id AND iv.type = 'genre' AND iv.value IN (%%s)
LEFT JOIN user_data ud ON ud.user_id = ? AND ud.item_id = i.id
WHERE i.is_available
  AND i.type IN ('movie', 'series')
  AND (
	ud.item_id IS NULL
	OR (NOT ud.completed AND (i.duration_ticks = 0 OR ud.position_ticks * 100 < i.duration_ticks * 5))
  )
  AND EXISTS (
	SELECT 1 FROM library_access la
	JOIN users u ON u.id = ?
	WHERE la.library_id = i.library_id
	  AND la.user_id = COALESCE(u.parent_user_id, u.id)
  )
GROUP BY i.id
ORDER BY genre_hits DESC, COALESCE(i.community_rating, 0) DESC, i.added_at DESC
LIMIT ?`

// sqlBecauseSeed — último item completado del usuario (seed para BYW).
const sqlBecauseSeed = `
SELECT
	CASE
		WHEN i.type = 'episode' AND i.parent_id IS NOT NULL
			THEN COALESCE(
				(SELECT s.parent_id FROM items s WHERE s.id = i.parent_id),
				i.parent_id
			)
		ELSE i.id
	END AS rollup_id,
	ud.last_played_at
FROM user_data ud
JOIN items i ON i.id = ud.item_id
WHERE ud.user_id = ? AND ud.completed AND i.is_available
ORDER BY ud.last_played_at DESC
LIMIT 1`

// sqlBecauseSeedMetaTpl — metadata + géneros del seed item.
// %s para groupConcatExpr (inyectado al construir).
const sqlBecauseSeedMetaTpl = `
SELECT
	i.id, i.type, i.title, CAST(i.year AS BIGINT) AS year, i.library_id,
	%s AS genres
FROM items i
LEFT JOIN item_value_map ivm ON ivm.item_id = i.id
LEFT JOIN item_values iv ON iv.id = ivm.value_id AND iv.type = 'genre'
WHERE i.id = ? AND i.is_available
GROUP BY i.id`

// sqlBecauseItemsTpl — items similares al seed. Mismo patrón template
// que sqlRecommendedItemsTpl, con exclusión del seed id.
const sqlBecauseItemsTpl = `
SELECT
	i.id, i.type, i.title, CAST(i.year AS BIGINT) AS year, i.community_rating, i.library_id,
	COALESCE(i.content_rating, '') AS content_rating,
	COUNT(DISTINCT iv.value) AS genre_hits,
	%s AS matched_genres
FROM items i
JOIN item_value_map ivm ON ivm.item_id = i.id
JOIN item_values iv ON iv.id = ivm.value_id AND iv.type = 'genre' AND iv.value IN (%%s)
LEFT JOIN user_data ud ON ud.user_id = ? AND ud.item_id = i.id
WHERE i.is_available
  AND i.type IN ('movie', 'series')
  AND (
	ud.item_id IS NULL
	OR (NOT ud.completed AND (i.duration_ticks = 0 OR ud.position_ticks * 100 < i.duration_ticks * 5))
  )
  AND EXISTS (
	SELECT 1 FROM library_access la
	JOIN users u ON u.id = ?
	WHERE la.library_id = i.library_id
	  AND la.user_id = COALESCE(u.parent_user_id, u.id)
  )
  AND i.id <> ?
GROUP BY i.id
ORDER BY genre_hits DESC, COALESCE(i.community_rating, 0) DESC, i.added_at DESC
LIMIT ?`

// sqlLiveNow — canales activos con programa EPG actual.
const sqlLiveNow = `
SELECT
	c.id, c.name, c.logo_url, c.library_id, l.name AS library_name,
	ep.title, ep.start_time, ep.end_time, ep.icon_url,
	CASE WHEN cf.channel_id IS NOT NULL THEN 1 ELSE 0 END AS is_fav,
	CASE WHEN ep.id IS NOT NULL          THEN 1 ELSE 0 END AS has_now
FROM channels c
JOIN libraries l ON l.id = c.library_id
LEFT JOIN epg_programs ep
	ON ep.channel_id = c.id
	AND ep.start_time <= ?
	AND ep.end_time   > ?
LEFT JOIN user_channel_favorites cf
	ON cf.channel_id = c.id AND cf.user_id = ?
WHERE c.is_active
  AND c.consecutive_failures < ?
  AND EXISTS (
	SELECT 1 FROM library_access la
	JOIN users u ON u.id = ?
	WHERE la.library_id = c.library_id
	  AND la.user_id = COALESCE(u.parent_user_id, u.id)
  )
ORDER BY is_fav DESC, has_now DESC, c.name ASC
LIMIT ?`

func NewHomeRepository(driver string, database *sql.DB) *HomeRepository {
	r := &HomeRepository{db: database, driver: driver}
	gcExpr := groupConcatExpr(driver, "iv.value")

	r.trendingSQL = rewritePlaceholders(driver, sqlTrending)
	r.recommendedGenresSQL = rewritePlaceholders(driver, sqlRecommendedGenres)
	r.recommendedItemsTemplate = fmt.Sprintf(sqlRecommendedItemsTpl, gcExpr)
	r.becauseSeedSQL = rewritePlaceholders(driver, sqlBecauseSeed)
	r.becauseSeedMetaSQL = rewritePlaceholders(driver, fmt.Sprintf(sqlBecauseSeedMetaTpl, gcExpr))
	r.becauseItemsTemplate = fmt.Sprintf(sqlBecauseItemsTpl, gcExpr)
	r.liveNowSQL = rewritePlaceholders(driver, sqlLiveNow)

	return r
}

// splitGroupConcat separa la salida comma-separated de GROUP_CONCAT o
// STRING_AGG. Compartido entre Recommended y BecauseYouWatched.
func splitGroupConcat(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// IDsFromTrending extrae solo los IDs de los resultados de trending.
func IDsFromTrending(items []librarymodel.HomeTrendingItem) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}
