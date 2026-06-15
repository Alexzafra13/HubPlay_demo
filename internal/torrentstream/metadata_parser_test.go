package torrentstream

import (
	"reflect"
	"testing"
)

func TestParseTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  ParsedMetadata
	}{
		{
			name:  "full 4k hdr web-dl hevc dual",
			title: "Dune Part Two 2024 2160p WEB-DL HDR DV x265 DUAL LATINO",
			want: ParsedMetadata{
				Resolution: "2160p",
				Codec:      "HEVC",
				Source:     "WEB-DL",
				HDR:        true,
				DV:         true,
				Languages:  []string{"Latino", "Dual"},
			},
		},
		{
			name:  "1080p bluray x264 spanish",
			title: "The Matrix 1999 1080p BluRay x264 SPANISH",
			want: ParsedMetadata{
				Resolution: "1080p",
				Codec:      "H.264",
				Source:     "BluRay",
				Languages:  []string{"Spanish"},
			},
		},
		{
			name:  "cam release",
			title: "Some Movie 2025 HDCAM 720p",
			want: ParsedMetadata{
				Resolution: "720p",
				IsCam:      true,
				Languages:  []string{"Original"},
			},
		},
		{
			name:  "no tags defaults to original",
			title: "Plain Title Without Tags",
			want: ParsedMetadata{
				Languages: []string{"Original"},
			},
		},
		{
			name:  "4k token maps to 2160p",
			title: "Movie 4K UHD",
			want: ParsedMetadata{
				Resolution: "2160p",
				Languages:  []string{"Original"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTitle(tt.title)
			// Score is derived; assert the structural fields only.
			got.QualityScore = 0
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseTitle(%q)\n got=%+v\nwant=%+v", tt.title, got, tt.want)
			}
		})
	}
}

func TestParseTitleDoesNotFalsePositiveDV(t *testing.T) {
	// "dvd" must not trigger the Dolby Vision (dv) detector.
	m := parseTitle("Old Movie DVDRip XviD")
	if m.DV {
		t.Error("DVDRip should not set DV")
	}
	if m.Source != "DVD" {
		t.Errorf("source: got %q want DVD", m.Source)
	}
	if m.Codec != "XviD" {
		t.Errorf("codec: got %q want XviD", m.Codec)
	}
}

func TestQualityScoreOrdering(t *testing.T) {
	bluray4k := parseTitle("Film 2160p BluRay x265 HDR").QualityScore
	web1080 := parseTitle("Film 1080p WEB-DL x264").QualityScore
	cam := parseTitle("Film 1080p HDCAM").QualityScore

	if !(bluray4k > web1080) {
		t.Errorf("4K BluRay (%d) should outscore 1080p WEB-DL (%d)", bluray4k, web1080)
	}
	if cam >= 0 {
		t.Errorf("cam score should be heavily negative, got %d", cam)
	}
}

func TestQualityBucket(t *testing.T) {
	cases := map[string]string{
		"Movie 2160p":      "4K",
		"Movie 1080p":      "1080p",
		"Movie 720p":       "720p",
		"Movie 480p":       "480p",
		"Show S01E01 HDTV": "HDTV",
		"Movie unknown":    "",
	}
	for title, want := range cases {
		if got := parseTitle(title).qualityBucket(); got != want {
			t.Errorf("%q: got %q want %q", title, got, want)
		}
	}
}
