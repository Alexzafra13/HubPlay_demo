package scanner

import "testing"

func TestParseEpisode_StandardLayout(t *testing.T) {
	root := "/media/shows"
	tests := []struct {
		name string
		path string
		want EpisodeMatch
	}{
		{
			name: "Plex layout SxxExx",
			path: "/media/shows/Breaking Bad/Season 03/Breaking Bad - S03E07 - One Minute.mkv",
			want: EpisodeMatch{SeriesName: "Breaking Bad", SeasonNumber: 3, EpisodeNumber: 7, EpisodeTitle: "One Minute", OK: true},
		},
		{
			name: "lowercase + dots between season/episode",
			path: "/media/shows/The Office/Season 02/the.office.s02e05.halloween.mkv",
			want: EpisodeMatch{SeriesName: "The Office", SeasonNumber: 2, EpisodeNumber: 5, EpisodeTitle: "halloween", OK: true},
		},
		{
			name: "Spanish season dir",
			path: "/media/shows/La Casa de Papel/Temporada 2/episodio.s02e03.mkv",
			want: EpisodeMatch{SeriesName: "La Casa de Papel", SeasonNumber: 2, EpisodeNumber: 3, OK: true},
		},
		{
			name: "alternative NxN notation",
			path: "/media/shows/Show/Season 01/Show.1x05.mkv",
			want: EpisodeMatch{SeriesName: "Show", SeasonNumber: 1, EpisodeNumber: 5, OK: true},
		},
		{
			name: "filename only has episode number, season comes from dir",
			path: "/media/shows/Show/Season 04/05.mkv",
			want: EpisodeMatch{SeriesName: "Show", SeasonNumber: 4, EpisodeNumber: 5, EpisodeTitle: "05", OK: true},
		},
		{
			name: "season dir disambiguates filename season (re-numbered show)",
			path: "/media/shows/Doctor Who 2005/Season 12/Doctor.Who.S01E03.mkv",
			want: EpisodeMatch{SeriesName: "Doctor Who 2005", SeasonNumber: 12, EpisodeNumber: 3, OK: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseEpisode(root, tt.path)
			if got.OK != tt.want.OK {
				t.Fatalf("OK: got %v want %v", got.OK, tt.want.OK)
			}
			if got.SeriesName != tt.want.SeriesName {
				t.Errorf("SeriesName: got %q want %q", got.SeriesName, tt.want.SeriesName)
			}
			if got.SeasonNumber != tt.want.SeasonNumber {
				t.Errorf("SeasonNumber: got %d want %d", got.SeasonNumber, tt.want.SeasonNumber)
			}
			if got.EpisodeNumber != tt.want.EpisodeNumber {
				t.Errorf("EpisodeNumber: got %d want %d", got.EpisodeNumber, tt.want.EpisodeNumber)
			}
			if tt.want.EpisodeTitle != "" && got.EpisodeTitle != tt.want.EpisodeTitle {
				t.Errorf("EpisodeTitle: got %q want %q", got.EpisodeTitle, tt.want.EpisodeTitle)
			}
		})
	}
}

func TestParseEpisode_RejectsBadShapes(t *testing.T) {
	root := "/media/shows"
	tests := []string{
		"/media/shows/movie-at-root.mkv",                          // file directly at lib root
		"/media/shows/Some Folder/random-name-no-numbers.mkv",     // no S/E pattern, no numeric filename
		"/media/movies/Inception (2010).mkv",                      // outside lib root
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			got := ParseEpisode(root, path)
			if got.OK {
				t.Errorf("expected OK=false, got %+v", got)
			}
		})
	}
}

func TestParseSeasonDir(t *testing.T) {
	tests := []struct {
		name string
		want int
		ok   bool
	}{
		{"Season 01", 1, true},
		{"Season 12", 12, true},
		{"season 3", 3, true},
		{"Temporada 5", 5, true},
		{"Saison 2", 2, true},
		{"Staffel 7", 7, true},
		{"S01", 1, true},
		{"s12", 12, true},
		{"Season01", 1, true},
		{"Specials", 0, false},
		{"Random Folder", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseSeasonDir(tt.name)
			if ok != tt.ok {
				t.Fatalf("ok: got %v want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("season: got %d want %d", got, tt.want)
			}
		})
	}
}

func TestCleanTitle(t *testing.T) {
	tests := map[string]string{
		" - One Minute":          "One Minute",
		".halloween.party.":      "halloween party",
		"  spaces   collapsed  ": "spaces collapsed",
		"-_ ()[]":                "",
	}
	for in, want := range tests {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q): got %q want %q", in, got, want)
		}
	}
}
