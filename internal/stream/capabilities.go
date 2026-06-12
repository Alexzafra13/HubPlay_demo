package stream

import (
	"net/http"
	"strconv"
	"strings"
)

// Capabilities es el conjunto de codecs y containers que un cliente
// puede decodificar nativamente. El servidor lo usa para decidir
// DirectPlay / DirectStream / Transcode.
//
// Campos vacíos significan "no declarado" — se usa el default
// conservador de navegador web para compatibilidad con clientes legacy.
type Capabilities struct {
	VideoCodecs map[string]bool
	AudioCodecs map[string]bool
	Containers  map[string]bool
	// HDRFormats lista formatos HDR que el cliente renderiza nativamente.
	// Tokens: "hdr10", "hlg", "dovi" ("dolbyvision" también aceptado).
	// Vacío/nil = "solo SDR" — el servidor hará tonemap a BT.709.
	HDRFormats map[string]bool
	// MaxAudioChannels es el máximo de canales que el cliente puede
	// decodificar (`channels=6`). 0 = no declarado → downmix a estéreo,
	// el comportamiento histórico. PB-22 (audit 2026-06-10).
	MaxAudioChannels int
}

// HeaderCapabilities es el header HTTP que los clientes envían para
// declarar sus capacidades de decodificación.
const HeaderCapabilities = "X-Hubplay-Client-Capabilities"

// ParseCapabilitiesHeader parsea el valor del header en un struct
// Capabilities. Devuelve nil si está vacío o sin claves reconocidas.
func ParseCapabilitiesHeader(value string) *Capabilities {
	if value == "" {
		return nil
	}
	caps := &Capabilities{}
	for _, segment := range strings.Split(value, ";") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		eq := strings.IndexByte(segment, '=')
		if eq <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(segment[:eq]))
		raw := segment[eq+1:]
		if key == "channels" {
			if v, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && v > 0 {
				caps.MaxAudioChannels = v
			}
			continue
		}
		var dst *map[string]bool
		switch key {
		case "video":
			dst = &caps.VideoCodecs
		case "audio":
			dst = &caps.AudioCodecs
		case "container":
			dst = &caps.Containers
		case "hdr":
			dst = &caps.HDRFormats
		default:
			continue // claves desconocidas se ignoran (forward-compat)
		}
		if *dst == nil {
			*dst = make(map[string]bool)
		}
		for _, tok := range strings.Split(raw, ",") {
			tok = strings.ToLower(strings.TrimSpace(tok))
			if tok != "" {
				(*dst)[tok] = true
			}
		}
	}
	if len(caps.VideoCodecs) == 0 && len(caps.AudioCodecs) == 0 && len(caps.Containers) == 0 && len(caps.HDRFormats) == 0 && caps.MaxAudioChannels == 0 {
		return nil
	}
	return caps
}

// CapabilitiesFromRequest lee el header de un HTTP request y lo parsea.
func CapabilitiesFromRequest(r *http.Request) *Capabilities {
	if r == nil {
		return nil
	}
	return ParseCapabilitiesHeader(r.Header.Get(HeaderCapabilities))
}

// DefaultWebCapabilities es el fallback cuando el cliente no declara
// nada (web player legacy). HDRFormats vacío intencionalmente: HDR en
// navegador depende de OS/display/GPU/driver — mejor tonemap a BT.709.
// MaxAudioChannels=2: sin declaración explícita, downmix a estéreo.
func DefaultWebCapabilities() *Capabilities {
	return &Capabilities{
		VideoCodecs:      map[string]bool{"h264": true, "vp8": true, "vp9": true, "av1": true},
		AudioCodecs:      map[string]bool{"aac": true, "mp3": true, "opus": true, "vorbis": true, "flac": true},
		Containers:       map[string]bool{"mp4": true, "webm": true, "mov": true},
		HDRFormats:       map[string]bool{},
		MaxAudioChannels: 2,
	}
}

// effectiveCapabilities devuelve las caps a usar: las declaradas si
// existen, o el default web. Buckets parciales se rellenan desde el
// default para que la intersección funcione sin checks especiales.
func effectiveCapabilities(c *Capabilities) *Capabilities {
	if c == nil {
		return DefaultWebCapabilities()
	}
	out := &Capabilities{
		VideoCodecs:      c.VideoCodecs,
		AudioCodecs:      c.AudioCodecs,
		Containers:       c.Containers,
		HDRFormats:       c.HDRFormats,
		MaxAudioChannels: c.MaxAudioChannels,
	}
	def := DefaultWebCapabilities()
	if out.VideoCodecs == nil {
		out.VideoCodecs = def.VideoCodecs
	}
	if out.AudioCodecs == nil {
		out.AudioCodecs = def.AudioCodecs
	}
	if out.Containers == nil {
		out.Containers = def.Containers
	}
	// HDRFormats NO hereda del default — un cliente que declaró buckets
	// pero no `hdr=` está diciendo "no soporto HDR".
	if out.HDRFormats == nil {
		out.HDRFormats = def.HDRFormats
	}
	// Mismo razonamiento que HDR: sin `channels=` explícito, estéreo.
	if out.MaxAudioChannels <= 0 {
		out.MaxAudioChannels = def.MaxAudioChannels
	}
	return out
}
