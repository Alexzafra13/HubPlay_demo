package stream

import "strings"

// BurnSubtitleSpec describe un stream de subtitulos que el transcoder
// debe renderizar directamente en los frames de video ("burn in").
// Necesario para PGS/DVDSUB/ASS que ningun browser renderiza nativamente.
//
// Consecuencia en session key: el burn-in queda horneado en los
// segmentos, asi que cambiar subtitulo requiere nueva sesion de transcode.
type BurnSubtitleSpec struct {
	// Index: 0-based per-type subtitle stream (0:s:N en ffmpeg).
	Index int
	// Codec: nombre ffmpeg lowercase. Determina la estrategia de filtro:
	//   - imagen (PGS, DVDSUB) -> filter_complex overlay
	//   - texto styled (ASS/SSA) -> subtitles= filter
	Codec string
	// InputPath: ruta absoluta del fichero fuente. Requerido para el
	// filtro subtitles= de ASS/SSA (re-lee el fichero).
	InputPath string
}

// IsImageSubtitleCodec indica si el codec es bitmap (PGS, DVDSUB, etc.)
// y necesita compositing via overlay.
func IsImageSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "hdmv_pgs_subtitle", "pgs",
		"dvd_subtitle", "dvdsub",
		"dvb_subtitle", "dvbsub",
		"xsub":
		return true
	default:
		return false
	}
}

// IsStyledTextSubtitleCodec indica si el codec es texto styled (ASS/SSA)
// que requiere burn-in para reproduccion fiel en browsers.
func IsStyledTextSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass", "ssa":
		return true
	default:
		return false
	}
}

// IsBurnableSubtitleCodec indica si el codec no se puede renderizar
// nativamente en un web player y requiere burn-in.
func IsBurnableSubtitleCodec(codec string) bool {
	return IsImageSubtitleCodec(codec) || IsStyledTextSubtitleCodec(codec)
}

// ffmpegInputPathEscape escapa caracteres especiales dentro de la
// sintaxis del filtro subtitles= de ffmpeg (: \ ' [ ] ,).
// Devuelve sin comillas — el caller las pone en subtitles='<escaped>'.
func ffmpegInputPathEscape(path string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		`:`, `\:`,
		`[`, `\[`,
		`]`, `\]`,
		`,`, `\,`,
	)
	return r.Replace(path)
}
