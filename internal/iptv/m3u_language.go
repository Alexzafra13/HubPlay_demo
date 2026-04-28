package iptv

import (
	"regexp"
	"strings"
)

// Language filtering for M3U import.
//
// Lists from Xtream-Codes providers routinely bundle 20–200 k entries
// across all the languages the operator carries. A user that only
// wants Spanish channels has historically had to import everything
// and live with a 5 MB JSON payload + a frontend that chokes on the
// reconciliation. This module keeps only the channels whose language
// signals match a user-chosen allowlist.
//
// Matching strategy — four cascaded heuristics, each more liberal
// than the previous, evaluated in order. The first one to produce a
// hit wins (so a clean tvg-language tag isn't second-guessed by a
// noisy group-title). When NO signal is present the channel is
// allowed through — the heuristic is a "deny only when sure" filter,
// not a strict whitelist, because dropping channels with zero
// metadata would be too aggressive on poorly-tagged feeds.
//
//  1. tvg-language attribute (the only standards-track signal).
//  2. tvg-country attribute (country-code → language proxy: a
//     channel tagged ES is overwhelmingly Spanish).
//  3. group-title keyword (Threadfin/xTeVe approach: matches
//     "Spain", "Latino HD", "ES |", "ES:" inside the group label).
//  4. Channel-name prefix patterns: "[ES] CanalSur", "(ES) BBC",
//     "ES | TVE 1", "ES: …", "ES - …", "ES. …".
//
// Returning true means "keep this channel", false means "drop it".
// `allowed` is the parsed allowlist (already lowercase ISO codes);
// callers that want the no-filter behaviour pass nil/empty, which
// short-circuits to true.

// MatchesLanguageFilter reports whether ch should survive an import
// that allows the languages in `allowed`. See file-level doc for the
// matching cascade.
func MatchesLanguageFilter(ch M3UChannel, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	allowSet := make(map[string]struct{}, len(allowed))
	for _, c := range allowed {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		allowSet[c] = struct{}{}
	}
	if len(allowSet) == 0 {
		// Caller passed only blank entries — degrade to no-filter
		// rather than dropping every channel.
		return true
	}

	hadSignal := false

	// 1. tvg-language: split on commas/spaces; any one match wins.
	//    Some feeds set "Spanish; English" or "es,en" — accept both.
	if ch.Language != "" {
		hadSignal = true
		for _, code := range splitLanguageTokens(ch.Language) {
			if isAllowed(code, allowSet) {
				return true
			}
		}
	}

	// 2. tvg-country → primary language. ISO 3166 → 639-1 mapping
	//    is intentionally narrow: only the cases where ONE language
	//    is unambiguous. Multi-language countries (CH, CA, BE, ...)
	//    fall through to the next heuristic.
	if ch.Country != "" {
		hadSignal = true
		for _, raw := range splitLanguageTokens(ch.Country) {
			if lang, ok := countryToLanguage[raw]; ok {
				if _, allowedLang := allowSet[lang]; allowedLang {
					return true
				}
			}
		}
	}

	// 3. group-title keyword.
	if ch.GroupName != "" {
		hadSignal = true
		gt := strings.ToLower(ch.GroupName)
		for code := range allowSet {
			if matchesGroupKeyword(gt, code) {
				return true
			}
		}
	}

	// 4. Name prefix.
	if ch.Name != "" {
		if hit, code := extractNamePrefixCode(ch.Name); hit {
			hadSignal = true
			if _, ok := allowSet[code]; ok {
				return true
			}
		}
	}

	// No signal at all: allow through. Dropping unsignalled channels
	// would lose untagged content from feeds that don't bother with
	// metadata — too aggressive for the typical IPTV provider.
	return !hadSignal
}

// splitLanguageTokens splits "Spanish, English" / "es;en" /
// "es / en" into lowercase trimmed tokens. Keeps tokens of any
// length so the caller can map "spanish" → "es" downstream.
func splitLanguageTokens(s string) []string {
	if s == "" {
		return nil
	}
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r == ',' || r == ';' || r == '/' || r == '|' || r == ' ' || r == '\t'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// isAllowed reports whether `token` represents a language the user
// allowed. It accepts the ISO 639-1 code directly OR a human-readable
// English name ("spanish", "english") via languageNameToCode.
func isAllowed(token string, allowSet map[string]struct{}) bool {
	if token == "" {
		return false
	}
	if _, ok := allowSet[token]; ok {
		return true
	}
	if code, ok := languageNameToCode[token]; ok {
		if _, allowed := allowSet[code]; allowed {
			return true
		}
	}
	return false
}

// matchesGroupKeyword returns true when the lowercase group title
// `gt` carries an unmistakable signal for the language code `code`.
// Patterns are tuned to avoid false positives:
//   - "es " or " es" is too aggressive (matches "Best of HD"); we
//     instead anchor at start, end, or surrounded by word boundaries
//     adjacent to brackets/pipes.
//   - The full English name ("spanish") and country names ("spain",
//     "españa") are accepted directly.
func matchesGroupKeyword(gt, code string) bool {
	if patterns, ok := groupKeywordPatterns[code]; ok {
		for _, p := range patterns {
			if p.MatchString(gt) {
				return true
			}
		}
	}
	return false
}

// extractNamePrefixCode parses a channel name and, if it starts with
// one of the standard country/language tag patterns, returns the
// lowercase code. Examples (returns "es" for all):
//
//	"[ES] CanalSur"
//	"(ES) BBC"
//	"ES | TVE 1"
//	"ES: La Sexta"
//	"ES - Antena 3"
//	"ES. Telecinco"
func extractNamePrefixCode(name string) (bool, string) {
	if name == "" {
		return false, ""
	}
	if m := namePrefixRE.FindStringSubmatch(name); m != nil {
		// Group 1 is the bracketed/parenthesised form, Group 2 is
		// the bare-prefix form. Whichever matched is non-empty.
		code := m[1]
		if code == "" {
			code = m[2]
		}
		return true, strings.ToLower(code)
	}
	return false, ""
}

// namePrefixRE matches the standard country/language tag at the
// start of a channel name. Two alternatives:
//
//	^[\[(] ([A-Za-z]{2,3}) [\])]   →  "[ES] …" / "(en) …"
//	^([A-Za-z]{2,3}) [|:.\-] \s    →  "ES | …" / "fr - …"
//
// Compiled once; tested in m3u_language_test.go.
var namePrefixRE = regexp.MustCompile(`^\s*(?:[\[\(]([A-Za-z]{2,3})[\]\)]|([A-Za-z]{2,3})\s*[|:.\-]\s)`)

// countryToLanguage maps ISO 3166-1 alpha-2 codes (lowercase) to the
// dominant ISO 639-1 language code. ONLY unambiguous mappings are
// listed — multi-language countries (ch, ca, be, ng, in, …) are
// excluded so the heuristic doesn't punish the long tail.
var countryToLanguage = map[string]string{
	"es": "es", "mx": "es", "ar": "es", "co": "es", "cl": "es",
	"pe": "es", "ve": "es", "uy": "es", "py": "es", "bo": "es",
	"do": "es", "cu": "es", "ec": "es", "gt": "es", "hn": "es",
	"ni": "es", "pa": "es", "sv": "es", "cr": "es", "pr": "es",
	"us": "en", "uk": "en", "gb": "en", "ie": "en", "au": "en",
	"nz": "en", "fr": "fr", "de": "de", "at": "de",
	"it": "it", "pt": "pt", "br": "pt", "ru": "ru",
	"jp": "ja", "kr": "ko", "tr": "tr", "pl": "pl",
	"nl": "nl", "se": "sv", "no": "no", "dk": "da",
	"fi": "fi", "gr": "el", "cz": "cs", "hu": "hu", "ro": "ro",
}

// languageNameToCode maps the English name of a language to its
// ISO 639-1 code, lowercased. Used by tvg-language tokens that come
// in human form ("Spanish", "English"). Keep narrow — exotic names
// belong on the fail-through path, not in this table.
var languageNameToCode = map[string]string{
	"spanish":    "es",
	"español":    "es", // common in feeds tagged in Spanish
	"english":    "en",
	"french":     "fr",
	"français":   "fr",
	"german":     "de",
	"deutsch":    "de",
	"italian":    "it",
	"italiano":   "it",
	"portuguese": "pt",
	"português":  "pt",
	"russian":    "ru",
	"japanese":   "ja",
	"korean":     "ko",
	"turkish":    "tr",
	"polish":     "pl",
	"dutch":      "nl",
	"swedish":    "sv",
	"norwegian":  "no",
	"danish":     "da",
	"finnish":    "fi",
	"greek":      "el",
	"czech":      "cs",
	"hungarian":  "hu",
	"romanian":   "ro",
	"arabic":     "ar",
	"chinese":    "zh",
	"hindi":      "hi",
}

// groupKeywordPatterns — for each language code, a set of compiled
// regexes that match unambiguous tokens inside a lowercase group
// title. Tuned to avoid the "[Es]" → "best" false positive.
//
// Patterns:
//   - The country name in English and the local language ("spain",
//     "españa", "germany", "deutschland", …) — word-boundary anchored.
//   - The 2-letter code preceded by [ ( | start, followed by ] ) | space:
//     `(?:^|[\[\(\| ])es(?:[\]\)\| ]|$)`.
//   - Common operator prefixes: "es -", "es:", "es |".
var groupKeywordPatterns = map[string][]*regexp.Regexp{
	"es": {
		regexp.MustCompile(`\b(spain|españa|spanish|español|latino|hispano|latinoamerica|méxico|mexico|argentina|colombia)\b`),
		regexp.MustCompile(`(?:^|[\[\(\|\- ])es(?:[\]\)\|\- ]|$)`),
	},
	"en": {
		regexp.MustCompile(`\b(uk|usa|united\s?states|united\s?kingdom|english|america|britain|british)\b`),
		regexp.MustCompile(`(?:^|[\[\(\|\- ])(en|us|uk)(?:[\]\)\|\- ]|$)`),
	},
	"fr": {
		regexp.MustCompile(`\b(france|french|français)\b`),
		regexp.MustCompile(`(?:^|[\[\(\|\- ])fr(?:[\]\)\|\- ]|$)`),
	},
	"de": {
		regexp.MustCompile(`\b(germany|deutschland|german|deutsch|austria|österreich)\b`),
		regexp.MustCompile(`(?:^|[\[\(\|\- ])(de|at)(?:[\]\)\|\- ]|$)`),
	},
	"it": {
		regexp.MustCompile(`\b(italy|italia|italian|italiano)\b`),
		regexp.MustCompile(`(?:^|[\[\(\|\- ])it(?:[\]\)\|\- ]|$)`),
	},
	"pt": {
		regexp.MustCompile(`\b(portugal|portuguese|português|brazil|brasil)\b`),
		regexp.MustCompile(`(?:^|[\[\(\|\- ])(pt|br)(?:[\]\)\|\- ]|$)`),
	},
	"ru": {
		regexp.MustCompile(`\b(russia|russian|россия)\b`),
		regexp.MustCompile(`(?:^|[\[\(\|\- ])ru(?:[\]\)\|\- ]|$)`),
	},
	"ar": {
		regexp.MustCompile(`\b(arab|arabic|saudi|emirates|egypt|morocco)\b`),
		regexp.MustCompile(`(?:^|[\[\(\|\- ])(ar|sa|ae|eg|ma)(?:[\]\)\|\- ]|$)`),
	},
	"tr": {
		regexp.MustCompile(`\b(turkey|turkish|türkiye|türk)\b`),
		regexp.MustCompile(`(?:^|[\[\(\|\- ])tr(?:[\]\)\|\- ]|$)`),
	},
	"pl": {
		regexp.MustCompile(`\b(poland|polish|polska|polski)\b`),
		regexp.MustCompile(`(?:^|[\[\(\|\- ])pl(?:[\]\)\|\- ]|$)`),
	},
	"nl": {
		regexp.MustCompile(`\b(netherlands|dutch|holland)\b`),
		regexp.MustCompile(`(?:^|[\[\(\|\- ])nl(?:[\]\)\|\- ]|$)`),
	},
	"el": {
		regexp.MustCompile(`\b(greece|greek)\b`),
		regexp.MustCompile(`(?:^|[\[\(\|\- ])(gr|el)(?:[\]\)\|\- ]|$)`),
	},
}
