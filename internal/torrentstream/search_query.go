package torrentstream

import (
	"fmt"
	"strings"
)

// Query describes a source-resolution request. It carries everything the
// aggregator needs to query the indexers BOTH ways — by IMDb id (precise,
// for indexers that support it) and by free text (title + year for movies,
// title + SxxEyy for series, the broadly-supported path) — and to verify
// that the text-search results actually match the requested title.
//
// Earlier the code only ever sent `imdbid=` to the indexers; most indexers
// don't support IMDb-id lookups and returned an empty feed, so every title
// resolved to "no sources". Carrying the title/year/season/episode lets the
// aggregator run the hybrid search that actually returns results.
type Query struct {
	Type MediaType
	// IMDbID is the canonical "ttNNN…" id, or "" when unknown. Drives the
	// precise (imdbid) search pass.
	IMDbID string
	// Title is the human title used for the text-search pass and for match
	// verification. "" disables the text pass.
	Title string
	// Year is the release year (movies). 0 = unknown. Used to disambiguate
	// remakes in the text pass.
	Year int
	// Season / Episode scope a series search. 0 = not scoped (whole series).
	Season  int
	Episode int
}

// queryMode selects how a single Torznab endpoint is queried for a Query.
type queryMode int

const (
	// modeIMDb queries by imdbid (typed movie/tvsearch). High precision; the
	// indexer matched the id, so results are trusted (not title-filtered).
	modeIMDb queryMode = iota
	// modeText queries by free text (t=search with a composed query). Broadly
	// supported; results ARE title-verified to drop false positives.
	modeText
)

// modes returns the query passes to run for this Query: by imdbid when an id
// is present, by text when a title is present. Both run and their results are
// merged, so an indexer that ignores imdbid still contributes via text.
func (q Query) modes() []queryMode {
	var m []queryMode
	if strings.TrimSpace(q.IMDbID) != "" {
		m = append(m, modeIMDb)
	}
	if strings.TrimSpace(q.Title) != "" {
		m = append(m, modeText)
	}
	return m
}

// textQuery composes the free-text search string the indexers understand:
// "Title Year" for movies, "Title SxxEyy" / "Title Sxx" for series. Returns
// "" when there's no title to search by.
func (q Query) textQuery() string {
	title := strings.TrimSpace(q.Title)
	if title == "" {
		return ""
	}
	if q.Type == MediaTypeSeries {
		switch {
		case q.Season > 0 && q.Episode > 0:
			return fmt.Sprintf("%s S%02dE%02d", title, q.Season, q.Episode)
		case q.Season > 0:
			return fmt.Sprintf("%s S%02d", title, q.Season)
		default:
			return title
		}
	}
	if q.Year > 0 {
		return fmt.Sprintf("%s %d", title, q.Year)
	}
	return title
}

// cacheKey is the stable per-request key for the raw-results cache. Every
// field that changes the indexer query is part of it so different
// years/episodes don't collide.
func (q Query) cacheKey() string {
	return fmt.Sprintf("%s|%s|%s|%d|%d|%d",
		q.Type,
		strings.ToLower(strings.TrimSpace(q.IMDbID)),
		strings.ToLower(strings.TrimSpace(q.Title)),
		q.Year, q.Season, q.Episode,
	)
}
