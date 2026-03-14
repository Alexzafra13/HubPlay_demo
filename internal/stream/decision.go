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
type PlaybackDecision struct {
	Method     PlaybackMethod
	VideoCodec string
	AudioCodec string
	Container  string
	Profile    Profile // transcoding profile if Method == Transcode
}

// Web-compatible codecs and containers.
var (
	webVideoCodecs    = map[string]bool{"h264": true, "vp8": true, "vp9": true, "av1": true}
	webAudioCodecs    = map[string]bool{"aac": true, "mp3": true, "opus": true, "vorbis": true, "flac": true}
	webContainers     = map[string]bool{"mp4": true, "webm": true, "mov": true}
	remuxableContainers = map[string]bool{"matroska": true, "mkv": true, "avi": true, "mpegts": true}
)

// Decide analyzes the item's media streams and returns a playback decision.
// It follows the waterfall: DirectPlay → DirectStream → Transcode.
func Decide(item *db.Item, streams []*db.MediaStream, requestedProfile string) PlaybackDecision {
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

	videoOK := webVideoCodecs[videoStream.Codec]
	audioOK := audioStream == nil || webAudioCodecs[audioStream.Codec]
	containerOK := isWebContainer(item.Container)

	// DirectPlay: everything is compatible
	if videoOK && audioOK && containerOK {
		return PlaybackDecision{
			Method:     MethodDirectPlay,
			VideoCodec: videoStream.Codec,
			AudioCodec: audioCodecName(audioStream),
			Container:  item.Container,
		}
	}

	// DirectStream: codecs OK but container needs remuxing (e.g., MKV → MP4)
	if videoOK && audioOK && remuxableContainers[item.Container] {
		return PlaybackDecision{
			Method:     MethodDirectStream,
			VideoCodec: videoStream.Codec,
			AudioCodec: audioCodecName(audioStream),
			Container:  "mp4",
		}
	}

	// Transcode
	profile := DefaultProfile()
	if requestedProfile != "" {
		if p, ok := Profiles[requestedProfile]; ok {
			profile = p
		}
	}

	return PlaybackDecision{
		Method:     MethodTranscode,
		VideoCodec: "h264",
		AudioCodec: "aac",
		Container:  "mpegts",
		Profile:    profile,
	}
}

func isWebContainer(container string) bool {
	// ffprobe may return comma-separated format names like "mov,mp4,m4a,3gp,3g2,mj2"
	for _, part := range splitContainer(container) {
		if webContainers[part] {
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
