package db

import (
	"database/sql"
	"fmt"
)

// --- SQL para los carriles del home (constantes y templates) ---

// rawTrendingSQL — items más reproducidos en la ventana de tiempo, agrupados por serie.
const rawTrendingSQL = `
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

// rawRecommendedGenresSQL — top-3 géneros del usuario por peso de reproducción.
const rawRecommendedGenresSQL = `
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

// rawRecommendedItemsTemplate — candidatos por afinidad de género; %s(1)=GROUP_CONCAT expr, %s(2)=IN-list placeholders.
const rawRecommendedItemsTemplate = `
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

// rawBecauseSeedSQL — último item completado del usuario, fold de episodios a serie.
const rawBecauseSeedSQL = `
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

// rawBecauseSeedMetaSQL — metadata + géneros del seed; %s=GROUP_CONCAT expr.
const rawBecauseSeedMetaSQL = `
		SELECT
			i.id, i.type, i.title, CAST(i.year AS BIGINT) AS year, i.library_id,
			%s AS genres
		FROM items i
		LEFT JOIN item_value_map ivm ON ivm.item_id = i.id
		LEFT JOIN item_values iv ON iv.id = ivm.value_id AND iv.type = 'genre'
		WHERE i.id = ? AND i.is_available
		GROUP BY i.id`

// rawBecauseItemsTemplate — candidatos similares al seed; %s(1)=GROUP_CONCAT expr, %s(2)=IN-list placeholders.
const rawBecauseItemsTemplate = `
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

// rawLiveNowSQL — canales activos con programa EPG actual, ordenados por favorito y emisión.
const rawLiveNowSQL = `
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

// HomeRepository serves the cross-cutting queries the configurable
// home page needs: per-library "latest", server-wide "trending", and
// the "live now" mini-rail that joins channels with their current EPG
// program.
//
// Lives in its own files (not bolted onto Items / UserData / Channels)
// because each query joins across multiple tables AND must respect
// per-user library access, and forcing those concerns into the
// existing repos would make their interfaces noisy. Raw SQL, not
// sqlc, since each query is a one-shot with joins + aggregations
// that sqlc's positional-binding model handles awkwardly (same
// rationale as the existing NextUp / UserHasAccess raw queries).
//
// Dual-dialect: Pattern B (raw SQL only). The dialect-specific bits
// are baked into the prepared query strings at construction time:
//
//   - `GROUP_CONCAT(DISTINCT col)` ↔ `STRING_AGG(DISTINCT col, ',')`
//     (Recommended / BecauseYouWatched seed-meta + items)
//   - `WHERE col = 1` / `= 0` over BOOLEAN columns → truthy predicates
//     (`WHERE col` / `NOT col`) so Postgres' strict type check passes
//     while SQLite's 0/1 INTEGER still reads truthy.
//   - `CAST(year AS BIGINT)` so the same `sql.NullInt64` scan target
//     works against SQLite (INTEGER → int64) and Postgres (INTEGER
//     → int32 widened to BIGINT).
//   - `?` → `$N` rewrite at construction time (fixed queries) or
//     after `fmt.Sprintf` (queries with a runtime-sized IN list).
//
// El fichero está particionado por carril ("rail") para que cada
// archivo cuente una sola historia: `home_trending.go`,
// `home_recommended.go` (Recommended + BecauseYouWatched, comparten
// helpers), `home_live.go`. Este fichero conserva la construcción
// (struct + constructor + dialect helpers) y la utility de id-
// extraction compartida.
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

// groupConcatExpr returns the dialect-specific aggregate that
// concatenates a column's values with a comma separator. SQLite ships
// `GROUP_CONCAT(DISTINCT col)` (comma is the default sep); Postgres
// expects `STRING_AGG(DISTINCT col, ',')` and rejects the SQLite name.
// Centralised so a future caller (a third rail) gets the same recipe
// without re-discovering the divergence.
func groupConcatExpr(driver, col string) string {
	if IsPostgres(driver) {
		return fmt.Sprintf("STRING_AGG(DISTINCT %s, ',')", col)
	}
	return fmt.Sprintf("GROUP_CONCAT(DISTINCT %s)", col)
}

// NewHomeRepository construye el repo inyectando el dialecto SQL en las constantes.
func NewHomeRepository(driver string, database *sql.DB) *HomeRepository {
	gc := groupConcatExpr(driver, "iv.value")

	return &HomeRepository{
		db:                       database,
		driver:                   driver,
		trendingSQL:              rewritePlaceholders(driver, rawTrendingSQL),
		recommendedGenresSQL:     rewritePlaceholders(driver, rawRecommendedGenresSQL),
		recommendedItemsTemplate: fmt.Sprintf(rawRecommendedItemsTemplate, gc),
		becauseSeedSQL:           rewritePlaceholders(driver, rawBecauseSeedSQL),
		becauseSeedMetaSQL:       rewritePlaceholders(driver, fmt.Sprintf(rawBecauseSeedMetaSQL, gc)),
		becauseItemsTemplate:     fmt.Sprintf(rawBecauseItemsTemplate, gc),
		liveNowSQL:               rewritePlaceholders(driver, rawLiveNowSQL),
	}
}

// splitGroupConcat splits the comma-separated output of either
// SQLite's GROUP_CONCAT or Postgres' STRING_AGG (we always pass `,`
// as the separator for STRING_AGG to keep the parse identical).
// Genre values themselves never contain commas in our normalised
// vocabulary so a plain split is safe; documenting it here so a
// future genre with embedded comma is caught at code review.
//
// Compartido entre Recommended y BecauseYouWatched (ambos parsean
// la columna matched_genres); por eso vive aquí y no en uno de los
// dos ficheros de carril.
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

// IDsFromTrending pulls just the ID column out of trending results,
// used by the home handler to batch-load full librarymodel.Item records and
// images for response enrichment.
func IDsFromTrending(items []HomeTrendingItem) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}
