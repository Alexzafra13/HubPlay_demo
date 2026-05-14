package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

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
