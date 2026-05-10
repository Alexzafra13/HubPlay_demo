package stream

import (
	"fmt"
	"strings"
	"testing"
)

func TestGenerateMasterPlaylist(t *testing.T) {
	playlist := GenerateMasterPlaylist("item123", "http://localhost:8096", []string{"1080p", "720p", "480p"}, -1)

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
	playlist := GenerateMasterPlaylist("item1", "", []string{"original", "720p"}, -1)

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

// SynthesizeVODManifest tests pin the playlist-shape contract that
// hls.js relies on: VOD type, ENDLIST present, segment count derived
// from duration / segmentDuration, last segment carries the leftover
// duration when the total isn't a multiple of segmentDuration.

func TestSynthesizeVODManifest_BasicShape(t *testing.T) {
	m := SynthesizeVODManifest(60, 6, "/seg/%d.ts")
	if !strings.Contains(m, "#EXT-X-PLAYLIST-TYPE:VOD") {
		t.Error("missing VOD playlist type — hls.js will treat as live and clamp seek")
	}
	if !strings.Contains(m, "#EXT-X-ENDLIST") {
		t.Error("missing ENDLIST marker — manifest must declare it's complete for free seeking")
	}
	if !strings.Contains(m, "#EXT-X-TARGETDURATION:6") {
		t.Error("expected TARGETDURATION matching segmentDuration")
	}
}

func TestSynthesizeVODManifest_SegmentCountMatchesDuration(t *testing.T) {
	// 60s at 6s segments → exactly 10 segments, all 6s long.
	m := SynthesizeVODManifest(60, 6, "/seg/%d.ts")
	for i := 0; i < 10; i++ {
		want := "/seg/" + fmt.Sprintf("%d", i) + ".ts"
		if !strings.Contains(m, want) {
			t.Errorf("expected segment %d (%q) in manifest", i, want)
		}
	}
	if strings.Contains(m, "/seg/10.ts") {
		t.Error("manifest should not list segment 10 — total is exactly 10 segments (0..9)")
	}
}

func TestSynthesizeVODManifest_LastSegmentDurationIsTrimmed(t *testing.T) {
	// 25s at 6s segments → 4 full segments + 1 partial (1s).
	m := SynthesizeVODManifest(25, 6, "/seg/%d.ts")
	if !strings.Contains(m, "/seg/4.ts") {
		t.Error("expected partial segment 4 to be listed")
	}
	if strings.Contains(m, "/seg/5.ts") {
		t.Error("expected only 5 segments (0..4)")
	}
	// The last #EXTINF line should carry the trimmed duration. We
	// don't pin the exact format too tight; just confirm there's an
	// EXTINF that's < 6.
	if !strings.Contains(m, "#EXTINF:1.000,") {
		t.Errorf("expected last EXTINF to declare 1.000s, manifest was:\n%s", m)
	}
}

func TestSynthesizeVODManifest_ZeroDurationFallback(t *testing.T) {
	if got := SynthesizeVODManifest(0, 6, "/seg/%d.ts"); got != "" {
		t.Errorf("expected empty string for unknown duration, got %q", got)
	}
}
