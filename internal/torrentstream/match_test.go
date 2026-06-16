package torrentstream

import "testing"

func TestMatchesQuery_Movies(t *testing.T) {
	cases := []struct {
		name  string
		title string
		q     Query
		want  bool
	}{
		{
			name:  "exact title and year",
			title: "Dune 2021 1080p BluRay x264-GROUP",
			q:     Query{Type: MediaTypeMovie, Title: "Dune", Year: 2021},
			want:  true,
		},
		{
			name:  "wrong year (remake) dropped",
			title: "Dune 1984 1080p BluRay x264-GROUP",
			q:     Query{Type: MediaTypeMovie, Title: "Dune", Year: 2021},
			want:  false,
		},
		{
			name:  "year within tolerance kept",
			title: "Some Movie 2020 1080p",
			q:     Query{Type: MediaTypeMovie, Title: "Some Movie", Year: 2021},
			want:  true,
		},
		{
			name:  "missing query token dropped",
			title: "Dune 2021 1080p",
			q:     Query{Type: MediaTypeMovie, Title: "Dune Part Two", Year: 2024},
			want:  false,
		},
		{
			name:  "accents folded",
			title: "Amelie 2001 1080p BluRay",
			q:     Query{Type: MediaTypeMovie, Title: "Amélie", Year: 2001},
			want:  true,
		},
		{
			name:  "stopwords ignored",
			title: "Lord of the Rings Fellowship 2001 1080p",
			q:     Query{Type: MediaTypeMovie, Title: "The Lord of the Rings: The Fellowship", Year: 2001},
			want:  true,
		},
		{
			name:  "no year in release title still kept",
			title: "Dune 1080p BluRay x264",
			q:     Query{Type: MediaTypeMovie, Title: "Dune", Year: 2021},
			want:  true,
		},
		{
			name:  "imdb id match overrides title mismatch",
			title: "Completely Different Name 1080p",
			q:     Query{Type: MediaTypeMovie, Title: "Dune", IMDbID: "tt1160419"},
			// result carries the matching imdb id set below
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := SearchResult{Title: tc.title}
			if tc.q.IMDbID != "" && tc.name == "imdb id match overrides title mismatch" {
				r.IMDbID = tc.q.IMDbID
			}
			if got := matchesQuery(r, tc.q); got != tc.want {
				t.Errorf("matchesQuery(%q) = %v, want %v", tc.title, got, tc.want)
			}
		})
	}
}

func TestMatchesQuery_Series(t *testing.T) {
	cases := []struct {
		name  string
		title string
		q     Query
		want  bool
	}{
		{
			name:  "explicit SxxEyy",
			title: "Breaking Bad S01E02 720p HDTV",
			q:     Query{Type: MediaTypeSeries, Title: "Breaking Bad", Season: 1, Episode: 2},
			want:  true,
		},
		{
			name:  "wrong episode dropped",
			title: "Breaking Bad S01E05 720p HDTV",
			q:     Query{Type: MediaTypeSeries, Title: "Breaking Bad", Season: 1, Episode: 2},
			want:  false,
		},
		{
			name:  "season pack serves the episode",
			title: "Breaking Bad S01 Complete 1080p",
			q:     Query{Type: MediaTypeSeries, Title: "Breaking Bad", Season: 1, Episode: 2},
			want:  true,
		},
		{
			name:  "NxNN form",
			title: "Breaking Bad 1x02 HDTV",
			q:     Query{Type: MediaTypeSeries, Title: "Breaking Bad", Season: 1, Episode: 2},
			want:  true,
		},
		{
			name:  "whole-series search (no episode) keeps any",
			title: "Breaking Bad S03E07 1080p",
			q:     Query{Type: MediaTypeSeries, Title: "Breaking Bad"},
			want:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesQuery(SearchResult{Title: tc.title}, tc.q); got != tc.want {
				t.Errorf("matchesQuery(%q) = %v, want %v", tc.title, got, tc.want)
			}
		})
	}
}

func TestQueryModes(t *testing.T) {
	if got := (Query{}).modes(); len(got) != 0 {
		t.Errorf("empty query should have no modes, got %v", got)
	}
	if got := (Query{IMDbID: "tt1"}).modes(); len(got) != 1 || got[0] != modeIMDb {
		t.Errorf("imdb-only modes: %v", got)
	}
	if got := (Query{Title: "x"}).modes(); len(got) != 1 || got[0] != modeText {
		t.Errorf("title-only modes: %v", got)
	}
	both := (Query{IMDbID: "tt1", Title: "x"}).modes()
	if len(both) != 2 {
		t.Errorf("hybrid modes: got %v want [imdb text]", both)
	}
}

func TestQueryTextQuery(t *testing.T) {
	cases := map[string]struct {
		q    Query
		want string
	}{
		"movie with year":  {Query{Type: MediaTypeMovie, Title: "Dune", Year: 2021}, "Dune 2021"},
		"movie no year":    {Query{Type: MediaTypeMovie, Title: "Dune"}, "Dune"},
		"series episode":   {Query{Type: MediaTypeSeries, Title: "Breaking Bad", Season: 1, Episode: 2}, "Breaking Bad S01E02"},
		"series season":    {Query{Type: MediaTypeSeries, Title: "Breaking Bad", Season: 3}, "Breaking Bad S03"},
		"series whole":     {Query{Type: MediaTypeSeries, Title: "Breaking Bad"}, "Breaking Bad"},
		"empty title none": {Query{Type: MediaTypeMovie, Year: 2021}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.q.textQuery(); got != tc.want {
				t.Errorf("textQuery() = %q, want %q", got, tc.want)
			}
		})
	}
}
