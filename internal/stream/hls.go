package stream

import (
	"fmt"
	"strings"
)

// GenerateMasterPlaylist crea un playlist HLS master (M3U8) con
// múltiples niveles de calidad para streaming adaptativo.
//
// `audioStreamIndex` >= 0 → embed en cada variant URL para mantener
// el dub al cambiar de calidad. `burnSubIndex` >= 0 → lleva la
// elección de subtítulo quemado a cada variant URL.
//
// `sourceHeight` > 0 filtra las variantes que upscalearían: anunciar
// 1080p para una fuente 480p hace que hls.js "suba de calidad" a un
// re-encode inflado que quema un slot de transcode por nada. Si el
// filtro dejara la lista vacía (fuente más pequeña que el perfil
// mínimo), se conserva el perfil más bajo. 0 = sin datos, no filtra.
// PB-10 (audit 2026-06-10).
func GenerateMasterPlaylist(itemID, baseURL string, profiles []string, audioStreamIndex, burnSubIndex, sourceHeight int) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")

	// Construye el sufijo query una vez. Orden estable para cache keys.
	params := make([]string, 0, 2)
	if audioStreamIndex >= 0 {
		params = append(params, fmt.Sprintf("audio=%d", audioStreamIndex))
	}
	if burnSubIndex >= 0 {
		params = append(params, fmt.Sprintf("subtitle=%d", burnSubIndex))
	}
	suffix := ""
	if len(params) > 0 {
		suffix = "?" + strings.Join(params, "&")
	}

	for _, name := range eligibleProfiles(profiles, sourceHeight) {
		p := Profiles[name]
		bandwidth := parseBitrate(p.VideoBitrate) + parseBitrate(p.AudioBitrate)

		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,FRAME-RATE=%d,NAME=\"%s\"\n",
			bandwidth, p.Width, p.Height, p.MaxFrameRate, p.Name)
		fmt.Fprintf(&b, "%s/api/v1/stream/%s/%s/index.m3u8%s\n", baseURL, itemID, name, suffix)
	}

	return b.String()
}

// eligibleProfiles devuelve los nombres de perfil válidos para el
// master playlist: existentes, no "original", y — con sourceHeight
// conocido — sin variantes que upscalearían. Garantiza al menos un
// perfil (el más bajo) cuando el filtro vaciaría la lista.
func eligibleProfiles(profiles []string, sourceHeight int) []string {
	var out []string
	lowest := ""
	lowestHeight := 0
	for _, name := range profiles {
		p, ok := Profiles[name]
		if !ok || name == "original" {
			continue
		}
		if lowest == "" || p.Height < lowestHeight {
			lowest, lowestHeight = name, p.Height
		}
		if sourceHeight > 0 && p.Height > sourceHeight {
			continue
		}
		out = append(out, name)
	}
	if len(out) == 0 && lowest != "" {
		return []string{lowest}
	}
	return out
}

// SynthesizeVODManifest construye un playlist HLS VOD completo,
// listando todos los segmentos de 0 a N-1 donde N = ceil(duración/segmento).
// Declara `#EXT-X-PLAYLIST-TYPE:VOD` + `#EXT-X-ENDLIST` para que hls.js
// permita seek libre en toda la timeline.
//
// Los ficheros de segmento no necesitan existir en disco aún; el handler
// arranca ffmpeg al offset correcto cuando se solicita un segmento
// no codificado.
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

	// `#EXT-X-TARGETDURATION` DEBE ser >= toda duración de segmento.
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

// ParseBitrate convierte un string como "4000k" o "2M" a bits por segundo.
// Exportado porque el handler de streaming federado lo necesita para
// calcular BANDWIDTH= en su master playlist.
func ParseBitrate(s string) int { return parseBitrate(s) }

// parseBitrate convierte un string como "4000k" a bits por segundo.
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
