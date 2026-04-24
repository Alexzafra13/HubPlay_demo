package iptv

// Channel ↔ XMLTV matcher. Takes a parsed XMLTV feed's channel
// reference (id + display-name aliases) and resolves it to one of
// our hub channels. Built around a per-library index that exact-
// matches cover in O(1) and the fuzzy fallback walks in O(N).
//
// The lookups the matcher consults, in order:
//
//  1. Exact tvg-id → channel.
//  2. Exact tvg-id via any XMLTV display-name (some feeds ship
//     tvg-id as a display-name alias).
//  3. Normalised name variant of the XMLTV channel id.
//  4. Normalised name variant of any XMLTV display-name.
//  5. Alias-folded name variant of any XMLTV display-name
//     (see epg_aliases.go).
//  6. Bare-integer display-name → channel.Number, when the M3U
//     carried explicit channel numbers (not the positional
//     fallback from assignNumber).
//  7. Fuzzy Levenshtein against the stripped-name pool, accepted
//     only below a strict distance ratio.
//
// Every earlier step is strictly stricter than the later ones —
// we return the first hit. Fuzzy is last so a typo'd alias never
// beats a clean exact match.

import (
	"regexp"
	"strconv"
	"strings"

	"hubplay/internal/db"
)

// channelIndex is the read-side lookup table the matcher walks.
// Built once per library at the start of a refresh (all XMLTV
// sources share the same index). Exact maps cover the hot path;
// the fuzzy slice is only walked as a last resort.
type channelIndex struct {
	// tvgMap: exact tvg-id → channel id.
	tvgMap map[string]string
	// nameMap: normalised name variant (or alias-folded form) →
	// channel id. Populated from nameVariants(channel.Name) plus
	// canonicalize() of each variant.
	nameMap map[string]string
	// numberMap: explicit, library-unique channel number →
	// channel id. Empty when the library's numbers look purely
	// positional (assignNumber fallback) — see buildChannelIndex.
	numberMap map[int]string
	// fuzzyPool: the stripped (quality / suffix removed) variant
	// of each channel name, deduplicated. Walked by the fuzzy
	// fallback. Kept parallel with fuzzyIDs to avoid a second map.
	fuzzyPool []string
	fuzzyIDs  []string
}

// buildChannelIndex prepares the per-library lookup used by every
// subsequent matchChannel call. Iterates the library's channels
// once and extracts:
//
//   - tvg-id direct lookup,
//   - every nameVariants() result + its alias-folded canonical,
//   - channel numbers (only if they don't look positional),
//   - the stripped base variant for fuzzy fallback.
func buildChannelIndex(channels []*db.Channel) *channelIndex {
	idx := &channelIndex{
		tvgMap:    make(map[string]string, len(channels)),
		nameMap:   make(map[string]string, len(channels)*3),
		numberMap: make(map[int]string),
	}

	// Heuristic: if channel.Number == position+1 for every entry,
	// the M3U importer filled numbers in positionally via
	// assignNumber() and they carry no semantic meaning. Skip the
	// number map entirely in that case — matching "5" against
	// channel-at-position-5 would create a torrent of false hits.
	useNumbers := false
	for i, ch := range channels {
		if ch.Number != i+1 {
			useNumbers = true
			break
		}
	}

	// Second pass: count numbers so we can drop collisions (the
	// same number assigned to two channels produces ambiguity the
	// matcher can't safely resolve).
	numberCounts := make(map[int]int, len(channels))
	if useNumbers {
		for _, ch := range channels {
			if ch.Number > 0 {
				numberCounts[ch.Number]++
			}
		}
	}

	for _, ch := range channels {
		if ch.TvgID != "" {
			idx.tvgMap[ch.TvgID] = ch.ID
		}
		if useNumbers && ch.Number > 0 && numberCounts[ch.Number] == 1 {
			idx.numberMap[ch.Number] = ch.ID
		}

		variants := nameVariants(ch.Name)
		for _, v := range variants {
			if _, exists := idx.nameMap[v]; !exists {
				idx.nameMap[v] = ch.ID
			}
			if c := canonicalize(v); c != v {
				if _, exists := idx.nameMap[c]; !exists {
					idx.nameMap[c] = ch.ID
				}
			}
		}

		// Fuzzy pool gets only the most-stripped variant — it's
		// the cleanest signal and keeps the pool small. Guard
		// against stems ≤4 chars where a 1-edit distance is too
		// loose (e.g. "ten" ↔ "ten4").
		if len(variants) == 0 {
			continue
		}
		base := variants[len(variants)-1]
		if len([]rune(base)) < 5 {
			continue
		}
		if _, seen := idx.nameMap[base]; seen {
			// Cheap dedupe: if the base is already in nameMap
			// as this channel's entry, avoid double-adding to
			// fuzzyPool under a different (same) channel id.
			// Not strictly required (fuzzy walks all entries)
			// but keeps the pool tight.
			if idx.nameMap[base] == ch.ID {
				alreadyPooled := false
				for _, p := range idx.fuzzyPool {
					if p == base {
						alreadyPooled = true
						break
					}
				}
				if alreadyPooled {
					continue
				}
			}
		}
		idx.fuzzyPool = append(idx.fuzzyPool, base)
		idx.fuzzyIDs = append(idx.fuzzyIDs, ch.ID)
	}
	return idx
}

// matchChannel joins one XMLTV channel reference (its id plus all
// display-name aliases, as published in the <channel> element) to
// one of our hub channels. See the channelIndex doc comment for
// the lookup order.
func matchChannel(epgChannelID string, xmltvDisplayNames []string, idx *channelIndex) string {
	// 1. Exact tvg-id.
	if id, ok := idx.tvgMap[epgChannelID]; ok {
		return id
	}
	// 2. Display-name exposed as a tvg-id value.
	for _, dn := range xmltvDisplayNames {
		if id, ok := idx.tvgMap[dn]; ok {
			return id
		}
	}
	// 3. Name variants of the XMLTV channel id itself.
	for _, v := range nameVariants(epgChannelID) {
		if id, ok := idx.nameMap[v]; ok {
			return id
		}
		if c := canonicalize(v); c != v {
			if id, ok := idx.nameMap[c]; ok {
				return id
			}
		}
	}
	// 4 + 5. Name variants of every display-name, plus their
	// alias-folded canonicals.
	for _, dn := range xmltvDisplayNames {
		for _, v := range nameVariants(dn) {
			if id, ok := idx.nameMap[v]; ok {
				return id
			}
			if c := canonicalize(v); c != v {
				if id, ok := idx.nameMap[c]; ok {
					return id
				}
			}
		}
	}
	// 6. Channel-number match: some feeds ship the dial number as
	// a display-name ("501", "8"). Only consults the number map
	// when it's populated (i.e. the M3U carried real numbers).
	if len(idx.numberMap) > 0 {
		for _, dn := range xmltvDisplayNames {
			trimmed := strings.TrimSpace(dn)
			if n, err := strconv.Atoi(trimmed); err == nil && n > 0 {
				if id, ok := idx.numberMap[n]; ok {
					return id
				}
			}
		}
	}
	// 7. Fuzzy Levenshtein fallback over stripped name variants.
	if len(idx.fuzzyPool) > 0 {
		if id := fuzzyMatch(epgChannelID, xmltvDisplayNames, idx); id != "" {
			return id
		}
	}
	return ""
}

// fuzzyMatch walks every stripped-base variant from every
// display-name candidate against the library's fuzzy pool and
// returns the closest hit below the distance threshold. Ties
// (two channels equally close) fail closed — we'd rather lose a
// match than bind to the wrong one.
func fuzzyMatch(epgChannelID string, xmltvDisplayNames []string, idx *channelIndex) string {
	// Collect all candidate stems to compare against. Dedupe so
	// repeated aliases don't multiply the cost.
	seen := make(map[string]struct{}, len(xmltvDisplayNames)+1)
	var candidates []string
	for _, v := range nameVariants(epgChannelID) {
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		candidates = append(candidates, v)
	}
	for _, dn := range xmltvDisplayNames {
		for _, v := range nameVariants(dn) {
			if _, dup := seen[v]; dup {
				continue
			}
			seen[v] = struct{}{}
			candidates = append(candidates, v)
		}
	}

	bestID := ""
	bestDist := -1
	bestCand := ""
	ambiguous := false
	for _, cand := range candidates {
		// Too short → Levenshtein is too permissive. 5 runes is
		// enough to meaningfully reject vs accept.
		if len([]rune(cand)) < 5 {
			continue
		}
		for i, pool := range idx.fuzzyPool {
			// Length-based pruning: if the length delta already
			// exceeds our distance budget for the shorter side,
			// they cannot satisfy the ratio test.
			if absDiff(len(cand), len(pool)) > maxFuzzyDistance(cand, pool) {
				continue
			}
			d := levenshtein(cand, pool)
			if !acceptFuzzy(cand, pool, d) {
				continue
			}
			if bestDist == -1 || d < bestDist {
				bestDist = d
				bestID = idx.fuzzyIDs[i]
				bestCand = cand
				ambiguous = false
			} else if d == bestDist && idx.fuzzyIDs[i] != bestID {
				// Two different channels equally close — refuse
				// to guess.
				ambiguous = true
			}
		}
	}
	if ambiguous {
		return ""
	}
	_ = bestCand
	return bestID
}

// acceptFuzzy enforces the Levenshtein threshold: distance must
// be ≤ 15 % of the longer string, and ≤ 3 edits in absolute
// terms. Strings shorter than 5 runes bail earlier.
func acceptFuzzy(a, b string, d int) bool {
	if d == 0 {
		// Exact would have matched earlier via nameMap; reaching
		// here with d=0 means the base variant wasn't registered
		// (e.g. dedupe collision). Accept it.
		return true
	}
	longer := len([]rune(a))
	if l := len([]rune(b)); l > longer {
		longer = l
	}
	if longer < 5 {
		return false
	}
	if d > 3 {
		return false
	}
	// distance / longer <= 0.15 using integer math.
	return d*100 <= longer*15
}

// maxFuzzyDistance returns the distance budget the length-delta
// pruner uses. Matches acceptFuzzy: 15 % of the longer side or
// 3, whichever is lower.
func maxFuzzyDistance(a, b string) int {
	longer := len([]rune(a))
	if l := len([]rune(b)); l > longer {
		longer = l
	}
	budget := longer * 15 / 100
	if budget > 3 {
		budget = 3
	}
	if budget < 1 {
		budget = 1
	}
	return budget
}

// levenshtein returns the edit distance between a and b using a
// two-row rolling buffer (O(len(a)*len(b)) time, O(min) space).
// Operates on runes so non-ASCII characters — rare after
// diacritic folding, but possible — count as single edits.
func levenshtein(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	// Ensure b is the shorter so the rolling buffer is smaller.
	if len(ar) < len(br) {
		ar, br = br, ar
	}
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min3(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// qualityRE matches the quality / resolution / bitrate suffixes
// that iptv-org and other sources routinely append to channel
// names. Kept as a list of alternations rather than a single
// regex so we can extend it with provider-specific noise
// (e.g. "[Geo-blocked]", "[Not 24/7]").
var qualityRE = regexp.MustCompile(
	`(?i)\s*(?:\[[^\]]*\]|\([^)]*\)|\b(?:uhd|fhd|hd|sd|4k|8k|1080p?|720p?|576p?|480p?|360p?|240p?|backup|alt)\b)`,
)

// nameVariants returns a list of lowercased, accent-folded
// strings that should all match the same channel. For "La 1
// (1080p) [Geo-blocked]" it yields ["la 1 (1080p) [geo-
// blocked]", "la 1"]. The fully-stripped variant is what
// usually matches EPG display-names.
//
// Whitespace is always collapsed in both variants: iptv-org
// feeds routinely carry doubled or trailing spaces ("  Canal
// Sur  "), and treating those as distinct from the cleaned form
// would create a spurious second variant that never matches
// anything real.
func nameVariants(name string) []string {
	base := strings.ToLower(strings.TrimSpace(name))
	if base == "" {
		return nil
	}
	folded := diacriticFolder.Replace(base)
	folded = strings.Join(strings.Fields(folded), " ")
	variants := []string{folded}

	stripped := strings.TrimSpace(qualityRE.ReplaceAllString(folded, " "))
	stripped = strings.Join(strings.Fields(stripped), " ")
	if stripped != "" && stripped != folded {
		variants = append(variants, stripped)
	}
	return variants
}
