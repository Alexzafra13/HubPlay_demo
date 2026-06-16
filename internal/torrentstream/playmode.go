package torrentstream

import "strings"

// PlayMode is how a torrent's main file should be delivered to the browser.
//
// The browser's <video> (and hls.js+MSE) can only decode a narrow set of
// codecs/containers. A huge share of torrents are H.264+AAC inside an MKV —
// the browser CAN decode the codecs but CAN'T open the container, so they
// show a black screen today. The fix is a cheap `-c copy` repackage (remux)
// into HLS; only genuinely undecodable video (HEVC/AV1/XviD…) needs a full
// re-encode (P1b-2, not yet wired).
type PlayMode int

const (
	// PlayDirect: serve the file as-is over /torrent/stream — the browser
	// plays it natively (mp4/H.264/AAC or webm/VP8-9/Opus).
	PlayDirect PlayMode = iota
	// PlayRemux: browser-decodable video (H.264) in a non-playable container
	// → `-c:v copy` to HLS (near-zero CPU); audio copied when AAC, otherwise
	// cheaply re-encoded to AAC.
	PlayRemux
	// PlayReencode: video codec the browser can't decode (HEVC/AV1/XviD…) →
	// full transcode. Detected here, but the transcoder lands in P1b-2; until
	// then the UI surfaces "needs transcoding" instead of a black screen.
	PlayReencode
)

func (m PlayMode) String() string {
	switch m {
	case PlayDirect:
		return "direct"
	case PlayRemux:
		return "remux"
	case PlayReencode:
		return "reencode"
	default:
		return "unknown"
	}
}

// MediaInfo is the slice of an ffprobe result the play decision needs:
// the container format and the primary video/audio codecs.
type MediaInfo struct {
	// Container is ffprobe's format_name (a comma list, e.g.
	// "mov,mp4,m4a,3gp,3g2,mj2" or "matroska,webm").
	Container string
	// VideoCodec / AudioCodec are the primary stream codec names, lowercased
	// (e.g. "h264", "hevc", "aac", "ac3"). Empty when absent.
	VideoCodec string
	AudioCodec string
}

// remuxableVideo are video codecs that survive `-c:v copy` into HLS/mpegts
// AND decode in the browser. Only H.264 qualifies (HEVC rides in mpegts but
// MSE rarely decodes it; VP8/9/AV1 aren't allowed in mpegts at all).
var remuxableVideo = map[string]bool{"h264": true}

// DecidePlayMode maps probed media to a delivery mode.
//
// ffprobe reports BOTH .mkv and .webm as the "matroska,webm" format, so the
// container name alone can't tell an unplayable MKV from a native WebM — the
// codecs decide. The rules:
//   - mp4 family + H.264 + AAC/MP3 → DirectPlay;
//   - matroska/webm + VP8/9/AV1 + Opus/Vorbis (i.e. a real WebM) → DirectPlay;
//   - H.264 video in anything else (the classic H.264-in-MKV) → Remux;
//   - everything else (HEVC/AV1-in-mkv/XviD…) → Reencode (P1b-2).
func DecidePlayMode(info MediaInfo) PlayMode {
	v := strings.ToLower(strings.TrimSpace(info.VideoCodec))
	a := strings.ToLower(strings.TrimSpace(info.AudioCodec))

	if isMP4Family(info.Container) && v == "h264" && (a == "" || a == "aac" || a == "mp3") {
		return PlayDirect
	}
	if isMatroskaFamily(info.Container) && isWebmVideo(v) && (a == "" || a == "opus" || a == "vorbis") {
		return PlayDirect
	}
	if remuxableVideo[v] {
		return PlayRemux
	}
	return PlayReencode
}

// AudioNeedsTranscode reports whether the remux path must re-encode audio to
// AAC (cheap) because the source audio wouldn't play after a bare copy.
func AudioNeedsTranscode(audioCodec string) bool {
	a := strings.ToLower(strings.TrimSpace(audioCodec))
	return a != "aac" && a != "mp3"
}

func isWebmVideo(codec string) bool {
	switch codec {
	case "vp8", "vp9", "av1":
		return true
	}
	return false
}

func containerHas(formatName string, want ...string) bool {
	for _, f := range strings.Split(strings.ToLower(formatName), ",") {
		f = strings.TrimSpace(f)
		for _, w := range want {
			if f == w {
				return true
			}
		}
	}
	return false
}

// isMP4Family reports whether ffprobe's format_name names the MP4/QuickTime
// container family (browser-native for H.264+AAC).
func isMP4Family(formatName string) bool {
	return containerHas(formatName, "mp4", "mov", "m4a", "3gp", "3g2")
}

// isMatroskaFamily reports whether the format is matroska/webm (the codecs
// then decide whether it's a playable WebM or an MKV that needs remuxing).
func isMatroskaFamily(formatName string) bool {
	return containerHas(formatName, "matroska", "webm")
}
