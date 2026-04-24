package iptv

import (
	"testing"

	"hubplay/internal/db"
)

func TestNameVariants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want []string
	}{
		{"La 1 HD", []string{"la 1 hd", "la 1"}},
		{"3Cat Càmeres del temps (1080p)", []string{"3cat cameres del temps (1080p)", "3cat cameres del temps"}},
		{"24 Horas Internacional [Geo-blocked]", []string{"24 horas internacional [geo-blocked]", "24 horas internacional"}},
		{"Movistar Plus+ FHD", []string{"movistar plus+ fhd", "movistar plus+"}},
		{"ESPN", []string{"espn"}},              // no stripping, single variant
		{"", nil},                               // empty input
		{"  Canal  Sur  ", []string{"canal sur"}}, // trim + lowercased + whitespace collapsed
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got := nameVariants(tc.in)
			if !equalStrings(got, tc.want) {
				t.Errorf("nameVariants(%q) = %v; want %v", tc.in, got, tc.want)
			}
		})
	}
}

// indexFrom builds a channelIndex from a slice literal. Helper for
// matcher tests so each case declares the library shape right next
// to the assertions.
func indexFrom(channels ...*db.Channel) *channelIndex {
	return buildChannelIndex(channels)
}

func TestMatchChannel_ExactPaths(t *testing.T) {
	t.Parallel()

	idx := indexFrom(
		&db.Channel{ID: "ch-la1", Name: "La 1 (1080p)", TvgID: "La1.es@HD"},
		&db.Channel{ID: "ch-3cat", Name: "3Cat Càmeres del temps (1080p) [Geo-blocked]", TvgID: "3CatCameresdeltemps.es@SD"},
		&db.Channel{ID: "ch-a3", Name: "Antena 3 HD"},
	)

	t.Run("exact tvg-id wins", func(t *testing.T) {
		if got := matchChannel("La1.es@HD", nil, idx); got != "ch-la1" {
			t.Errorf("got %q, want ch-la1", got)
		}
	})

	t.Run("davidmuma-style EPG id matches by display-name after quality strip", func(t *testing.T) {
		got := matchChannel("La 1 HD", []string{"La 1 HD", "La 1", "La 1 SD"}, idx)
		if got != "ch-la1" {
			t.Errorf("got %q, want ch-la1", got)
		}
	})

	t.Run("accent-folded match", func(t *testing.T) {
		got := matchChannel("3CatInfo Catalunya", []string{"3Cat Cameres del temps"}, idx)
		if got != "ch-3cat" {
			t.Errorf("got %q, want ch-3cat", got)
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		got := matchChannel("CNN USA", []string{"CNN", "CNN International"}, idx)
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("xmltv display-name matches tvg-id directly", func(t *testing.T) {
		got := matchChannel("doesnt-matter", []string{"La1.es@HD"}, idx)
		if got != "ch-la1" {
			t.Errorf("got %q, want ch-la1", got)
		}
	})
}

func TestMatchChannel_Aliases(t *testing.T) {
	t.Parallel()

	idx := indexFrom(
		&db.Channel{ID: "ch-la1", Name: "La 1 HD"},
		&db.Channel{ID: "ch-a3", Name: "Antena 3"},
		&db.Channel{ID: "ch-t5", Name: "Telecinco"},
		&db.Channel{ID: "ch-l6", Name: "laSexta"},
		&db.Channel{ID: "ch-mll", Name: "Movistar La Liga"},
	)

	// EPG side spells the digit as a word; index resolves via alias
	// registration at build time ("la uno" ↦ "la 1" canonical).
	t.Run("spelled-out digit (la uno → la 1)", func(t *testing.T) {
		got := matchChannel("La Uno", []string{"La Uno"}, idx)
		if got != "ch-la1" {
			t.Errorf("got %q, want ch-la1", got)
		}
	})

	t.Run("antena tres → antena 3", func(t *testing.T) {
		got := matchChannel("antena.tres", []string{"Antena Tres"}, idx)
		if got != "ch-a3" {
			t.Errorf("got %q, want ch-a3", got)
		}
	})

	// Hub has concatenated form, EPG has spaced variant.
	t.Run("tele cinco ↔ telecinco", func(t *testing.T) {
		got := matchChannel("tele5.es", []string{"Tele Cinco"}, idx)
		if got != "ch-t5" {
			t.Errorf("got %q, want ch-t5", got)
		}
	})

	// Hub has concatenated "laSexta", EPG has "la 6" or "la seis".
	t.Run("la 6 / la seis → lasexta", func(t *testing.T) {
		got := matchChannel("lasexta.es", []string{"La 6"}, idx)
		if got != "ch-l6" {
			t.Errorf("got %q, want ch-l6 via 'la 6' alias", got)
		}
		got = matchChannel("lasexta.es", []string{"La Seis"}, idx)
		if got != "ch-l6" {
			t.Errorf("got %q, want ch-l6 via 'la seis' alias", got)
		}
	})

	t.Run("movistar laliga → movistar la liga", func(t *testing.T) {
		got := matchChannel("m+laliga", []string{"Movistar LaLiga"}, idx)
		if got != "ch-mll" {
			t.Errorf("got %q, want ch-mll", got)
		}
	})

	// Hub has the alias form, EPG has the canonical: we register the
	// canonical at index-build time too.
	t.Run("reverse direction (hub uses alias, EPG canonical)", func(t *testing.T) {
		idxAlias := indexFrom(
			&db.Channel{ID: "ch-la1", Name: "La Uno"},
		)
		got := matchChannel("la1.es", []string{"La 1"}, idxAlias)
		if got != "ch-la1" {
			t.Errorf("got %q, want ch-la1", got)
		}
	})
}

func TestMatchChannel_ChannelNumber(t *testing.T) {
	t.Parallel()

	// Scenario A: positional numbers (assignNumber-filled) — number
	// match MUST NOT register, or every digit-named display-name
	// would bind to whichever channel happens to be at that slot.
	t.Run("positional numbers are ignored", func(t *testing.T) {
		idx := indexFrom(
			&db.Channel{ID: "ch-a", Name: "Channel A", Number: 1},
			&db.Channel{ID: "ch-b", Name: "Channel B", Number: 2},
			&db.Channel{ID: "ch-c", Name: "Channel C", Number: 3},
		)
		if got := matchChannel("chan-2", []string{"2"}, idx); got != "" {
			t.Errorf("positional number should not match; got %q", got)
		}
	})

	// Scenario B: explicit numbers (non-positional) — bare-integer
	// display-name binds by dial number.
	t.Run("explicit number matches bare-integer display-name", func(t *testing.T) {
		idx := indexFrom(
			&db.Channel{ID: "ch-la1", Name: "La 1 HD", Number: 101},
			&db.Channel{ID: "ch-a3", Name: "Antena 3 HD", Number: 205},
			&db.Channel{ID: "ch-t5", Name: "Telecinco", Number: 302},
		)
		got := matchChannel("unknown-id", []string{"205"}, idx)
		if got != "ch-a3" {
			t.Errorf("got %q, want ch-a3 via channel number 205", got)
		}
	})

	// Scenario C: explicit but ambiguous — two channels share the
	// same number, the matcher refuses to bind.
	t.Run("ambiguous numbers are skipped", func(t *testing.T) {
		idx := indexFrom(
			&db.Channel{ID: "ch-x", Name: "X", Number: 500},
			&db.Channel{ID: "ch-y", Name: "Y", Number: 500},
			&db.Channel{ID: "ch-z", Name: "Z", Number: 777}, // forces non-positional
		)
		if got := matchChannel("unknown", []string{"500"}, idx); got != "" {
			t.Errorf("ambiguous number should not bind; got %q", got)
		}
		// Z's unique number still works.
		if got := matchChannel("unknown", []string{"777"}, idx); got != "ch-z" {
			t.Errorf("unique 777 should bind ch-z; got %q", got)
		}
	})
}

func TestMatchChannel_FuzzyFallback(t *testing.T) {
	t.Parallel()

	idx := indexFrom(
		&db.Channel{ID: "ch-la1", Name: "La 1 HD"},
		&db.Channel{ID: "ch-a3", Name: "Antena 3"},
		&db.Channel{ID: "ch-t5", Name: "Telecinco"},
		&db.Channel{ID: "ch-discovery", Name: "Discovery Channel"},
		&db.Channel{ID: "ch-eurosport", Name: "Eurosport 1 HD"},
	)

	t.Run("1-edit typo accepted", func(t *testing.T) {
		// "Teleciinco" (one extra 'i') vs "telecinco"
		got := matchChannel("teleciinco.es", []string{"Teleciinco"}, idx)
		if got != "ch-t5" {
			t.Errorf("got %q, want ch-t5 via fuzzy", got)
		}
	})

	t.Run("2-edit typo in long name accepted", func(t *testing.T) {
		// "Disovery Chanel" vs "discovery channel" — 2 deletions,
		// 17 chars → ratio 2/17 ≈ 0.12, under the 0.15 cap.
		got := matchChannel("disovery", []string{"Disovery Chanel"}, idx)
		if got != "ch-discovery" {
			t.Errorf("got %q, want ch-discovery via fuzzy", got)
		}
	})

	t.Run("too-short stems refuse fuzzy", func(t *testing.T) {
		// "cnn" vs "cin" (both 3 chars) — Levenshtein=1 but both are
		// below the 5-rune minimum. Should NOT match anything.
		shortIdx := indexFrom(
			&db.Channel{ID: "ch-cin", Name: "CIN"},
		)
		got := matchChannel("CNN", []string{"CNN"}, shortIdx)
		if got != "" {
			t.Errorf("short stems should not fuzzy-match; got %q", got)
		}
	})

	t.Run("over-threshold edit distance refused", func(t *testing.T) {
		// "Cuatro" vs "telecinco" — both valid channel names with no
		// shared structure. Must NOT bind.
		got := matchChannel("cuatro.es", []string{"Cuatro"}, idx)
		if got != "" {
			t.Errorf("unrelated channel should not fuzzy-match; got %q", got)
		}
	})

	t.Run("non-ASCII survives folding and still matches fuzzy", func(t *testing.T) {
		// "Movistar Plus+" keeps the "+" after diacritic folding (it's
		// not in the folder table), so both sides carry multi-byte
		// history. The byte-length pruner would miscompute the
		// budget; the rune-based pruner lets this through. Typo in
		// the EPG payload → 1-edit distance, should match.
		mpIdx := indexFrom(
			&db.Channel{ID: "ch-mp", Name: "Movistar Plus+"},
		)
		got := matchChannel("movistar.plus+", []string{"Movistar Pluz+"}, mpIdx)
		if got != "ch-mp" {
			t.Errorf("got %q, want ch-mp via fuzzy over multi-byte", got)
		}
	})

	t.Run("tie between two close channels refuses to guess", func(t *testing.T) {
		// "ABCDEFGH" equidistant from "ABCDEFGX" and "ABCDEFGY" — same
		// distance (1) to two different ids. Matcher refuses.
		tieIdx := indexFrom(
			&db.Channel{ID: "ch-x", Name: "ABCDEFGX"},
			&db.Channel{ID: "ch-y", Name: "ABCDEFGY"},
		)
		got := matchChannel("abcdefgh", []string{"ABCDEFGH"}, tieIdx)
		if got != "" {
			t.Errorf("ambiguous fuzzy should not bind; got %q", got)
		}
	})
}

func TestLevenshtein(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3}, // canonical textbook example
		{"telecinco", "teleciinco", 1},
		{"discovery channel", "disovery chanel", 2},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.a+"_"+tc.b, func(t *testing.T) {
			t.Parallel()
			if got := levenshtein(tc.a, tc.b); got != tc.want {
				t.Errorf("levenshtein(%q,%q)=%d; want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestCanonicalize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"la uno", "la 1"},
		{"tele cinco", "telecinco"},
		{"antena tres", "antena 3"},
		{"la 1", "la 1"}, // no-op when no alias
		{"", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := canonicalize(tc.in); got != tc.want {
				t.Errorf("canonicalize(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
