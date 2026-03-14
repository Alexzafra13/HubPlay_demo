package stream

import (
	"strings"
	"testing"
)

func TestGenerateMasterPlaylist(t *testing.T) {
	playlist := GenerateMasterPlaylist("item123", "http://localhost:8096", []string{"1080p", "720p", "480p"})

	if !strings.HasPrefix(playlist, "#EXTM3U") {
		t.Error("playlist should start with #EXTM3U")
	}

	if !strings.Contains(playlist, "RESOLUTION=1920x1080") {
		t.Error("should contain 1080p resolution")
	}
	if !strings.Contains(playlist, "RESOLUTION=1280x720") {
		t.Error("should contain 720p resolution")
	}
	if !strings.Contains(playlist, "RESOLUTION=854x480") {
		t.Error("should contain 480p resolution")
	}

	if !strings.Contains(playlist, "/api/v1/stream/item123/1080p/index.m3u8") {
		t.Error("should contain 1080p stream URL")
	}
	if !strings.Contains(playlist, "/api/v1/stream/item123/720p/index.m3u8") {
		t.Error("should contain 720p stream URL")
	}
}

func TestGenerateMasterPlaylist_SkipsOriginal(t *testing.T) {
	playlist := GenerateMasterPlaylist("item1", "", []string{"original", "720p"})

	if strings.Contains(playlist, "original") {
		t.Error("should not include original profile in master playlist")
	}
	if !strings.Contains(playlist, "720p") {
		t.Error("should include 720p")
	}
}

func TestParseBitrate(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"4000k", 4_000_000},
		{"192k", 192_000},
		{"2M", 2_000_000},
		{"", 0},
	}

	for _, tc := range tests {
		got := parseBitrate(tc.input)
		if got != tc.want {
			t.Errorf("parseBitrate(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}
