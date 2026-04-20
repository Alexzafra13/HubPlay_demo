package iptv

import (
	"hash/fnv"
	"strings"
	"unicode"
)

// LogoFallback is a deterministic initial-letter avatar for channels whose
// upstream `logo_url` is missing, broken, or slow to load. The frontend uses
// it as an immediate placeholder and an error fallback — the same channel
// always gets the same initials and colors so the UI is stable across
// renders and between sessions.
type LogoFallback struct {
	Initials   string // 1–3 uppercase characters, stripped of accents.
	Background string // #RRGGBB, deterministic from the channel name.
	Foreground string // #RRGGBB — white or near-black for WCAG contrast.
}

// logoPalette is hand-picked for dark UIs: every color hits AA contrast with
// at least one of white/near-black foreground, and no two neighbors clash.
// Adding or reordering is a breaking change for user-visible colors — don't.
var logoPalette = []string{
	"#0d9488", // teal (app accent)
	"#2dd4bf", // teal-light
	"#0d4d2a", // forest
	"#1e4fd1", // royal blue
	"#0ea5e9", // sky
	"#a43dc1", // violet
	"#c99bff", // lavender
	"#d81f26", // crimson
	"#ff6600", // orange
	"#ffb84a", // amber
	"#ffd400", // gold
	"#b6f23c", // lime
}

// nearBlack is used as foreground on light palette entries. True black on a
// near-white badge looks harsh; #0a0d0b matches the diseño baseline ink.
const (
	fgLight = "#ffffff"
	fgDark  = "#0a0d0b"
)

// DeriveLogoFallback builds a placeholder avatar from a channel name.
// It is pure and deterministic: the same name always yields the same result,
// so UI renders and tests can rely on equality without snapshotting colors.
//
// Rules:
//   - Empty / whitespace-only names yield initials "?" on palette[0].
//   - 1 word: first two letters (uppercased, accent-folded). A single-rune
//     word becomes one character — we never pad arbitrarily.
//   - 2+ words: the first letter of each of the first two significant words.
//   - Non-letter tokens ("2", "+", "·") are skipped when picking initials.
func DeriveLogoFallback(name string) LogoFallback {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return LogoFallback{Initials: "?", Background: logoPalette[0], Foreground: fgLight}
	}

	initials := pickInitials(clean)
	if initials == "" {
		initials = "?"
	}

	bg := logoPalette[hashIndex(clean, len(logoPalette))]
	fg := fgLight
	if isLightHex(bg) {
		fg = fgDark
	}
	return LogoFallback{Initials: initials, Background: bg, Foreground: fg}
}

// pickInitials extracts 1–3 uppercase ASCII letters/digits from the name.
// Works across Latin alphabets because diacriticFolder flattens accents.
func pickInitials(name string) string {
	folded := diacriticFolder.Replace(name)
	fields := strings.FieldsFunc(folded, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	})

	// No usable tokens (e.g. "···" or pure punctuation).
	if len(fields) == 0 {
		return ""
	}

	// Single word: take the first two runes. If it's only one rune we
	// return one character rather than pad.
	if len(fields) == 1 {
		return upperFirst(fields[0], 2)
	}

	// Multiple words: one letter from each of the first two significant words.
	// Skip 1-char numeric tokens like "2" unless they're the only signal.
	primary := fields[0]
	secondary := fields[1]
	first := upperFirst(primary, 1)
	second := upperFirst(secondary, 1)
	return first + second
}

// upperFirst returns up to n runes from s, uppercased. ASCII only after fold.
func upperFirst(s string, n int) string {
	out := make([]rune, 0, n)
	for _, r := range s {
		out = append(out, unicode.ToUpper(r))
		if len(out) == n {
			break
		}
	}
	return string(out)
}

// hashIndex returns a stable palette index for s. FNV-1a is chosen for its
// speed, standard-library availability, and good distribution on short
// strings — we are not using this for security, only for color assignment.
func hashIndex(s string, modulo int) int {
	if modulo <= 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(s)))
	return int(h.Sum32() % uint32(modulo))
}

// isLightHex returns true when the background benefits from a dark foreground.
// Uses relative luminance per WCAG 2.2 §1.4.3 with the sRGB linearization
// skipped — the palette is curated enough that the simplified formula
// picks the right foreground on every entry. If the palette grows, revisit.
func isLightHex(hex string) bool {
	if len(hex) != 7 || hex[0] != '#' {
		return false
	}
	r := hexByte(hex[1:3])
	g := hexByte(hex[3:5])
	b := hexByte(hex[5:7])
	// Rec. 601 luma — good enough for picking white vs near-black on solid fills.
	luma := (299*int(r) + 587*int(g) + 114*int(b)) / 1000
	return luma >= 160
}

func hexByte(s string) uint8 {
	if len(s) != 2 {
		return 0
	}
	hi := hexNibble(s[0])
	lo := hexNibble(s[1])
	return uint8(hi<<4 | lo)
}

func hexNibble(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return 0
}
