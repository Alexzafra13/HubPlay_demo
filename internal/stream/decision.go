package stream

import "hubplay/internal/db"

// PlaybackMethod describes how the server will deliver a media item.
type PlaybackMethod string

const (
	MethodDirectPlay   PlaybackMethod = "DirectPlay"   // client plays file as-is
	MethodDirectStream PlaybackMethod = "DirectStream"  // remux into compatible container
	MethodTranscode    PlaybackMethod = "Transcode"      // full transcode
)

// PlaybackDecision is the result of analyzing an item's streams against client capabilities.
//
// CopyVideo / CopyAudio describe what ffmpeg should do per stream when
// the chosen Method requires a session (DirectStream or Transcode):
//
//   - CopyVideo=true  → `-c:v copy` (no re-encode, ~5% of full-encode cost)
//   - CopyVideo=false → re-encode with the configured encoder
//   - CopyAudio=true  → `-c:a copy` (works only when the codec is in client caps)
//   - CopyAudio=false → re-encode to AAC stereo (the universal fallback)
//
// The flags exist so a common case — "h264 mkv with AC3 / DTS audio
// the browser can't decode" — can copy the (expensive) video stream
// untouched and only re-encode the cheap audio. Before this change,
// that path full-transcoded the video for no reason.
type PlaybackDecision struct {
	Method     PlaybackMethod
	VideoCodec string
	AudioCodec string
	Container  string
	Profile    Profile // transcoding profile if Method == Transcode
	CopyVideo  bool
	CopyAudio  bool
}

// remuxableContainers lists source containers that ffmpeg can repackage
// into a web-friendly mp4 without re-encoding. The intersection with
// the *client's* container caps is the one that matters for DirectPlay
// (we'd send the file as-is), but DirectStream still needs the source
// to be remuxable on our side.
var remuxableContainers = map[string]bool{
	"matroska": true,
	"mkv":      true,
	"avi":      true,
	"mpegts":   true,
}

// Decide analyzes the item's media streams and returns a playback decision.
// It follows the waterfall: DirectPlay → DirectStream → Transcode.
//
// `caps` carries the client's declared capabilities (parsed from the
// X-Hubplay-Client-Capabilities header). Pass nil for "unknown client" —
// the function falls back to DefaultWebCapabilities, matching the
// behaviour the original hard-coded version had so legacy web clients
// see no change.
func Decide(item *db.Item, streams []*db.MediaStream, caps *Capabilities, requestedProfile string) PlaybackDecision {
	var videoStream, audioStream *db.MediaStream
	for _, s := range streams {
		switch s.StreamType {
		case "video":
			if videoStream == nil || s.IsDefault {
				videoStream = s
			}
		case "audio":
			if audioStream == nil || s.IsDefault {
				audioStream = s
			}
		}
	}

	// No video stream — can't play
	if videoStream == nil {
		return PlaybackDecision{Method: MethodTranscode, Profile: DefaultProfile()}
	}

	eff := effectiveCapabilities(caps)
	videoOK := eff.VideoCodecs[videoStream.Codec]
	audioOK := audioStream == nil || eff.AudioCodecs[audioStream.Codec]
	containerOK := containerInSet(item.Container, eff.Containers)

	// Profile selection used for any path that touches ffmpeg. For
	// stream-copy paths the profile only carries HLS-segmenting knobs
	// (the bitrate / scale fields are ignored when -c:v copy is set).
	profile := DefaultProfile()
	if requestedProfile != "" {
		if p, ok := Profiles[requestedProfile]; ok {
			profile = p
		}
	}

	// DirectPlay: everything is compatible — no ffmpeg session at all.
	if videoOK && audioOK && containerOK {
		return PlaybackDecision{
			Method:     MethodDirectPlay,
			VideoCodec: videoStream.Codec,
			AudioCodec: audioCodecName(audioStream),
			Container:  item.Container,
		}
	}

	// DirectStream when the *video* stream is already compatible. This
	// covers two distinct sub-cases:
	//
	//   a) video + audio OK, only the container needs remuxing
	//      (the classic h264 + AAC + mkv → mp4/HLS case).
	//   b) video OK, audio NOT OK (AC3 / DTS / TrueHD ripped from
	//      BluRay), container is one we can remux into HLS.
	//
	// Both promote out of the full-transcode path: ffmpeg copies the
	// video bytes (`-c:v copy`) and only touches audio when the codec
	// is incompatible. The video stream is by far the most expensive
	// to re-encode, so this is the difference between "burns a CPU
	// core for the duration of playback" and "costs essentially
	// nothing on top of disk I/O".
	if videoOK && remuxableContainers[item.Container] {
		return PlaybackDecision{
			Method:     MethodDirectStream,
			VideoCodec: videoStream.Codec,
			AudioCodec: audioCodecName(audioStream),
			Container:  "mpegts",
			Profile:    profile,
			CopyVideo:  true,
			CopyAudio:  audioOK,
		}
	}

	// Transcode: video codec isn't compatible (HEVC, VP9 on a client
	// without VP9, …). Re-encode everything to the safe defaults.
	return PlaybackDecision{
		Method:     MethodTranscode,
		VideoCodec: "h264",
		AudioCodec: "aac",
		Container:  "mpegts",
		Profile:    profile,
		CopyVideo:  false,
		CopyAudio:  false,
	}
}

func containerInSet(container string, set map[string]bool) bool {
	// ffprobe may return comma-separated format names like
	// "mov,mp4,m4a,3gp,3g2,mj2" — accept the item if ANY of those
	// matches a name the client said it supports.
	for _, part := range splitContainer(container) {
		if set[part] {
			return true
		}
	}
	return false
}

func splitContainer(c string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(c); i++ {
		if i == len(c) || c[i] == ',' {
			p := c[start:i]
			if p != "" {
				parts = append(parts, p)
			}
			start = i + 1
		}
	}
	return parts
}

func audioCodecName(s *db.MediaStream) string {
	if s == nil {
		return ""
	}
	return s.Codec
}
