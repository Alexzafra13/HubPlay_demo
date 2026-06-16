package torrentstream

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Free-text indexer searches are noisy: a query for "Dune 2021" can return
// "Dune 1984", "Dune Part Two", soundtracks, or unrelated releases that
// merely share a word. matchesQuery is the verification gate applied to the
// TEXT search pass (the imdbid pass is trusted and skips this) so the curated
// list only contains releases that plausibly match the requested title.
//
// The rule set, in order:
//   - an indexer-reported IMDb id equal to the query's id → accept outright;
//   - every significant token of the requested title must appear in the
//     release title (case/accent-insensitive);
//   - for movies with a known year, if the release title carries year tokens
//     none may be off by more than one (kills remakes / wrong editions);
//   - for a scoped series episode, the release title must contain the SxxEyy
//     (or the Sxx season-pack) marker.
func matchesQuery(r SearchResult, q Query) bool {
	// High confidence: the indexer told us this release IS the requested id.
	if id := strings.TrimSpace(q.IMDbID); id != "" && r.IMDbID != "" &&
		strings.EqualFold(r.IMDbID, id) {
		return true
	}

	wantTitle := normalizeTitle(q.Title)
	if wantTitle == "" {
		return true // nothing to verify against
	}
	gotTitle := normalizeTitle(r.Title)
	for _, tok := range significantTokens(wantTitle) {
		if !containsToken(gotTitle, tok) {
			return false
		}
	}

	// Year disambiguation for movies (series episodes rarely carry the show's
	// premiere year, so don't penalise them on year).
	if q.Type != MediaTypeSeries && q.Year > 0 {
		if years := yearsIn(r.Title); len(years) > 0 && !anyYearWithin(years, q.Year, 1) {
			return false
		}
	}

	// Episode scoping: when a specific S/E was requested, require it.
	if q.Type == MediaTypeSeries && q.Season > 0 && q.Episode > 0 {
		if !containsEpisode(r.Title, q.Season, q.Episode) {
			return false
		}
	}

	return true
}

// accentFolder maps common Latin-1/Spanish accented runes to their ASCII
// base so "Amélie" matches "Amelie" and "Coração" matches "Coracao". Kept as
// a small replacer to avoid pulling in golang.org/x/text just for this.
var accentFolder = strings.NewReplacer(
	"á", "a", "à", "a", "ä", "a", "â", "a", "ã", "a",
	"é", "e", "è", "e", "ë", "e", "ê", "e",
	"í", "i", "ì", "i", "ï", "i", "î", "i",
	"ó", "o", "ò", "o", "ö", "o", "ô", "o", "õ", "o",
	"ú", "u", "ù", "u", "ü", "u", "û", "u",
	"ñ", "n", "ç", "c",
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// normalizeTitle lowercases, folds accents and collapses every non
// alphanumeric run to a single space, yielding a space-delimited token string
// suitable for substring/token matching.
func normalizeTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = accentFolder.Replace(s)
	s = nonAlnum.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// titleStopwords are low-signal words skipped during token matching so a
// release that drops "the"/"a"/"of" still matches. Kept small and
// English/Spanish-leaning (the app's two locales).
var titleStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "and": true,
	"el": true, "la": true, "los": true, "las": true, "de": true,
	"y": true, "un": true, "una": true,
}

// significantTokens returns the meaningful tokens of a normalized title: it
// drops stopwords and bare years, but never returns empty — if everything was
// filtered (e.g. a title that is only stopwords/a year) it falls back to all
// tokens so matching still has something to assert.
func significantTokens(normalized string) []string {
	all := strings.Fields(normalized)
	out := make([]string, 0, len(all))
	for _, t := range all {
		if titleStopwords[t] || isYearToken(t) {
			continue
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return all
	}
	return out
}

// containsToken reports whether tok appears as a whole token in the
// space-delimited normalized title.
func containsToken(normalizedTitle, tok string) bool {
	for _, t := range strings.Fields(normalizedTitle) {
		if t == tok {
			return true
		}
	}
	return false
}

func isYearToken(t string) bool {
	if len(t) != 4 {
		return false
	}
	n, err := strconv.Atoi(t)
	return err == nil && n >= 1900 && n <= 2099
}

var yearRe = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)

// yearsIn extracts plausible 4-digit years (1900-2099) from a raw title.
func yearsIn(title string) []int {
	matches := yearRe.FindAllString(title, -1)
	out := make([]int, 0, len(matches))
	for _, m := range matches {
		if n, err := strconv.Atoi(m); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func anyYearWithin(years []int, want, tol int) bool {
	for _, y := range years {
		d := y - want
		if d < 0 {
			d = -d
		}
		if d <= tol {
			return true
		}
	}
	return false
}

// containsEpisode reports whether a release title carries the SxxEyy marker
// (e.g. "S01E02", "s1e2") or the season-pack marker ("S01", "Season 1") for
// the requested season/episode.
func containsEpisode(title string, season, episode int) bool {
	t := strings.ToLower(title)
	patterns := []string{
		fmt.Sprintf("s%02de%02d", season, episode),
		fmt.Sprintf("s%de%d", season, episode),
		fmt.Sprintf("%dx%02d", season, episode),
		fmt.Sprintf("%dx%d", season, episode),
	}
	for _, p := range patterns {
		if strings.Contains(t, p) {
			return true
		}
	}
	// Season pack (no explicit episode in the title) still serves the episode.
	seasonPacks := []string{
		fmt.Sprintf("s%02d", season),
		fmt.Sprintf("season %d", season),
		fmt.Sprintf("temporada %d", season),
	}
	for _, p := range seasonPacks {
		if strings.Contains(t, p) && !hasOtherEpisodeMarker(t, season, episode) {
			return true
		}
	}
	return false
}

// episodeMarkerRe finds any SxxEyy marker so we can tell a season pack
// ("S01") apart from a different specific episode ("S01E05" when we want E02).
var episodeMarkerRe = regexp.MustCompile(`(?i)s(\d{1,2})e(\d{1,2})`)

// hasOtherEpisodeMarker reports whether the title pins a DIFFERENT episode
// than the requested one — in which case a season-pack match must not apply.
func hasOtherEpisodeMarker(loweredTitle string, season, episode int) bool {
	for _, m := range episodeMarkerRe.FindAllStringSubmatch(loweredTitle, -1) {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		if s == season && e != episode {
			return true
		}
	}
	return false
}
