package iptv

import (
	"strings"
	"testing"
)

func TestParseM3U_Basic(t *testing.T) {
	input := `#EXTM3U
#EXTINF:-1 tvg-id="bbc1" tvg-name="BBC One" tvg-logo="http://logo.com/bbc1.png" group-title="UK",BBC One HD
http://stream.example.com/bbc1
#EXTINF:-1 tvg-id="bbc2" group-title="UK",BBC Two
http://stream.example.com/bbc2
`
	channels, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(channels))
	}

	ch := channels[0]
	if ch.Name != "BBC One HD" {
		t.Errorf("name = %q, want BBC One HD", ch.Name)
	}
	if ch.TvgID != "bbc1" {
		t.Errorf("tvg_id = %q, want bbc1", ch.TvgID)
	}
	if ch.TvgName != "BBC One" {
		t.Errorf("tvg_name = %q, want BBC One", ch.TvgName)
	}
	if ch.LogoURL != "http://logo.com/bbc1.png" {
		t.Errorf("logo = %q", ch.LogoURL)
	}
	if ch.GroupName != "UK" {
		t.Errorf("group = %q, want UK", ch.GroupName)
	}
	if ch.StreamURL != "http://stream.example.com/bbc1" {
		t.Errorf("url = %q", ch.StreamURL)
	}
}

func TestParseM3U_WithAttributes(t *testing.T) {
	input := `#EXTM3U
#EXTINF:-1 tvg-id="canal1" tvg-language="Spanish" tvg-country="ES" channel-number="5",Canal 1
http://stream.example.com/canal1
`
	channels, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}

	ch := channels[0]
	if ch.Language != "Spanish" {
		t.Errorf("language = %q, want Spanish", ch.Language)
	}
	if ch.Country != "ES" {
		t.Errorf("country = %q, want ES", ch.Country)
	}
	if ch.Number != 5 {
		t.Errorf("number = %d, want 5", ch.Number)
	}
}

func TestParseM3U_Empty(t *testing.T) {
	input := `#EXTM3U
`
	channels, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 0 {
		t.Errorf("expected 0 channels, got %d", len(channels))
	}
}

func TestParseM3U_NoHeader(t *testing.T) {
	// Be lenient with M3U files that don't have #EXTM3U
	input := `#EXTINF:-1,Test Channel
http://stream.example.com/test
`
	channels, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}
	if channels[0].Name != "Test Channel" {
		t.Errorf("name = %q, want Test Channel", channels[0].Name)
	}
}

func TestParseM3U_SkipsComments(t *testing.T) {
	input := `#EXTM3U
# This is a comment
#EXTINF:-1,Channel 1
http://stream.example.com/ch1
#EXTVLCOPT:some-option
#EXTINF:-1,Channel 2
http://stream.example.com/ch2
`
	channels, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(channels))
	}
}
