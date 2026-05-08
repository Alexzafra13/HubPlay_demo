// Package library content-rating ranking + filter helpers.
//
// HubPlay's content rating values come from TMDb's certifications,
// which mix MPAA (movies: G, PG, PG-13, R, NC-17) and the US TV
// Parental Guidelines (TV-Y, TV-Y7, TV-G, TV-PG, TV-14, TV-MA). The
// admin Resumen + the per-profile selector both surface ratings as
// opaque strings; the filter here turns them into a comparable
// ordinal so we can answer "is this item above the profile's cap?".
//
// Anything we don't recognise gets the highest tier — better to over-
// restrict than to leak content the family didn't expect. A future
// localisation (BBFC, FSK, ICAA, ...) would extend the table without
// touching the filter callsites.

package library

// ratingRank assigns each known certification a tier number. Lower =
// younger audience. Two systems share the table because in practice
// both appear in TMDb data and a profile rated for "PG-13" should
// also see "TV-14" content the same family-friendly tier — we map
// them to comparable rungs.
var ratingRank = map[string]int{
	// MPAA (movies)
	"G":     1,
	"PG":    2,
	"PG-13": 3,
	"R":     4,
	"NC-17": 5,

	// US TV
	"TV-Y":  1,
	"TV-Y7": 2,
	"TV-G":  1,
	"TV-PG": 2,
	"TV-14": 3,
	"TV-MA": 4,
}

const ratingRankUnknown = 5 // pessimistic — unknown labels treated as adult

// ContentRatingRank returns the comparable tier of a rating string.
// Empty rating means "unrated" — we treat that as the most-permissive
// case (rank 0) so unrated items only slip through profiles with no
// cap set; the cap comparison `itemRank > capRank` automatically
// blocks unrated items for restricted profiles when the cap is non-
// zero.
func ContentRatingRank(rating string) int {
	if rating == "" {
		return 0
	}
	if r, ok := ratingRank[rating]; ok {
		return r
	}
	return ratingRankUnknown
}

// AllowedRating returns true when an item with `itemRating` is below
// or equal to the profile's `capRating`. Empty cap = "no
// restriction"; everything passes. Empty itemRating ("unrated")
// passes only when the profile has no cap, since we can't tell
// whether an unrated item is family-friendly or hardcore.
func AllowedRating(itemRating, capRating string) bool {
	if capRating == "" {
		return true
	}
	cap, ok := ratingRank[capRating]
	if !ok {
		// Unknown cap → fail-open to "no restriction" rather than
		// locking the user out of everything they own. Logged
		// elsewhere; the operator should fix the cap value.
		return true
	}
	if itemRating == "" {
		// Unrated against a non-empty cap: deny. Unrated content in
		// a TMDb library is usually old / international where TMDb
		// hasn't categorised it; safer not to surface to a kid
		// profile.
		return false
	}
	return ContentRatingRank(itemRating) <= cap
}

// AllowedRatingsAtMost returns the slice of known rating literals
// that pass the cap. Used by the SQL filter callsites that need an
// explicit `IN (...)` list rather than a function call (SQLite can't
// register Go callbacks safely without CGO; we materialise the
// allowed list once).
func AllowedRatingsAtMost(capRating string) []string {
	if capRating == "" {
		return nil
	}
	cap, ok := ratingRank[capRating]
	if !ok {
		return nil // fail-open
	}
	out := make([]string, 0, len(ratingRank))
	for rating, r := range ratingRank {
		if r <= cap {
			out = append(out, rating)
		}
	}
	return out
}
