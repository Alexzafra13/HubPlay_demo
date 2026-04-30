package stream

import (
	"fmt"
	"strings"
)

// GenerateMasterPlaylist creates an HLS master playlist (M3U8) offering
// multiple quality levels for adaptive bitrate streaming.
func GenerateMasterPlaylist(itemID, baseURL string, profiles []string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")

	for _, name := range profiles {
		p, ok := Profiles[name]
		if !ok || name == "original" {
			continue
		}

		bandwidth := parseBitrate(p.VideoBitrate) + parseBitrate(p.AudioBitrate)

		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,FRAME-RATE=%d,NAME=\"%s\"\n",
			bandwidth, p.Width, p.Height, p.MaxFrameRate, p.Name)
		fmt.Fprintf(&b, "%s/api/v1/stream/%s/%s/index.m3u8\n", baseURL, itemID, name)
	}

	return b.String()
}

// ParseBitrate converts a string like "4000k" to bits per second.
// Exported so federation peer-stream handlers can build their own
// master playlist without duplicating the parser.
func ParseBitrate(s string) int { return parseBitrate(s) }

// parseBitrate is the internal helper used by GenerateMasterPlaylist
// in this package. ParseBitrate above is the export.
func parseBitrate(s string) int {
	if s == "" {
		return 0
	}
	multiplier := 1
	if strings.HasSuffix(s, "k") {
		multiplier = 1000
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "M") {
		multiplier = 1_000_000
		s = s[:len(s)-1]
	}
	val := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			val = val*10 + int(c-'0')
		}
	}
	return val * multiplier
}
