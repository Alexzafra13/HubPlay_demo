package iptv

import "testing"

func TestNormalizeGroupTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		// ── Pass-through ──────────────────────────────────────────
		{"empty",                  "",                        ""},
		{"whitespace only",        "   ",                     ""},
		{"plain",                  "Cine",                    "Cine"},
		{"plain with spaces",      "  News  ",                "News"},

		// ── Semicolon — the case that drove this refactor ─────────
		{"two tokens semicolon",   "Animation;Kids",          "Animation"},
		{"three tokens semicolon", "Animation;Kids;Public",   "Animation"},
		{"semicolon trailing ws",  "Comedy ;Public",          "Comedy"},
		{"semicolon then space",   "News; Talk",              "News"},

		// ── Space-padded pipe ────────────────────────────────────
		{"pipe with spaces",       "News | Talk",             "News"},
		{"pipe ascii art only",    "News|Talk",               "News|Talk"}, // no spaces → kept whole; safer default
		{"pipe with multi tokens", "Movies | HD | Sports",    "Movies"},

		// ── Space-padded slash ───────────────────────────────────
		{"slash with spaces",      "Movies / HD",             "Movies"},
		{"slash inline",           "S/N Special",             "S/N Special"}, // bare slash kept
		{"url-like fragment",      "feed/url",                "feed/url"},

		// ── Commas preserved (legitimate inside a label) ─────────
		{"comma stays",            "Movies, HD",              "Movies, HD"},

		// ── Edge cases ───────────────────────────────────────────
		{"semicolon at start",     ";Animation",              ""},
		{"only separator",         ";;;",                     ""},
		{"single semicolon",       ";",                       ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeGroupTitle(tc.in); got != tc.want {
				t.Errorf("NormalizeGroupTitle(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
