package stream

import librarymodel "hubplay/internal/library/model"

// PlaybackMethod describe cómo el servidor entrega un media item.
type PlaybackMethod string

const (
	MethodDirectPlay   PlaybackMethod = "DirectPlay"   // cliente reproduce el fichero tal cual
	MethodDirectStream PlaybackMethod = "DirectStream"  // remux a container compatible
	MethodTranscode    PlaybackMethod = "Transcode"      // transcode completo
)

// PlaybackDecision es el resultado de analizar los streams del item
// contra las capabilities del cliente.
//
// CopyVideo/CopyAudio controlan qué hace ffmpeg por stream:
//   - true  → `-c:v/a copy` (sin re-encode)
//   - false → re-encode con el encoder configurado
//
// ToneMap se activa cuando la fuente es HDR y el cliente no la soporta;
// implica CopyVideo=false e inserta cadena zscale+tonemap.
type PlaybackDecision struct {
	Method     PlaybackMethod
	VideoCodec string
	AudioCodec string
	Container  string
	Profile    Profile // perfil de transcoding si Method == Transcode
	CopyVideo  bool
	CopyAudio  bool
	ToneMap    bool
}

// remuxableContainers lista containers fuente que ffmpeg puede
// reempaquetar en mp4 sin re-encode.
var remuxableContainers = map[string]bool{
	"matroska": true,
	"mkv":      true,
	"avi":      true,
	"mpegts":   true,
}

// DecideForceDirectPlay cortocircuita el waterfall y devuelve DirectPlay
// sin importar las capabilities del cliente. Hook de política para el
// flag admin `playback.force_direct_play`.
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

// Decide analiza los media streams del item y devuelve una decisión de
// playback. Waterfall: DirectPlay → DirectStream → Transcode.
// `caps` nil = "cliente desconocido" → fallback a DefaultWebCapabilities.
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

	// Sin video stream — no se puede reproducir
	if videoStream == nil {
		return PlaybackDecision{Method: MethodTranscode, Profile: DefaultProfile()}
	}

	eff := effectiveCapabilities(caps)
	videoOK := eff.VideoCodecs[videoStream.Codec]
	audioOK := audioStream == nil || eff.AudioCodecs[audioStream.Codec]
	containerOK := containerInSet(item.Container, eff.Containers)

	// Gate HDR: fuente con transfer HDR solo puede ir DirectPlay/DirectStream
	// si el cliente declaró soporte para ese formato exacto. Si no, se
	// fuerza Transcode con ToneMap=true → zscale a BT.709.
	hdrOK := videoStream.HDRType == "" || hdrFormatInSet(videoStream.HDRType, eff.HDRFormats)

	// Selección de profile para cualquier path que toque ffmpeg.
	profile := DefaultProfile()
	if requestedProfile != "" {
		if p, ok := Profiles[requestedProfile]; ok {
			profile = p
		}
	}

	// DirectPlay: todo compatible — sin sesión ffmpeg.
	if videoOK && audioOK && containerOK && hdrOK {
		return PlaybackDecision{
			Method:     MethodDirectPlay,
			VideoCodec: videoStream.Codec,
			AudioCodec: audioCodecName(audioStream),
			Container:  item.Container,
		}
	}

	// DirectStream: video compatible, solo container o audio necesitan
	// trabajo. Copia el video (`-c:v copy`) y solo toca audio si el
	// codec es incompatible. Mucho más barato que transcode completo.
	// Requiere hdrOK porque stream-copy no permite tonemap.
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

	// Transcode: codec de video incompatible o fuente HDR sin soporte
	// en el cliente. Re-encode completo a H.264 + AAC.
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

// hdrFormatInSet compara el label HDR de la fuente (HDR10/HLG/DolbyVision)
// contra el set declarado por el cliente (lowercase aliases).
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
	// ffprobe puede devolver nombres separados por coma (ej. "mov,mp4,m4a").
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
