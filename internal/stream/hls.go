package stream

import (
	"fmt"
	"strings"
)

// GenerateMasterPlaylist creates an HLS master playlist (M3U8) offering
// multiple quality levels for adaptive bitrate streaming.
// audioStreamIndex < 0 → omit the ?audio= query string entirely so
// the per-quality endpoint falls back to ffmpeg's default audio
// pick (current behaviour for users without a preferred-language
// preference). audioStreamIndex >= 0 → embed it in every variant URL
// the master playlist emits so hls.js's per-quality switches keep
// the same dub instead of falling back to the file default mid-play.
func GenerateMasterPlaylist(itemID, baseURL string, profiles []string, audioStreamIndex int) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")

	suffix := ""
	if audioStreamIndex >= 0 {
		suffix = fmt.Sprintf("?audio=%d", audioStreamIndex)
	}

	for _, name := range profiles {
		p, ok := Profiles[name]
		if !ok || name == "original" {
			continue
		}

		bandwidth := parseBitrate(p.VideoBitrate) + parseBitrate(p.AudioBitrate)

		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,FRAME-RATE=%d,NAME=\"%s\"\n",
			bandwidth, p.Width, p.Height, p.MaxFrameRate, p.Name)
		fmt.Fprintf(&b, "%s/api/v1/stream/%s/%s/index.m3u8%s\n", baseURL, itemID, name, suffix)
	}

	return b.String()
}

// SynthesizeVODManifest builds a complete HLS playlist for a transcode
// session up-front, listing every segment from 0 to N-1 where N =
// ceil(durationSeconds / segmentDuration). The manifest declares
// `#EXT-X-PLAYLIST-TYPE:VOD` and closes with `#EXT-X-ENDLIST`, which
// is what tells hls.js it can seek freely across the whole timeline
// — without these the player treats the stream as live and clamps
// the seek bar to the buffered window.
//
// Segment files do NOT have to exist on disk yet; the segment handler
// is responsible for spinning up ffmpeg at the right offset when an
// unencoded segment is requested. The trade-off is one ffmpeg
// restart per "far seek" (cheap with -c:v copy, ~1-2 s to first
// segment) instead of "seek bar locked to the encoded prefix".
//
// `segmentURLPath` is the per-quality URL the segment handler
// answers — typically the same path that already serves `.ts` files
// today (e.g. `/api/v1/stream/{itemId}/{quality}/segment%05d.ts`).
//
// `durationSeconds` should be > 0; the caller is responsible for
// falling back to the ffmpeg-written manifest when the item's
// duration is unknown (stream-only sources, scan-in-progress).
func SynthesizeVODManifest(durationSeconds, segmentDuration float64, segmentURLTemplate string) string {
	if durationSeconds <= 0 || segmentDuration <= 0 {
		return ""
	}

	totalSegments := int(durationSeconds / segmentDuration)
	lastSegmentDuration := durationSeconds - float64(totalSegments)*segmentDuration
	if lastSegmentDuration > 0.001 {
		totalSegments++
	} else {
		lastSegmentDuration = segmentDuration
	}

	// `#EXT-X-TARGETDURATION` MUST be >= every segment duration; we
	// round up to be safe (an off-by-one rounding error would have
	// some players reject the manifest).
	target := int(segmentDuration)
	if float64(target) < segmentDuration {
		target++
	}

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", target)
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	b.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")

	for i := 0; i < totalSegments; i++ {
		dur := segmentDuration
		if i == totalSegments-1 {
			dur = lastSegmentDuration
		}
		fmt.Fprintf(&b, "#EXTINF:%.3f,\n", dur)
		fmt.Fprintf(&b, segmentURLTemplate+"\n", i)
	}

	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

// ParseBitrate converts a string like "4000k" or "2M" to bits per
// second. Exported because the federation streaming handler needs to
// compute BANDWIDTH= for its peer-flavoured HLS master playlist
// without re-implementing the same parser.
func ParseBitrate(s string) int { return parseBitrate(s) }

// parseBitrate converts a string like "4000k" to bits per second.
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
