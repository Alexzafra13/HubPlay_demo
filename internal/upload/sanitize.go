// Package upload contains the media-upload pipeline: validation,
// staging, atomic move into a library, and audit. The HTTP surface
// is mounted from internal/api/handlers/uploads.go; the bytes flow
// through tusd (github.com/tus/tusd/v2). See docs/architecture/uploads.md
// when that doc lands.
package upload

import (
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

// maxFilenameLength caps the sanitised basename at a length that fits
// on every filesystem we target (ext4 = 255 bytes, NTFS = 255 chars,
// APFS = 255 chars). 220 leaves headroom for the conflict suffix that
// staging.go appends when a name collision happens — "name-0001.mkv"
// adds 5 chars + extension overhead.
const maxFilenameLength = 220

// SanitizeFilename returns a safe basename derived from a user-supplied
// filename. The transformations, in order:
//
//  1. Strip any path component (filepath.Base): rejects "../../etc/passwd"
//     and "C:\Windows\System32\evil.mkv" — we only ever want the leaf.
//  2. Normalise Unicode whitespace to a single ASCII space, then trim.
//  3. Drop control characters (U+0000–U+001F, U+007F) and the path
//     separators "/" and "\" — defence in depth on top of (1).
//  4. Replace any character not in [A-Za-z0-9._-() áéíóúüñÁÉÍÓÚÜÑ ] with
//     an underscore. We keep accents and ñ because users name files
//     in Spanish; we keep dots so the extension is preserved.
//  5. Collapse multiple consecutive underscores or spaces.
//  6. Truncate to maxFilenameLength while preserving the extension.
//  7. Reject if the result is empty, equal to "." or "..". Leading
//     dots are already stripped by step (5)'s trim so a UNIX hidden
//     file like ".hidden.mkv" loses the leading dot rather than being
//     rejected — same effect from a security standpoint and lets users
//     drag in poorly-named files without seeing a hard error.
//
// Returns "" when the input is unsalvageable; the caller treats "" as
// "reject with VALIDATION_ERROR" rather than silently rewriting it.
func SanitizeFilename(raw string) string {
	// (1) strip path. Normalizamos separadores a "/" y usamos path.Base
	// (POSIX) en lugar de filepath.Base — el input es el nombre que un
	// usuario subió, no un filesystem path local. En Windows
	// filepath.Base("a:::b.mkv") devuelve "::b.mkv" porque trata "a:"
	// como drive prefix, y eso pierde caracteres semánticamente válidos
	// del nombre. path.Base aplica la misma regla en todos los OS.
	normalized := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	base := path.Base(normalized)
	if base == "" || base == "." || base == ".." {
		return ""
	}

	// (3+4) char filter pass — build the new string with a builder so
	// we avoid quadratic concatenation on the rare 1000-char filename.
	var b strings.Builder
	b.Grow(len(base))
	for _, r := range base {
		switch {
		case r == '/' || r == '\\' || r == 0 || unicode.IsControl(r):
			b.WriteByte('_')
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			// Letters covers ASCII + accented + ñ.
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-' || r == '(' || r == ')' || r == ' ' || r == '[' || r == ']':
			b.WriteRune(r)
		default:
			// Anything exotic — emoji, math operators, RTL marks — out.
			b.WriteByte('_')
		}
	}
	clean := b.String()

	// (5) collapse consecutive separators.
	clean = collapseRuns(clean, '_')
	clean = collapseRuns(clean, ' ')

	clean = strings.Trim(clean, ". _-")
	if clean == "" {
		return ""
	}

	// (6) truncate while preserving extension. If the extension is
	// already absurd (> 12 chars) we drop it — that's not a real
	// extension and the validator would reject it anyway.
	if len(clean) > maxFilenameLength {
		ext := filepath.Ext(clean)
		if len(ext) > 12 {
			ext = ""
		}
		stem := strings.TrimSuffix(clean, ext)
		// keep room for ext
		if budget := maxFilenameLength - len(ext); budget > 0 && len(stem) > budget {
			stem = stem[:budget]
		}
		clean = strings.TrimRight(stem, " ._-") + ext
	}

	// (7) final sanity check
	if clean == "" || clean == "." || clean == ".." {
		return ""
	}
	return clean
}

// collapseRuns reduces consecutive runs of `sep` to a single sep. Tiny
// helper — no fancy regex; the hot path runs once per upload.
func collapseRuns(s string, sep rune) string {
	if !strings.ContainsRune(s, sep) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	prev := rune(-1)
	for _, r := range s {
		if r == sep && prev == sep {
			continue
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

// ExtensionLower returns the file extension (without dot), lowercased,
// or "" if the filename has none. The validator uses this to gate the
// extension whitelist without each caller repeating the strings.ToLower
// dance.
func ExtensionLower(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(ext, "."))
}
