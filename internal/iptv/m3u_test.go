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
	pl, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(pl.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(pl.Channels))
	}

	ch := pl.Channels[0]
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
	pl, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(pl.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(pl.Channels))
	}

	ch := pl.Channels[0]
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
	pl, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.Channels) != 0 {
		t.Errorf("expected 0 channels, got %d", len(pl.Channels))
	}
}

func TestParseM3U_NoHeader(t *testing.T) {
	// Be lenient with M3U files that don't have #EXTM3U
	input := `#EXTINF:-1,Test Channel
http://stream.example.com/test
`
	pl, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(pl.Channels))
	}
	if pl.Channels[0].Name != "Test Channel" {
		t.Errorf("name = %q, want Test Channel", pl.Channels[0].Name)
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
	pl, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(pl.Channels))
	}
}

func TestParseM3U_ExtractsEPGURLFromHeader(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{
			name:   "url-tvg",
			header: `#EXTM3U url-tvg="https://iptv-org.github.io/epg/guides/es.xml"`,
			want:   "https://iptv-org.github.io/epg/guides/es.xml",
		},
		{
			name:   "x-tvg-url",
			header: `#EXTM3U x-tvg-url="https://example.com/epg.xml"`,
			want:   "https://example.com/epg.xml",
		},
		{
			name:   "tvg-url",
			header: `#EXTM3U tvg-url="http://example.com/guide.xml"`,
			want:   "http://example.com/guide.xml",
		},
		{
			name:   "comma-separated list keeps first",
			header: `#EXTM3U url-tvg="https://example.com/a.xml,https://example.com/b.xml"`,
			want:   "https://example.com/a.xml",
		},
		{
			name:   "url-tvg wins over alternates",
			header: `#EXTM3U x-tvg-url="https://alt.com/alt.xml" url-tvg="https://primary.com/guide.xml"`,
			want:   "https://primary.com/guide.xml",
		},
		{
			name:   "rejects javascript scheme",
			header: `#EXTM3U url-tvg="javascript:alert(1)"`,
			want:   "",
		},
		{
			name:   "no attribute",
			header: `#EXTM3U`,
			want:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := tc.header + "\n" +
				`#EXTINF:-1 tvg-id="c1",Channel 1` + "\n" +
				"http://stream.example.com/c1\n"
			pl, err := ParseM3U(strings.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			if pl.EPGURL != tc.want {
				t.Errorf("EPGURL = %q, want %q", pl.EPGURL, tc.want)
			}
		})
	}
}
