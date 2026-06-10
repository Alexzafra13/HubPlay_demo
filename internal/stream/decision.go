package stream

import (
	"strings"

	librarymodel "hubplay/internal/library/model"
)

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
// reempaquetar en mpegts sin re-encode. mp4/mov van incluidos: un MP4
// h264+AC3 (rip típico) solo necesita remux + transcode de audio — sin
// ellos en este set caía a re-encode completo del vídeo (CPU + pérdida
// de calidad) sin necesidad. PB-7 (audit 2026-06-10).
var remuxableContainers = map[string]bool{
	"matroska": true,
	"mkv":      true,
	"avi":      true,
	"mpegts":   true,
	"mp4":      true,
	"mov":      true,
	"m4a":      true,
}

// DecideForceDirectPlay cortocircuita el waterfall y devuelve DirectPlay
// sin importar las capabilities del cliente. Hook de política para el
// flag admin `playback.force_direct_play`.
func DecideForceDirectPlay(item *librarymodel.Item, streams []*librarymodel.MediaStream) PlaybackDecision {
	videoStream, audioStream := pickStreams(streams, -1)
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
//
// audioStreamIndex es la pista de audio seleccionada (0-based,
// per-type); -1 = default del fichero. La decisión DEBE evaluarse
// contra la pista que va a sonar: con un MKV de default AAC + pista
// DTS, el usuario que cambiaba a DTS recibía DirectStream con
// CopyAudio=true → DTS copiado al TS → vídeo mudo en el navegador.
// PB-6 (audit 2026-06-10).
func Decide(item *librarymodel.Item, streams []*librarymodel.MediaStream, caps *Capabilities, requestedProfile string, audioStreamIndex int) PlaybackDecision {
	videoStream, audioStream := pickStreams(streams, audioStreamIndex)

	// Sin video stream — no se puede reproducir
	if videoStream == nil {
		return PlaybackDecision{Method: MethodTranscode, Profile: DefaultProfile()}
	}

	eff := effectiveCapabilities(caps)
	videoOK := eff.VideoCodecs[videoStream.Codec] && videoProfileCompatible(videoStream)
	audioOK := audioStream == nil || eff.AudioCodecs[audioStream.Codec]
	containerOK := containerOKForClient(item.Container, eff.Containers, videoStream, audioStream)

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

// pickStreams selecciona el video stream (default o primero) y la pista
// de audio efectiva: la del índice pedido (0-based, contando solo
// audio) o, con índice negativo / fuera de rango, la default del
// fichero — fuera de rango cae al default por robustez; la validación
// dura contra el índice vive en el Manager antes de llegar aquí.
func pickStreams(streams []*librarymodel.MediaStream, audioStreamIndex int) (video, audio *librarymodel.MediaStream) {
	var defaultAudio *librarymodel.MediaStream
	audioOrd := 0
	for _, s := range streams {
		switch s.StreamType {
		case "video":
			if video == nil || s.IsDefault {
				video = s
			}
		case "audio":
			if defaultAudio == nil || s.IsDefault {
				defaultAudio = s
			}
			if audioOrd == audioStreamIndex {
				audio = s
			}
			audioOrd++
		}
	}
	if audio == nil {
		audio = defaultAudio
	}
	return video, audio
}

// videoProfileCompatible gatea los perfiles h264 que ningún navegador
// decodifica: High 10 ("Hi10P", omnipresente en anime), High 4:2:2 y
// High 4:4:4 Predictive. Sin este gate, "h264" matcheaba las caps del
// cliente por nombre y el fichero salía DirectPlay → pantalla
// rota/verde garantizada. HEVC Main 10 NO se gatea a propósito: todo
// decoder hardware de HEVC soporta Main 10 (es el perfil dominante) y
// forzar transcode aquí quemaría CPU en los 4K que hoy reproducen bien.
// Perfil vacío (probe antiguo, pre-captura) = asumir compatible.
// PB-8 (audit 2026-06-10).
func videoProfileCompatible(s *librarymodel.MediaStream) bool {
	if s.Codec != "h264" || s.Profile == "" {
		return true
	}
	p := strings.ToLower(s.Profile)
	return !strings.Contains(p, "10") &&
		!strings.Contains(p, "4:2:2") &&
		!strings.Contains(p, "4:4:4")
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

// Códecs que la spec de WebM permite. ffprobe etiqueta TODO Matroska
// como "matroska,webm" (comparten demuxer), así que el alias "webm" del
// format_name no prueba nada por sí solo: un MKV h264+aac matchearía el
// "webm" de las caps del cliente y se serviría como DirectPlay —
// Firefox y Safari no reproducen Matroska (pantalla negra). Solo
// contamos el alias webm cuando los códecs reales caben en un WebM de
// verdad. Ver PB-1 en docs/memory/audit-2026-06-10-playback-chain.md.
var webmVideoCodecs = map[string]bool{"vp8": true, "vp9": true, "av1": true}
var webmAudioCodecs = map[string]bool{"opus": true, "vorbis": true}

// containerOKForClient es containerInSet con la corrección del alias
// webm: el match por nombre de container se acepta salvo que el nombre
// sea "webm" y los códecs desmientan que el fichero sea un WebM real.
func containerOKForClient(container string, set map[string]bool, video, audio *librarymodel.MediaStream) bool {
	for _, part := range splitContainer(container) {
		if part == "webm" && !webmCompatibleCodecs(video, audio) {
			continue
		}
		if set[part] {
			return true
		}
	}
	return false
}

func webmCompatibleCodecs(video, audio *librarymodel.MediaStream) bool {
	if video == nil || !webmVideoCodecs[video.Codec] {
		return false
	}
	return audio == nil || webmAudioCodecs[audio.Codec]
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
