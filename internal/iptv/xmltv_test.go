package iptv

import (
	"strings"
	"testing"
)

func TestParseXMLTV_Basic(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="bbc1.uk">
    <display-name>BBC One</display-name>
    <icon src="http://logo.com/bbc1.png"/>
  </channel>
  <channel id="bbc2.uk">
    <display-name>BBC Two</display-name>
  </channel>
  <programme start="20260314180000 +0000" stop="20260314190000 +0000" channel="bbc1.uk">
    <title>News at Six</title>
    <desc>Evening news bulletin</desc>
    <category>News</category>
    <icon src="http://img.com/news.png"/>
  </programme>
  <programme start="20260314190000 +0000" stop="20260314200000 +0000" channel="bbc1.uk">
    <title>EastEnders</title>
    <desc>Drama series</desc>
    <category>Drama</category>
  </programme>
</tv>`

	data, err := ParseXMLTV(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(data.Channels))
	}

	if data.Channels[0].ID != "bbc1.uk" {
		t.Errorf("channel ID = %q", data.Channels[0].ID)
	}
	if data.Channels[0].Name != "BBC One" {
		t.Errorf("channel name = %q", data.Channels[0].Name)
	}
	if data.Channels[0].Icon != "http://logo.com/bbc1.png" {
		t.Errorf("channel icon = %q", data.Channels[0].Icon)
	}

	if len(data.Programs) != 2 {
		t.Fatalf("expected 2 programs, got %d", len(data.Programs))
	}

	prog := data.Programs[0]
	if prog.ChannelID != "bbc1.uk" {
		t.Errorf("program channel = %q", prog.ChannelID)
	}
	if prog.Title != "News at Six" {
		t.Errorf("program title = %q", prog.Title)
	}
	if prog.Description != "Evening news bulletin" {
		t.Errorf("program desc = %q", prog.Description)
	}
	if prog.Category != "News" {
		t.Errorf("program category = %q", prog.Category)
	}
	if prog.IconURL != "http://img.com/news.png" {
		t.Errorf("program icon = %q", prog.IconURL)
	}
	if prog.Start.Year() != 2026 || prog.Start.Month() != 3 || prog.Start.Day() != 14 {
		t.Errorf("program start = %v", prog.Start)
	}
	if prog.Stop.Hour() != 19 {
		t.Errorf("program stop hour = %d", prog.Stop.Hour())
	}
}

func TestParseXMLTV_Empty(t *testing.T) {
	input := `<?xml version="1.0"?><tv></tv>`
	data, err := ParseXMLTV(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Channels) != 0 || len(data.Programs) != 0 {
		t.Error("expected empty data")
	}
}

func TestParseXMLTV_SkipsInvalidTimes(t *testing.T) {
	input := `<?xml version="1.0"?>
<tv>
  <programme start="invalid" stop="20260314190000" channel="ch1">
    <title>Bad Start</title>
  </programme>
  <programme start="20260314190000" stop="20260314200000" channel="ch1">
    <title>Good One</title>
  </programme>
</tv>`

	data, err := ParseXMLTV(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Programs) != 1 {
		t.Fatalf("expected 1 program (skipping bad time), got %d", len(data.Programs))
	}
	if data.Programs[0].Title != "Good One" {
		t.Errorf("wrong program kept: %q", data.Programs[0].Title)
	}
}

func TestParseXMLTVTime(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"20260314180000 +0000", false},
		{"20260314180000 +0100", false},
		{"20260314180000", false},
		{"invalid", true},
		{"", true},
	}

	for _, tc := range tests {
		_, err := parseXMLTVTime(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseXMLTVTime(%q): err=%v, wantErr=%v", tc.input, err, tc.wantErr)
		}
	}
}
