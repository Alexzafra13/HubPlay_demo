package iptv

import (
	"strings"
	"testing"
)

func TestParseM3U_DeduplicatesByTvgID(t *testing.T) {
	input := `#EXTM3U
#EXTINF:-1 tvg-id="bbc1" group-title="UK",BBC One
http://a/1
#EXTINF:-1 tvg-id="bbc1" group-title="News",BBC One
http://a/1-dup
#EXTINF:-1 tvg-id="bbc2" group-title="UK",BBC Two
http://a/2
#EXTINF:-1 tvg-id="bbc1" group-title="Sport",BBC One
http://a/1-dup2
`
	channels, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 2 {
		t.Fatalf("expected 2 unique channels after dedup, got %d: %+v", len(channels), channels)
	}
	// First occurrence wins: bbc1 should be in group UK, not News/Sport.
	if channels[0].TvgID != "bbc1" || channels[0].GroupName != "UK" {
		t.Errorf("first bbc1: got %+v, want UK group", channels[0])
	}
}

func TestParseM3U_NoDedupForEmptyTvgID(t *testing.T) {
	// Entries without a TvgID have nothing to key on — should be kept all.
	input := `#EXTM3U
#EXTINF:-1 group-title="Custom",Channel A
http://a/x
#EXTINF:-1 group-title="Custom",Channel B
http://a/y
#EXTINF:-1 group-title="Custom",Channel C
http://a/z
`
	channels, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 3 {
		t.Fatalf("entries without tvg-id should all be kept, got %d", len(channels))
	}
}

func TestParseM3U_StripsUTF8BOM(t *testing.T) {
	// Windows tools often save files with a UTF-8 BOM (EF BB BF) prepended.
	// Without stripping it, the first #EXTM3U line fails the HasPrefix check
	// and the parser falls back to lenient mode — safe but ugly.
	input := "\xEF\xBB\xBF#EXTM3U\n" +
		`#EXTINF:-1 tvg-id="c1",Channel 1` + "\n" +
		"http://a/1\n"
	channels, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel after BOM strip, got %d", len(channels))
	}
	if channels[0].Name != "Channel 1" {
		t.Errorf("name: %q", channels[0].Name)
	}
}
