package stream

import librarymodel "hubplay/internal/library/model"

// PlaybackMethod indica como el servidor entrega un media item.
type PlaybackMethod string

const (
	MethodDirectPlay   PlaybackMethod = "DirectPlay"
	MethodDirectStream PlaybackMethod = "DirectStream"
	MethodTranscode    PlaybackMethod = "Transcode"
)

// PlaybackDecision es el resultado de analizar los streams de un item
// contra las capacidades del cliente.
//
// CopyVideo/CopyAudio controlan -c:v/-c:a copy vs re-encode en ffmpeg.
// ToneMap indica que la fuente es HDR y el cliente es SDR; implica
// CopyVideo=false y agrega cadena zscale+tonemap en BuildFFmpegArgs.
type PlaybackDecision struct {
	Method     PlaybackMethod
	VideoCodec string
	AudioCodec string
	Container  string
	Profile    Profile
	CopyVideo  bool
	CopyAudio  bool
	ToneMap    bool
}

var remuxableContainers = map[string]bool{
	"matroska": true,
	"mkv":      true,
	"avi":      true,
	"mpegts":   true,
}

// DecideForceDirectPlay cortocircuita la waterfall: el admin ha
// activado playback.force_direct_play. Rellena codecs reales para
// que el UI muestre info correcta.
func DecideForceDirectPlay(item *librarymodel.Item, streams []*librarymodel.MediaStream) PlaybackDecision {
	var videoStream, audioStream *librarymodel.MediaStream
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
	videoCodec := ""
	if videoStream != nil {
		videoCodec = videoStream.Codec
	}
	return PlaybackDecision{
		Method:     MethodDirectPlay,
		VideoCodec: videoCodec,
		AudioCodec: audioCodecName(audioStream),
		Container:  item.Container,
	}
}

// Decide analiza los streams del item y devuelve una decision de playback.
// Waterfall: DirectPlay -> DirectStream -> Transcode.
// caps=nil usa DefaultWebCapabilities.
func Decide(item *librarymodel.Item, streams []*librarymodel.MediaStream, caps *Capabilities, requestedProfile string) PlaybackDecision {
	var videoStream, audioStream *librarymodel.MediaStream
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

	if videoStream == nil {
		return PlaybackDecision{Method: MethodTranscode, Profile: DefaultProfile()}
	}

	eff := effectiveCapabilities(caps)
	videoOK := eff.VideoCodecs[videoStream.Codec]
	audioOK := audioStream == nil || eff.AudioCodecs[audioStream.Codec]
	containerOK := containerInSet(item.Container, eff.Containers)

	// Gate HDR: fuente HDR sin soporte declarado por el cliente ->
	// Transcode con ToneMap para evitar imagen gris/desaturada.
	hdrOK := videoStream.HDRType == "" || hdrFormatInSet(videoStream.HDRType, eff.HDRFormats)

	profile := DefaultProfile()
	if requestedProfile != "" {
		if p, ok := Profiles[requestedProfile]; ok {
			profile = p
		}
	}

	// DirectPlay: todo compatible, sin sesion ffmpeg.
	if videoOK && audioOK && containerOK && hdrOK {
		return PlaybackDecision{
			Method:     MethodDirectPlay,
			VideoCodec: videoStream.Codec,
			AudioCodec: audioCodecName(audioStream),
			Container:  item.Container,
		}
	}

	// DirectStream: video compatible + container remuxable. Copia video,
	// solo re-encode audio si su codec es incompatible. DirectStream
	// no puede tonemapear (no hay decode).
	if videoOK && hdrOK && containerInSet(item.Container, remuxableContainers) {
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

	// Transcode: video incompatible o fuente HDR sin soporte cliente.
	return PlaybackDecision{
		Method:     MethodTranscode,
		VideoCodec: "h264",
		AudioCodec: "aac",
		Container:  "mpegts",
		Profile:    profile,
		CopyVideo:  false,
		CopyAudio:  false,
		ToneMap:    !hdrOK,
	}
}

// hdrFormatInSet compara el label HDR de la fuente contra el set
// declarado por el cliente, normalizando casing.
func hdrFormatInSet(hdrType string, set map[string]bool) bool {
	if hdrType == "" || len(set) == 0 {
		return false
	}
	switch hdrType {
	case "HDR10":
		return set["hdr10"]
	case "HLG":
		return set["hlg"]
	case "DolbyVision":
		return set["dovi"] || set["dolbyvision"]
	default:
		return false
	}
}

func containerInSet(container string, set map[string]bool) bool {
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

func audioCodecName(s *librarymodel.MediaStream) string {
	if s == nil {
		return ""
	}
	return s.Codec
}
