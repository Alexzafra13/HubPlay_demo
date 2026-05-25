package stream

import librarymodel "hubplay/internal/library/model"

// PlaybackMethod indica cómo el servidor entrega un media item.
type PlaybackMethod string

const (
	MethodDirectPlay   PlaybackMethod = "DirectPlay"   // el cliente reproduce el archivo tal cual
	MethodDirectStream PlaybackMethod = "DirectStream"  // remux a contenedor compatible
	MethodTranscode    PlaybackMethod = "Transcode"      // transcodificación completa
)

// PlaybackDecision es el resultado de analizar streams contra las capacidades del cliente.
//
// CopyVideo/CopyAudio controlan qué hace ffmpeg por stream:
//   - true  → `-c:v/a copy` (sin re-encode)
//   - false → re-encode (video con encoder configurado, audio a AAC stereo)
//
// Permite copiar video (caro) y solo re-encodear audio (barato) en el caso
// común de h264+mkv con AC3/DTS incompatible con el navegador.
//
// ToneMap se activa cuando la fuente es HDR y el cliente no soporta ese formato.
// Implica CopyVideo=false e inserta cadena zscale+tonemap en BuildFFmpegArgs.
// Sin esto, HDR en cliente SDR se ve gris y desaturado (luma PQ renderizado como sRGB).
type PlaybackDecision struct {
	Method     PlaybackMethod
	VideoCodec string
	AudioCodec string
	Container  string
	Profile    Profile // transcoding profile if Method == Transcode
	CopyVideo  bool
	CopyAudio  bool
	ToneMap    bool
}

// remuxableContainers: contenedores que ffmpeg puede reempaquetar sin re-encode.
// DirectStream requiere que la fuente sea remuxable del lado servidor.
var remuxableContainers = map[string]bool{
	"matroska": true,
	"mkv":      true,
	"avi":      true,
	"mpegts":   true,
}

// DecideForceDirectPlay cortocircuita la cascada de capacidades y devuelve
// DirectPlay incondicional. Hook para admin con `playback.force_direct_play`.
// Rellena codecs reales del archivo para que la UI muestre info correcta.
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

// Decide analyzes the item's media streams and returns a playback decision.
// It follows the waterfall: DirectPlay → DirectStream → Transcode.
//
// `caps` carries the client's declared capabilities (parsed from the
// X-Hubplay-Client-Capabilities header). Pass nil for "unknown client" —
// the function falls back to DefaultWebCapabilities, matching the
// behaviour the original hard-coded version had so legacy web clients
// see no change.
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

	// No video stream — can't play
	if videoStream == nil {
		return PlaybackDecision{Method: MethodTranscode, Profile: DefaultProfile()}
	}

	eff := effectiveCapabilities(caps)
	videoOK := eff.VideoCodecs[videoStream.Codec]
	audioOK := audioStream == nil || eff.AudioCodecs[audioStream.Codec]
	containerOK := containerInSet(item.Container, eff.Containers)

	// HDR gate: a source with an HDR transfer (PQ / HLG / Dolby Vision)
	// can only ride the DirectPlay or DirectStream paths if the client
	// declared support for that exact HDR format. Otherwise the browser
	// receives PQ-coded luma but renders it as if it were sRGB and the
	// picture comes out grey and desaturated. When this gate fires we
	// fall through to Transcode with ToneMap=true; the encoder side
	// inserts a zscale chain that maps the HDR signal down to BT.709.
	hdrOK := videoStream.HDRType == "" || hdrFormatInSet(videoStream.HDRType, eff.HDRFormats)

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
	if videoOK && audioOK && containerOK && hdrOK {
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
	//
	// `item.Container` is what ffprobe returned for the file's
	// format_name field, which for mkv arrives as `"matroska,webm"`
	// — a comma-separated list. `containerInSet` is the same helper
	// used for the client-caps check above; it splits on `,` and
	// matches per-part so we recognise the file as remuxable
	// regardless of the exact label ffprobe used.
	// DirectStream stream-copies the video, which is incompatible with
	// any HDR→SDR fix-up (no decode = nothing to tonemap). Gate it on
	// hdrOK so HDR sources for SDR clients fall through to the full
	// transcode below and get the zscale chain applied.
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

	// Transcode: video codec isn't compatible (HEVC, VP9 on a client
	// without VP9, …) OR the source is HDR and the client can't render
	// it. Re-encode everything to the safe defaults; ToneMap propagates
	// to BuildFFmpegArgs, which inserts the zscale+tonemap chain.
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

// hdrFormatInSet matches the source's HDR transfer label (the strings
// emitted by probe.detectHDR — "HDR10", "HLG", "DolbyVision") against
// the client-declared set, which uses lowercase aliases the wire
// header carries ("hdr10", "hlg", "dovi" / "dolbyvision"). Both sides
// are lowercased here so the parser doesn't have to memorize casing.
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

func audioCodecName(s *librarymodel.MediaStream) string {
	if s == nil {
		return ""
	}
	return s.Codec
}
