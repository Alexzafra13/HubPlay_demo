package iptv

import (
	"strings"
	"testing"
)

func TestDeriveLogoFallback_Initials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    string
		initials string
	}{
		{"empty is question mark", "", "?"},
		{"whitespace is question mark", "   ", "?"},
		{"single word takes two letters", "News", "NE"},
		{"two words take first of each", "Real Madrid TV", "RM"},
		{"accent is folded", "Películas", "PE"},
		{"single rune name yields one char", "X", "X"},
		{"punctuation is stripped", "···", "?"},
		{"digits count as letters", "24 Horas", "2H"},
		{"lowercase is uppercased", "discovery channel", "DC"},
		{"multi accent folds", "Música en Español", "ME"},
		{"hyphen splits words", "bein-sports", "BS"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DeriveLogoFallback(tc.input)
			if got.Initials != tc.initials {
				t.Errorf("DeriveLogoFallback(%q).Initials = %q; want %q", tc.input, got.Initials, tc.initials)
			}
			if !strings.HasPrefix(got.Background, "#") || len(got.Background) != 7 {
				t.Errorf("Background %q is not a #RRGGBB hex", got.Background)
			}
			if got.Foreground != fgLight && got.Foreground != fgDark {
				t.Errorf("Foreground %q is neither light nor dark sentinel", got.Foreground)
			}
		})
	}
}

func TestDeriveLogoFallback_IsDeterministic(t *testing.T) {
	t.Parallel()
	name := "3Cat El búnquer"
	first := DeriveLogoFallback(name)
	for i := 0; i < 100; i++ {
		got := DeriveLogoFallback(name)
		if got != first {
			t.Fatalf("non-deterministic at iter %d: %+v vs %+v", i, got, first)
		}
	}
}

func TestDeriveLogoFallback_DifferentNamesUseDifferentColors(t *testing.T) {
	t.Parallel()
	// Not a strict collision test — the palette is small (12) so collisions
	// are expected. But across a realistic sample of channel names we want
	// the hash to spread, not degenerate to one or two colors.
	names := []string{
		"News", "Sports", "Movies", "Music", "Kids", "Discovery",
		"National Geographic", "Real Madrid TV", "DAZN F1", "TCM Clásicos",
		"MTV Hits", "La Sexta", "Antena 3", "Cuatro", "Telecinco",
		"3Cat El búnquer", "Teledeporte", "24 Horas", "CNN", "BBC World",
	}
	seen := map[string]struct{}{}
	for _, n := range names {
		seen[DeriveLogoFallback(n).Background] = struct{}{}
	}
	if len(seen) < 6 {
		t.Fatalf("palette distribution too narrow: only %d distinct colors across %d names", len(seen), len(names))
	}
}

func TestIsLightHex(t *testing.T) {
	t.Parallel()

	cases := []struct {
		hex  string
		want bool
	}{
		{"#ffffff", true},
		{"#000000", false},
		{"#0d9488", false}, // teal
		{"#b6f23c", true},  // lime
		{"#ffd400", true},  // gold
		{"#a43dc1", false}, // violet
		{"#ff6600", false}, // orange (luma ~130, below threshold — foreground is white)
		{"nothex", false},
		{"", false},
	}

	for _, tc := range cases {
		t.Run(tc.hex, func(t *testing.T) {
			t.Parallel()
			if got := isLightHex(tc.hex); got != tc.want {
				t.Errorf("isLightHex(%q) = %v; want %v", tc.hex, got, tc.want)
			}
		})
	}
}
