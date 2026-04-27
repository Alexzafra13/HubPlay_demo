package scanner

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// EpisodeMatch captures what the filename + parent dir of a TV episode
// can tell us before we look at metadata: which series it belongs to
// (just the on-disk name, the matcher fills in the canonical title
// later from TMDb), which season, which episode, and the human-friendly
// episode title if the filename includes one.
//
// `OK == false` means the path doesn't look like an episode at all
// (flat lib root, weird structure) — the caller should fall back to
// the "single item, no parents" code path so the file isn't lost.
type EpisodeMatch struct {
	SeriesName    string
	SeasonNumber  int
	EpisodeNumber int
	EpisodeTitle  string
	OK            bool
}

// Common patterns Plex / Jellyfin / Kodi all parse:
//
//	"S01E05"           — the standard
//	"s01e05"           — case insensitive
//	"S1E5"             — single-digit
//	"1x05" / "01x05"   — alternative notation
//	"S01.E05"          — dotted variant some scrapers emit
//
// Captured: $1 = season, $2 = episode. Anchored to a non-digit boundary
// on either side so we don't gobble part of a year ("2024 1x05" works).
var epPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[^a-z\d])s(\d{1,3})[\.\s_-]?e(\d{1,3})(?:[^a-z\d]|$)`),
	regexp.MustCompile(`(?:^|\D)(\d{1,3})x(\d{1,3})(?:\D|$)`),
}

// Season-only patterns for the parent dir. The episode regex above
// catches season+ep together when both live in the filename, but the
// season number sometimes only lives in the dir name ("Season 03/01.mkv").
var seasonDirPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(?:season|temporada|saison|staffel)[\.\s_-]*(\d{1,3})$`),
	regexp.MustCompile(`(?i)^s(\d{1,3})$`),
}

// titleStripChars are the characters we trim around the extracted
// episode title. Filenames frequently use any of these as separators
// between the SxxExx token and the title proper.
const titleStripChars = " .-_[]()"

// ParseEpisode extracts the episode coordinates from a file path.
//
// `libraryRoot` is the absolute path of the library; we use it to know
// when we've walked off the end (so a file directly at root doesn't
// pretend to belong to a non-existent series).
//
// The expected layout is the de-facto Plex/Jellyfin convention:
//
//	<libRoot>/<Series Name>/<Season N>/<file>.ext
//	<libRoot>/<Series Name>/<file>.ext            (rare, but supported)
//
// In the second form season number is read from the filename when the
// SxxExx pattern is present, and defaults to 1 if only the episode
// number is encoded.
func ParseEpisode(libraryRoot, filePath string) EpisodeMatch {
	rel, err := filepath.Rel(libraryRoot, filePath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return EpisodeMatch{OK: false}
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		// File at lib root — no series dir to read.
		return EpisodeMatch{OK: false}
	}

	// parts[0] is always the immediate child of the lib root. We treat
	// it as the series dir; deeper dirs are season(s).
	seriesDir := parts[0]
	fileName := parts[len(parts)-1]
	// All segments between series and file count as candidates for the
	// season name. In the typical 3-part layout there's exactly one.
	var seasonDirs []string
	if len(parts) >= 3 {
		seasonDirs = parts[1 : len(parts)-1]
	}

	se, ee, titleFromFile, hasSE := extractEpisodeFromFilename(fileName)
	hasSeasonDir := false
	seasonFromDir := 0
	for i := len(seasonDirs) - 1; i >= 0; i-- {
		if n, ok := parseSeasonDir(seasonDirs[i]); ok {
			seasonFromDir = n
			hasSeasonDir = true
			break
		}
	}

	switch {
	case hasSE:
		// Filename has SxxExx. Prefer season-from-dir when present (it
		// disambiguates re-numbered shows like Doctor Who 2005), but
		// fall back to the filename season otherwise.
		if hasSeasonDir {
			se = seasonFromDir
		}
		return EpisodeMatch{
			SeriesName:    seriesDir,
			SeasonNumber:  se,
			EpisodeNumber: ee,
			EpisodeTitle:  titleFromFile,
			OK:            true,
		}
	case hasSeasonDir:
		// Filename has no SxxExx — try to extract just the episode
		// number from the filename (e.g. "01.mkv" inside Season 03).
		if epOnly, ok := extractTrailingEpisodeNumber(fileName); ok {
			return EpisodeMatch{
				SeriesName:    seriesDir,
				SeasonNumber:  seasonFromDir,
				EpisodeNumber: epOnly,
				EpisodeTitle:  cleanTitle(strings.TrimSuffix(fileName, filepath.Ext(fileName))),
				OK:            true,
			}
		}
	}

	return EpisodeMatch{OK: false}
}

// extractEpisodeFromFilename tries every pattern in turn. Returns the
// season, episode, an attempt at the episode title (everything after
// the SxxExx token), and a flag.
func extractEpisodeFromFilename(name string) (season, episode int, title string, ok bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	for _, re := range epPatterns {
		m := re.FindStringSubmatchIndex(base)
		if m == nil {
			continue
		}
		s, _ := strconv.Atoi(base[m[2]:m[3]])
		e, _ := strconv.Atoi(base[m[4]:m[5]])
		// The matched range is m[0]..m[1]; everything after is a
		// candidate episode title. Strip leading separators that the
		// release packager left behind.
		tail := ""
		if m[1] < len(base) {
			tail = cleanTitle(base[m[1]:])
		}
		return s, e, tail, true
	}
	return 0, 0, "", false
}

// extractTrailingEpisodeNumber catches files like "01.mkv" that only
// encode the episode number (season comes from the parent dir).
var trailingNumRE = regexp.MustCompile(`^(\d{1,3})(?:[\.\s_-].*)?$`)

func extractTrailingEpisodeNumber(name string) (int, bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	m := trailingNumRE.FindStringSubmatch(base)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, true
}

// parseSeasonDir reads a season number out of a directory name. Returns
// false when the dir name doesn't match any known pattern (so the
// caller can keep looking up the chain).
func parseSeasonDir(name string) (int, bool) {
	trimmed := strings.TrimSpace(name)
	for _, re := range seasonDirPatterns {
		m := re.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	return 0, false
}

// cleanTitle strips leading/trailing separators and replaces internal
// dots / underscores with spaces (the standard release-naming
// convention uses dots between words: "S01E05.Halloween.Party"). The
// resulting title is what we hand to the user before TMDb metadata
// lands; "" is acceptable when nothing is left after trimming.
func cleanTitle(raw string) string {
	out := strings.TrimLeft(raw, titleStripChars)
	out = strings.TrimRight(out, titleStripChars)
	// Word separators commonly used by release packagers. Replaced
	// inline rather than with a regex because we don't need pattern
	// matching here, just translation.
	out = strings.ReplaceAll(out, ".", " ")
	out = strings.ReplaceAll(out, "_", " ")
	out = strings.Join(strings.Fields(out), " ")
	return out
}
