package iptv

import "strings"

// NormalizeGroupTitle cleans an M3U `group-title` attribute so the UI
// only ever sees a single, human-readable category label.
//
// M3U authors regularly pack multiple labels in one field, separated
// by ';', ' | ', or ' / ' — for example "Animation;Kids;Public",
// "News | Talk", "Movies / HD". Without normalisation those strings
// land in the filter chips of every consumer (web, Android TV, …),
// producing a wall of duplicated-but-near-identical categories.
//
// Rules:
//   - empty input         → ""
//   - contains ';'        → first ';' separated token (Xtream / IPTV-Org style)
//   - contains ' | '      → first '|' separated token (with spaces around)
//   - contains ' / '      → first '/' separated token (with spaces around)
//   - otherwise           → trimmed input
//
// Bare commas are preserved deliberately ("Movies, HD" is a single
// label). Slashes WITHOUT spaces on both sides are also preserved
// (URLs may contain them, and "S/N" is a legit short token).
//
// Exported so handlers can defend in depth — calling it on the
// response side also fixes data that was imported before this
// normalisation existed.
func NormalizeGroupTitle(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Try the most-specific (space-padded) separators first so an
	// inline "/" in a URL-ish label doesn't get clipped accidentally.
	for _, sep := range []string{";", " | ", " / "} {
		if idx := strings.Index(s, sep); idx >= 0 {
			return strings.TrimSpace(s[:idx])
		}
	}
	return s
}
