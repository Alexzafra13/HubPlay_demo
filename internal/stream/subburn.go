package stream

import "strings"

// BurnSubtitleSpec describe un stream de subtítulos que el transcoder
// debe renderizar directamente en los frames de video ("burn in").
//
// Invariante de sesión: la elección de burn-in queda baked en los
// segmentos transcodificados, por lo que cambiar subtítulo requiere
// una nueva sesión — misma restricción que cambiar pista de audio.
type BurnSubtitleSpec struct {
	// Index es el índice 0-based del stream de subtítulos que ffmpeg
	// direcciona como `0:s:N`.
	Index int
	// Codec es el nombre ffmpeg en minúsculas del stream de subtítulos
	// elegido. Determina la estrategia de filtro:
	//   - formatos imagen → filter_complex overlay
	//   - texto styled (ass/ssa) → filtro subtitles=
	Codec string
	// InputPath es la ruta absoluta del fichero fuente, usado por el
	// filtro `subtitles` de ffmpeg para re-leer el stream de subtítulos.
	InputPath string
}

// IsImageSubtitleCodec reporta si el codec es un formato de subtítulos
// bitmap que debe componerse sobre el frame de video.
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

// IsStyledTextSubtitleCodec reporta si el codec es subtítulo de texto
// con estilos (ASS/SSA) que el navegador no renderiza fielmente.
// Se queman en transcode time igual que Plex/Jellyfin.
func IsStyledTextSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass", "ssa":
		return true
	default:
		return false
	}
}

// IsBurnableSubtitleCodec es la unión: un subtítulo de este codec no
// se renderiza nativamente en web y debe quemarse. El frontend usa
// el mismo check para decidir si elegir un sub dispara restart de
// sesión (burn-in) o switch de track HLS (SRT/WebVTT).
func IsBurnableSubtitleCodec(codec string) bool {
	return IsImageSubtitleCodec(codec) || IsStyledTextSubtitleCodec(codec)
}

// ffmpegInputPathEscape escapa caracteres especiales dentro de la
// sintaxis del filtro `subtitles` de ffmpeg. El path va sin comillas
// envolventes — el caller lo embebe como `subtitles='<escaped>'`.
func ffmpegInputPathEscape(path string) string {
	// Backslash primero para no duplicar los escapes que añadimos después.
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
