package iptv

import "testing"

func TestCanonical(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		group string
		want  Category
	}{
		// Empty / fallback
		{"empty string is general", "", CategoryGeneral},
		{"whitespace only is general", "   ", CategoryGeneral},
		{"unknown group is general", "Some Random Channel", CategoryGeneral},

		// News
		{"plain news", "News", CategoryNews},
		{"spanish noticias", "Noticias España", CategoryNews},
		{"catalan informatius", "Informatius", CategoryNews},
		{"24h tag", "24h España", CategoryNews},
		{"cnn", "CNN International", CategoryNews},

		// Sports — priority wins over news
		{"plain sports", "Sports", CategorySports},
		{"spanish deportes", "Deportes HD", CategorySports},
		{"sports news is sports", "Sports News", CategorySports},
		{"laliga", "LaLiga TV", CategorySports},
		{"dazn f1", "DAZN F1", CategorySports},
		{"eurosport", "Eurosport 2", CategorySports},

		// Movies
		{"plain movies", "Movies HD", CategoryMovies},
		{"spanish peliculas", "Películas", CategoryMovies},
		{"cine clasico", "Cine Clásico", CategoryMovies},
		{"tcm", "TCM Clásicos", CategoryMovies},

		// Music
		{"music tag", "MUSIC", CategoryMusic},
		{"mtv", "MTV Hits", CategoryMusic},
		{"los 40", "Los 40 Principales", CategoryMusic},
		{"cadena dial", "Cadena Dial", CategoryMusic},

		// Kids before entertainment
		{"kids", "KIDS", CategoryKids},
		{"infantil", "Canal Infantil", CategoryKids},
		{"disney", "Disney Channel", CategoryKids},
		{"cartoon entertainment", "Cartoon Entertainment", CategoryKids},

		// Documentaries before culture
		{"documental", "Documental HD", CategoryDocumentaries},
		{"nat geo", "Nat Geo Wild", CategoryDocumentaries},
		{"national geographic", "National Geographic", CategoryDocumentaries},
		{"history docs", "History Docs", CategoryDocumentaries},

		// Case / accent insensitivity
		{"uppercase", "PELÍCULAS", CategoryMovies},
		{"mixed case accent", "Música en Directo", CategoryMusic},
		{"trailing whitespace", "  News  ", CategoryNews},

		// Adult always wins
		{"adult variety", "Adult Variety Show", CategoryAdult},
		{"xxx", "XXX HD", CategoryAdult},

		// Religion
		{"catholic channel", "Catholic Channel", CategoryReligion},
		{"ewtn", "EWTN España", CategoryReligion},

		// International as last resort
		{"world plain", "World Channel", CategoryInternational},
		{"internacional", "Canal Internacional", CategoryInternational},

		// Entertainment
		{"reality show", "Reality Show HD", CategoryEntertainment},
		{"comedia", "Comedia Central", CategoryEntertainment},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Canonical(tc.group)
			if got != tc.want {
				t.Fatalf("Canonical(%q) = %q; want %q", tc.group, got, tc.want)
			}
		})
	}
}

func TestCanonical_IsDeterministic(t *testing.T) {
	t.Parallel()
	group := "Deportes HD España"
	first := Canonical(group)
	for i := 0; i < 100; i++ {
		if got := Canonical(group); got != first {
			t.Fatalf("non-deterministic: iter %d returned %q, want %q", i, got, first)
		}
	}
}

func TestAllCategories_CoversEveryConstant(t *testing.T) {
	t.Parallel()
	// Guard against adding a Category constant but forgetting to expose it
	// in AllCategories (the source of truth for UI chips and i18n keys).
	want := map[Category]bool{
		CategoryGeneral:       true,
		CategoryNews:          true,
		CategorySports:        true,
		CategoryMovies:        true,
		CategoryMusic:         true,
		CategoryEntertainment: true,
		CategoryKids:          true,
		CategoryCulture:       true,
		CategoryDocumentaries: true,
		CategoryInternational: true,
		CategoryTravel:        true,
		CategoryReligion:      true,
		CategoryAdult:         true,
	}
	got := map[Category]bool{}
	for _, c := range AllCategories {
		got[c] = true
	}
	for c := range want {
		if !got[c] {
			t.Errorf("AllCategories missing %q", c)
		}
	}
	for c := range got {
		if !want[c] {
			t.Errorf("AllCategories contains unexpected %q", c)
		}
	}
}
