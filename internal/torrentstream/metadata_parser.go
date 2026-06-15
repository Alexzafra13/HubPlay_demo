package torrentstream

import (
	"regexp"
	"strings"
)

// ParsedMetadata holds attributes inferred from a release title — the
// implicit information indexers encode in the file name (resolution,
// codec, source, languages, cam markers). It is computed once per result
// during normalisation and drives the filter/sort engine.
type ParsedMetadata struct {
	Resolution   string   // "2160p","1440p","1080p","720p","480p",""
	Codec        string   // "HEVC","H.264","AV1","XviD",""
	Source       string   // "Remux","BluRay","WEB-DL","WEBRip","HDTV","DVD",""
	HDR          bool     // HDR / HDR10(+)
	DV           bool     // Dolby Vision
	Languages    []string // normalised tags; ["Original"] when none found
	IsCam        bool     // CAM / TS / Screener / R5 / TC …
	QualityScore int      // computed ranking score
}

// Regexes are compiled once at package load (init) — RE2, case-insensitive
// — so parsing a title is just a handful of matches with no per-call
// compilation cost. langOrder fixes the output order so results are
// deterministic.
var (
	reResolution *regexp.Regexp
	re4K         *regexp.Regexp
	reCodec      *regexp.Regexp
	reSource     *regexp.Regexp
	reHDR        *regexp.Regexp
	reDV         *regexp.Regexp
	reCam        *regexp.Regexp
	reLangs      map[string]*regexp.Regexp
	langOrder    = []string{"Spanish", "Latino", "Dual", "Multi", "VOST", "English", "French"}
)

func init() {
	reResolution = regexp.MustCompile(`(?i)\b(2160p|1440p|1080p|720p|480p)\b`)
	re4K = regexp.MustCompile(`(?i)\b(4k|uhd)\b`)
	reCodec = regexp.MustCompile(`(?i)\b(x265|h\.?265|hevc|x264|h\.?264|av1|xvid|divx)\b`)
	reSource = regexp.MustCompile(`(?i)\b(remux|blu-?ray|bd-?rip|br-?rip|web-?dl|web-?rip|webrip|hdtv|dvd-?rip|dvd)\b`)
	reHDR = regexp.MustCompile(`(?i)\b(hdr10\+?|hdr)\b`)
	reDV = regexp.MustCompile(`(?i)\b(dv|dovi|dolby[\s._-]?vision)\b`)
	// Cam / low-quality source markers. "ts"/"tc" are matched only as
	// standalone tokens to avoid false positives inside other words.
	reCam = regexp.MustCompile(`(?i)\b(hdcam|cam-?rip|cam|hd-?ts|telesync|ts|telecine|tc|screener|scr|dvdscr|r5)\b`)
	reLangs = map[string]*regexp.Regexp{
		"Spanish": regexp.MustCompile(`(?i)\b(spanish|castellano|español|espanol|cast)\b`),
		"Latino":  regexp.MustCompile(`(?i)\b(latino|latin|lat)\b`),
		"Dual":    regexp.MustCompile(`(?i)\b(dual)\b`),
		"Multi":   regexp.MustCompile(`(?i)\b(multi)\b`),
		"VOST":    regexp.MustCompile(`(?i)\b(vost|vose|subtitulado)\b`),
		"English": regexp.MustCompile(`(?i)\b(english|eng|ingles)\b`),
		"French":  regexp.MustCompile(`(?i)\b(truefrench|french|vff|vf|francais)\b`),
	}
}

// parseTitle extracts metadata from a release title.
func parseTitle(title string) ParsedMetadata {
	var m ParsedMetadata

	if r := reResolution.FindString(title); r != "" {
		m.Resolution = strings.ToLower(r)
	} else if re4K.MatchString(title) {
		m.Resolution = "2160p"
	}
	if c := reCodec.FindString(title); c != "" {
		m.Codec = normalizeCodec(c)
	}
	if s := reSource.FindString(title); s != "" {
		m.Source = normalizeSource(s)
	}
	m.HDR = reHDR.MatchString(title)
	m.DV = reDV.MatchString(title)
	m.IsCam = reCam.MatchString(title)

	for _, name := range langOrder {
		if reLangs[name].MatchString(title) {
			m.Languages = append(m.Languages, name)
		}
	}
	if len(m.Languages) == 0 {
		// No explicit tag → assume the original/English track.
		m.Languages = []string{"Original"}
	}

	m.QualityScore = m.computeScore()
	return m
}

func normalizeCodec(c string) string {
	c = strings.ToLower(strings.ReplaceAll(c, ".", ""))
	switch {
	case strings.Contains(c, "265"), strings.Contains(c, "hevc"):
		return "HEVC"
	case strings.Contains(c, "264"):
		return "H.264"
	case strings.Contains(c, "av1"):
		return "AV1"
	case strings.Contains(c, "xvid"), strings.Contains(c, "divx"):
		return "XviD"
	}
	return ""
}

func normalizeSource(s string) string {
	s = strings.ToLower(s)
	switch {
	case strings.Contains(s, "remux"):
		return "Remux"
	case strings.Contains(s, "blu"), strings.HasPrefix(s, "bd"), strings.HasPrefix(s, "br"):
		return "BluRay"
	case strings.Contains(s, "web-dl"), s == "webdl":
		return "WEB-DL"
	case strings.Contains(s, "web"):
		return "WEBRip"
	case strings.Contains(s, "hdtv"):
		return "HDTV"
	case strings.Contains(s, "dvd"):
		return "DVD"
	}
	return ""
}

// computeScore turns the parsed attributes into a single sortable number.
// A cam release gets a heavy negative penalty so it always sinks even if
// it isn't filtered out.
func (m ParsedMetadata) computeScore() int {
	score := 0
	switch m.Resolution {
	case "2160p":
		score += 4000
	case "1440p":
		score += 3500
	case "1080p":
		score += 3000
	case "720p":
		score += 2000
	case "480p":
		score += 1000
	}
	switch m.Source {
	case "Remux":
		score += 500
	case "BluRay":
		score += 400
	case "WEB-DL":
		score += 300
	case "WEBRip":
		score += 200
	case "HDTV":
		score += 100
	case "DVD":
		score += 50
	}
	switch m.Codec {
	case "HEVC":
		score += 150
	case "AV1":
		score += 120
	case "H.264":
		score += 50
	}
	if m.HDR {
		score += 60
	}
	if m.DV {
		score += 60
	}
	if m.IsCam {
		score -= 10000
	}
	return score
}

// qualityBucket maps the parsed resolution (and HDTV source) onto the
// coarse label the existing UI chip shows.
func (m ParsedMetadata) qualityBucket() string {
	switch m.Resolution {
	case "2160p":
		return "4K"
	case "1440p":
		return "1440p"
	case "1080p":
		return "1080p"
	case "720p":
		return "720p"
	case "480p":
		return "480p"
	}
	if m.Source == "HDTV" {
		return "HDTV"
	}
	return ""
}
