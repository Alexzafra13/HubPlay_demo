package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// HomeRepository serves the cross-cutting queries the configurable
// home page needs: per-library "latest", server-wide "trending", and
// the "live now" mini-rail that joins channels with their current EPG
// program.
//
// Lives in its own file (not bolted onto Items / UserData / Channels)
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
type HomeRepository struct {
	db     *sql.DB
	driver string

	trendingSQL                string
	recommendedGenresSQL       string
	recommendedItemsTemplate   string // %s = IN-list placeholders (raw `?`s before rewrite)
	becauseSeedSQL             string
	becauseSeedMetaSQL         string
	becauseItemsTemplate       string // %s = IN-list placeholders (raw `?`s before rewrite)
	liveNowSQL                 string
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

func NewHomeRepository(driver string, database *sql.DB) *HomeRepository {
	r := &HomeRepository{db: database, driver: driver}

	// CAST(i.year AS BIGINT) is needed for Postgres where the column
	// is INTEGER (32-bit). The cast is a no-op on SQLite (BIGINT is
	// an alias of INTEGER under the type-affinity rules). Same Scan
	// target (`sql.NullInt64`) works against both.
	r.trendingSQL = rewritePlaceholders(driver, `
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
		LIMIT ?`)

	r.recommendedGenresSQL = rewritePlaceholders(driver, `
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
		LIMIT 3`)

	// Template — placeholders for IN list inserted later via
	// fmt.Sprintf, rewrite to $N happens after that injection so the
	// counter sees every `?`.
	r.recommendedItemsTemplate = `
		SELECT
			i.id, i.type, i.title, CAST(i.year AS BIGINT) AS year, i.community_rating, i.library_id,
			COALESCE(i.content_rating, '') AS content_rating,
			COUNT(DISTINCT iv.value) AS genre_hits,
			` + groupConcatExpr(driver, "iv.value") + ` AS matched_genres
		FROM items i
		JOIN item_value_map ivm ON ivm.item_id = i.id
		JOIN item_values iv ON iv.id = ivm.value_id AND iv.type = 'genre' AND iv.value IN (%s)
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

	r.becauseSeedSQL = rewritePlaceholders(driver, `
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
		LIMIT 1`)

	r.becauseSeedMetaSQL = rewritePlaceholders(driver, `
		SELECT
			i.id, i.type, i.title, CAST(i.year AS BIGINT) AS year, i.library_id,
			`+groupConcatExpr(driver, "iv.value")+` AS genres
		FROM items i
		LEFT JOIN item_value_map ivm ON ivm.item_id = i.id
		LEFT JOIN item_values iv ON iv.id = ivm.value_id AND iv.type = 'genre'
		WHERE i.id = ? AND i.is_available
		GROUP BY i.id`)

	r.becauseItemsTemplate = `
		SELECT
			i.id, i.type, i.title, CAST(i.year AS BIGINT) AS year, i.community_rating, i.library_id,
			COALESCE(i.content_rating, '') AS content_rating,
			COUNT(DISTINCT iv.value) AS genre_hits,
			` + groupConcatExpr(driver, "iv.value") + ` AS matched_genres
		FROM items i
		JOIN item_value_map ivm ON ivm.item_id = i.id
		JOIN item_values iv ON iv.id = ivm.value_id AND iv.type = 'genre' AND iv.value IN (%s)
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

	r.liveNowSQL = rewritePlaceholders(driver, `
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
		LIMIT ?`)

	return r
}

// HomeTrendingItem is one entry in the trending rail.
type HomeTrendingItem struct {
	ID              string
	Type            string
	Title           string
	Year            sql.NullInt64
	CommunityRating sql.NullFloat64
	LibraryID       string
	PlayCount       int64
	LastPlayedAt    time.Time
	// ContentRating exposed so the handler can apply per-profile
	// rating caps post-fetch. We push it through the same SELECT
	// rather than a second per-row query — the items table is
	// already in the FROM clause for trending so the column is free.
	ContentRating string
}

// Trending returns the top `limit` items played across ALL users in
// the last `windowDays`, scoped to libraries the caller can see.
// Items the user can't access (private library) are filtered out at
// the SQL level via the same library_access EXISTS pattern the rest
// of the codebase uses.
//
// Counts plays as "user_data rows touched in the window" rather than
// "play events" — HubPlay doesn't keep a play-event log; user_data
// last_played_at is updated on every progress write, which is the
// closest signal we have. A user that resumes a movie three times in
// a week counts as one trending vote, not three. That's a feature:
// it prevents one obsessive viewer from skewing the ranking.
//
// Movies and individual episodes count, but episodes are folded back
// to their series so the rail surfaces "Game of Thrones is hot",
// not "S04E09 is hot". Series ranking aggregates plays of all its
// episodes via the parent_id climb (one CTE).
func (r *HomeRepository) Trending(ctx context.Context, userID string, windowDays, limit int) ([]HomeTrendingItem, error) {
	if windowDays <= 0 {
		windowDays = 7
	}
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	cutoff := time.Now().UTC().Add(-time.Duration(windowDays) * 24 * time.Hour)

	rows, err := r.db.QueryContext(ctx, r.trendingSQL, cutoff, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("trending: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := make([]HomeTrendingItem, 0, limit)
	for rows.Next() {
		var it HomeTrendingItem
		var lastPlayedRaw any
		if err := rows.Scan(&it.ID, &it.Type, &it.Title, &it.Year, &it.CommunityRating,
			&it.LibraryID, &it.PlayCount, &lastPlayedRaw, &it.ContentRating); err != nil {
			return nil, fmt.Errorf("scan trending row: %w", err)
		}
		it.LastPlayedAt, err = coerceSQLiteTime(lastPlayedRaw)
		if err != nil {
			return nil, fmt.Errorf("parse last_played_at: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// HomeRecommendation is one item the genre-affinity recommender
// surfaced for the caller, plus the genres that triggered the match.
// `Because` is the slice of genres (already humanly-cased) the user
// most actively watches that this item shares — copy the homepage
// renders as "Porque te gusta Drama, Thriller". Movies and series
// only; episodes are filtered at the SQL level.
type HomeRecommendation struct {
	ID              string
	Type            string
	Title           string
	Year            sql.NullInt64
	CommunityRating sql.NullFloat64
	LibraryID       string
	Because         []string
	// ContentRating exposed so the handler can apply per-profile
	// rating caps post-fetch — same rationale as on
	// HomeTrendingItem.
	ContentRating string
}

// Recommended returns up to `limit` items the caller hasn't watched
// (no user_data row, or watched < 5% with no completion mark) drawn
// from the genres the caller most actively engages with. Acts as the
// "Recomendado para ti" tier of the home hero without depending on a
// metadata provider being reachable.
//
// Ranking strategy:
//
//  1. Compute the caller's top-3 genres by play weight (any user_data
//     row with progress counts as one vote per genre on the played
//     item, episodes folded back to their series so binge-watching
//     one show doesn't dominate).
//  2. Score every unwatched item by how many of those top genres it
//     hits, breaking ties on community_rating then added_at.
//  3. Return the top `limit` along with the genres that scored them.
//
// Returns (nil, nil) when the user has no engagement history yet —
// the cold-start case. Caller decides whether to fall back to a
// generic "newest in catalogue" rail or hide the slot.
//
// Library access enforced via the same EXISTS pattern as Trending.
func (r *HomeRepository) Recommended(ctx context.Context, userID string, limit int) ([]HomeRecommendation, error) {
	if limit <= 0 || limit > 30 {
		limit = 5
	}

	// First pass: pull the caller's top-3 genres. Empty slice = the
	// user has touched nothing yet, so no personalised pick is
	// possible. The handler treats this as "skip the slot".
	rows, err := r.db.QueryContext(ctx, r.recommendedGenresSQL, userID)
	if err != nil {
		return nil, fmt.Errorf("recommended seeds: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var topGenres []string
	for rows.Next() {
		var g string
		var w int64
		if err := rows.Scan(&g, &w); err != nil {
			return nil, fmt.Errorf("scan recommended seed: %w", err)
		}
		topGenres = append(topGenres, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(topGenres) == 0 {
		return nil, nil
	}

	// Second pass: score candidate items by genre overlap. The
	// `genre_hits` aggregate counts how many of the user's top-3
	// genres the candidate carries — that's the primary sort. We
	// surface that count plus the actual matched genres so the wire
	// can render "Porque te gusta {{genre1}}, {{genre2}}".
	//
	// Unwatched filter: no user_data row OR row with position_ticks <
	// 5% of duration AND not completed. The 5% threshold matches the
	// "user opened the player by accident" case — a 30-second play on
	// a 2-hour movie shouldn't flag it as watched.
	// Param order matches the placeholder order in the query body:
	//   1. iv.value IN (?,?,?)       — top genres
	//   2. ud.user_id = ?            — left join filter
	//   3. la.user_id = ?            — library access guard
	//   4. LIMIT ?
	placeholders := "?"
	for i := 1; i < len(topGenres); i++ {
		placeholders += ",?"
	}
	args := make([]any, 0, len(topGenres)+3)
	for _, g := range topGenres {
		args = append(args, g)
	}
	args = append(args, userID, userID, limit)

	itemsQuery := rewritePlaceholders(r.driver, fmt.Sprintf(r.recommendedItemsTemplate, placeholders))

	itemsRows, err := r.db.QueryContext(ctx, itemsQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("recommended items: %w", err)
	}
	defer itemsRows.Close() //nolint:errcheck

	out := make([]HomeRecommendation, 0, limit)
	for itemsRows.Next() {
		var rec HomeRecommendation
		var matched sql.NullString
		var genreHits int64
		if err := itemsRows.Scan(&rec.ID, &rec.Type, &rec.Title, &rec.Year, &rec.CommunityRating,
			&rec.LibraryID, &rec.ContentRating, &genreHits, &matched); err != nil {
			return nil, fmt.Errorf("scan recommended item: %w", err)
		}
		if matched.Valid && matched.String != "" {
			rec.Because = splitGroupConcat(matched.String)
		}
		out = append(out, rec)
	}
	return out, itemsRows.Err()
}

// HomeBecauseSeed is the "this is the item that triggered the rail"
// row that pairs with a list of recommendations. The wire format
// renders "Porque viste {{seed.title}}" as the rail title, so we
// surface enough metadata for that header without making the
// frontend do a second round-trip on the seed item.
type HomeBecauseSeed struct {
	ID        string
	Type      string
	Title     string
	Year      sql.NullInt64
	LibraryID string
}

// HomeBecauseResult is the one-call payload for the
// "Because-you-watched" rail: the seed (the item that lit up the
// rail) plus a list of recommendations that share genres with it.
// items.Because already exists in HomeRecommendation; we re-use
// that shape so the frontend has one card vocabulary across both
// rails ("Recomendado para ti" and "Porque viste X").
type HomeBecauseResult struct {
	Seed  *HomeBecauseSeed
	Items []HomeRecommendation
}

// BecauseYouWatched picks the user's most recent COMPLETED watch
// and returns items that share ≥1 genre with it. The seed is
// folded for episodes (an episode's "completed" lights up the
// whole series as the seed) so the rail header reads
// "Porque viste Breaking Bad", not "Porque viste S05E14".
//
// Returns (nil, nil) when the user has no completed watches yet —
// the cold-start case. Caller hides the slot rather than rendering
// an empty rail.
//
// Strategy:
//
//   1. Find the latest user_data row with completed = true for this
//      user. Episodes fold to the parent series id (climb
//      parent_id twice). Movies / series stay as themselves.
//   2. Look up the seed item's genres + title for the rail
//      header. Bail when the seed has no genres tagged
//      (recommendations would be unfocused).
//   3. Score every unwatched movie / series by how many of the
//      seed's genres it carries, surface the top `limit`.
func (r *HomeBecauseResult) IsEmpty() bool {
	return r == nil || r.Seed == nil
}

func (r *HomeRepository) BecauseYouWatched(ctx context.Context, userID string, limit int) (*HomeBecauseResult, error) {
	if limit <= 0 || limit > 30 {
		limit = 12
	}

	// Step 1: find the seed. Pick the most recent completed watch,
	// folding episodes to their parent series id. We deliberately
	// don't constrain by item type — a finished movie or completed
	// series both qualify. The rollup expression mirrors the one in
	// Trending so the same "binge-completed Breaking Bad" event
	// shows up consistently across rails.
	var seedID string
	var lastPlayedRaw any
	if err := r.db.QueryRowContext(ctx, r.becauseSeedSQL, userID).
		Scan(&seedID, &lastPlayedRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("because seed: %w", err)
	}

	// Step 2: pull the seed's metadata for the rail header + the
	// genre list we'll use to score candidates. We do this in a
	// single query rather than two so we don't have to ferry the
	// seed id between calls.
	var seed HomeBecauseSeed
	var genresRaw sql.NullString
	if err := r.db.QueryRowContext(ctx, r.becauseSeedMetaSQL, seedID).
		Scan(&seed.ID, &seed.Type, &seed.Title, &seed.Year, &seed.LibraryID, &genresRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("because seed meta: %w", err)
	}
	if !genresRaw.Valid || genresRaw.String == "" {
		// No genres tagged → recommendations would be too noisy
		// to be useful. Hide the rail rather than show
		// "everything in the catalogue".
		return &HomeBecauseResult{Seed: &seed, Items: nil}, nil
	}
	genres := splitGroupConcat(genresRaw.String)
	if len(genres) == 0 {
		return &HomeBecauseResult{Seed: &seed, Items: nil}, nil
	}

	// Step 3: score candidates. Same shape as Recommended, but
	// scoped to the seed's specific genres rather than the user's
	// top-3, AND we exclude the seed itself from the result so
	// "Porque viste X" doesn't include X.
	placeholders := "?"
	for i := 1; i < len(genres); i++ {
		placeholders += ",?"
	}
	args := make([]any, 0, len(genres)+4)
	for _, g := range genres {
		args = append(args, g)
	}
	args = append(args, userID, userID, seed.ID, limit)

	itemsQuery := rewritePlaceholders(r.driver, fmt.Sprintf(r.becauseItemsTemplate, placeholders))

	itemsRows, err := r.db.QueryContext(ctx, itemsQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("because items: %w", err)
	}
	defer itemsRows.Close() //nolint:errcheck

	out := make([]HomeRecommendation, 0, limit)
	for itemsRows.Next() {
		var rec HomeRecommendation
		var matched sql.NullString
		var genreHits int64
		if err := itemsRows.Scan(&rec.ID, &rec.Type, &rec.Title, &rec.Year, &rec.CommunityRating,
			&rec.LibraryID, &rec.ContentRating, &genreHits, &matched); err != nil {
			return nil, fmt.Errorf("scan because item: %w", err)
		}
		if matched.Valid && matched.String != "" {
			rec.Because = splitGroupConcat(matched.String)
		}
		out = append(out, rec)
	}
	if err := itemsRows.Err(); err != nil {
		return nil, err
	}

	// lastPlayedRaw is a side-effect of the seed query; not
	// surfaced on the wire today, but we drop it explicitly so
	// staticcheck doesn't flag the var as unused.
	_ = lastPlayedRaw

	return &HomeBecauseResult{Seed: &seed, Items: out}, nil
}

// splitGroupConcat splits the comma-separated output of either
// SQLite's GROUP_CONCAT or Postgres' STRING_AGG (we always pass `,`
// as the separator for STRING_AGG to keep the parse identical).
// Genre values themselves never contain commas in our normalised
// vocabulary so a plain split is safe; documenting it here so a
// future genre with embedded comma is caught at code review.
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

// HomeLiveNowChannel is one entry in the "live now" rail.
type HomeLiveNowChannel struct {
	ChannelID    string
	ChannelName  string
	ChannelLogo  sql.NullString
	LibraryID    string
	LibraryName  string
	ProgramTitle sql.NullString
	ProgramStart sql.NullTime
	ProgramEnd   sql.NullTime
	ProgramIcon  sql.NullString
}

// LiveNow returns up to `limit` channels with their current EPG
// program. Order:
//
//   1. User's favourited channels (user_channel_favorites) first
//   2. Then channels with a program currently airing
//   3. Then anything else, by name
//
// Restricted to libraries the user can access. Inactive channels
// (is_active = false) are skipped — they're disabled at the source.
// Unhealthy channels (consecutive_failures >= UnhealthyThreshold) are
// also excluded so the rail and the LiveTV channel list stay in sync —
// otherwise clicking a card here deep-links into LiveTV with a channel
// id that LiveTV's healthy-only fetch doesn't surface, and the player
// never opens.
func (r *HomeRepository) LiveNow(ctx context.Context, userID string, limit int) ([]HomeLiveNowChannel, error) {
	if limit <= 0 || limit > 30 {
		limit = 5
	}
	now := time.Now().UTC()

	rows, err := r.db.QueryContext(ctx, r.liveNowSQL, now, now, userID, UnhealthyThreshold, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("live now: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := make([]HomeLiveNowChannel, 0, limit)
	for rows.Next() {
		var c HomeLiveNowChannel
		var startRaw, endRaw any
		var isFav, hasNow int
		if err := rows.Scan(&c.ChannelID, &c.ChannelName, &c.ChannelLogo,
			&c.LibraryID, &c.LibraryName,
			&c.ProgramTitle, &startRaw, &endRaw, &c.ProgramIcon,
			&isFav, &hasNow); err != nil {
			return nil, fmt.Errorf("scan live-now row: %w", err)
		}
		if startRaw != nil {
			t, err := coerceSQLiteTime(startRaw)
			if err != nil {
				return nil, fmt.Errorf("parse program start: %w", err)
			}
			c.ProgramStart = sql.NullTime{Time: t, Valid: true}
		}
		if endRaw != nil {
			t, err := coerceSQLiteTime(endRaw)
			if err != nil {
				return nil, fmt.Errorf("parse program end: %w", err)
			}
			c.ProgramEnd = sql.NullTime{Time: t, Valid: true}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// IDsFromTrending pulls just the ID column out of trending results,
// used by the home handler to batch-load full Item records and
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
