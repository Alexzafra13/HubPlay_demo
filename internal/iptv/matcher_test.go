package iptv

import "testing"

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
		{"ESPN", []string{"espn"}},                             // no stripping, single variant
		{"", nil},                                              // empty input
		{"  Canal  Sur  ", []string{"canal sur"}},              // trim + lowercased + whitespace collapsed
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

func TestMatchChannel(t *testing.T) {
	t.Parallel()

	// Our library (iptv-org style): tvg-ids are coded with suffixes, names
	// carry quality/resolution.
	tvgMap := map[string]string{
		"La1.es@HD":                "ch-la1",
		"3CatCameresdeltemps.es@SD": "ch-3cat",
	}
	nameMap := map[string]string{}
	addAll := func(name, id string) {
		for _, v := range nameVariants(name) {
			nameMap[v] = id
		}
	}
	addAll("La 1 (1080p)", "ch-la1")
	addAll("3Cat Càmeres del temps (1080p) [Geo-blocked]", "ch-3cat")
	addAll("Antena 3 HD", "ch-a3")

	t.Run("exact tvg-id wins", func(t *testing.T) {
		got := matchChannel("La1.es@HD", nil, tvgMap, nameMap)
		if got != "ch-la1" {
			t.Errorf("got %q, want ch-la1", got)
		}
	})

	t.Run("davidmuma-style EPG id matches by display-name after quality strip", func(t *testing.T) {
		// EPG channel ID = "La 1 HD"; our name = "La 1 (1080p)".
		// Neither side matches literally; stripping HD/1080p both yield "la 1".
		got := matchChannel("La 1 HD", []string{"La 1 HD", "La 1", "La 1 SD"}, tvgMap, nameMap)
		if got != "ch-la1" {
			t.Errorf("got %q, want ch-la1", got)
		}
	})

	t.Run("accent-folded match", func(t *testing.T) {
		got := matchChannel("3CatInfo Catalunya", []string{"3Cat Cameres del temps"}, tvgMap, nameMap)
		if got != "ch-3cat" {
			t.Errorf("got %q, want ch-3cat", got)
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		got := matchChannel("CNN USA", []string{"CNN", "CNN International"}, tvgMap, nameMap)
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("xmltv display-name matches tvg-id directly", func(t *testing.T) {
		// Some EPGs expose the tvg-id as a display-name alias.
		got := matchChannel("doesnt-matter", []string{"La1.es@HD"}, tvgMap, nameMap)
		if got != "ch-la1" {
			t.Errorf("got %q, want ch-la1", got)
		}
	})
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
