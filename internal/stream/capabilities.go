package stream

import (
	"net/http"
	"strings"
)

// Capabilities is the set of codecs and containers a client can decode
// natively. The server uses it to decide whether to send the file as-is
// (DirectPlay), remux it (DirectStream), or transcode (Transcode).
//
// Empty fields mean "I haven't told you about this category" — the
// server treats that as "use the conservative web-browser default" so
// older clients that don't send the header still work unchanged.
//
// Why a struct of sets and not just three slices: the decision code
// looks up by codec/container name on every call (DirectPlay check is
// 3 map hits per item per request). The parser builds the maps once.
type Capabilities struct {
	VideoCodecs map[string]bool
	AudioCodecs map[string]bool
	Containers  map[string]bool
}

// HeaderCapabilities is the request header clients send to declare what
// they can decode. Format is the typical semicolon-separated key=value-list
// pattern (same shape as Accept-CH, Vary, etc.):
//
//	X-Hubplay-Client-Capabilities: video=h264,h265,vp9,av1; audio=aac,opus,eac3; container=mp4,mkv,ts
//
// Tokens are lower-cased and trimmed; unknown keys are silently ignored
// so adding a future "subtitle=srt,ass" field is non-breaking.
const HeaderCapabilities = "X-Hubplay-Client-Capabilities"

// ParseCapabilitiesHeader parses the header value into a Capabilities
// struct. Returns nil when the header is empty or has no recognised
// keys — callers should treat nil as "use defaults" (see DefaultWebCapabilities).
//
// The parser is forgiving on whitespace / case / order; pretty much any
// sensible client formatting decodes the same way. The forbidden fruit
// is silently dropping a malformed segment instead of returning an
// error: a request that says "video=h264,zh265" (typo) still gets the
// h264 entry and the typo'd one is just unknown — the decision falls
// through normally.
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
		var dst *map[string]bool
		switch key {
		case "video":
			dst = &caps.VideoCodecs
		case "audio":
			dst = &caps.AudioCodecs
		case "container":
			dst = &caps.Containers
		default:
			continue // forward-compat: unknown keys ignored
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
	if len(caps.VideoCodecs) == 0 && len(caps.AudioCodecs) == 0 && len(caps.Containers) == 0 {
		return nil
	}
	return caps
}

// CapabilitiesFromRequest reads the header off an HTTP request and
// parses it. Wrapper for the common case so handlers stay one line.
func CapabilitiesFromRequest(r *http.Request) *Capabilities {
	if r == nil {
		return nil
	}
	return ParseCapabilitiesHeader(r.Header.Get(HeaderCapabilities))
}

// DefaultWebCapabilities is the fallback used when a client doesn't
// declare anything (legacy web client, request from the in-browser
// player today). Mirrors the codec sets the original Decide() function
// hard-coded — keep these in sync if either side moves; the original
// constants in decision.go now reference these maps.
//
// Picking these as the default rather than "nothing" means:
//
//   - Today's web player keeps working without sending the header.
//   - A future Kotlin TV app that DOES send the header gets to declare
//     hevc / eac3 / dolby / hdr / mkv etc. and direct-stream where the
//     web defaults would have transcoded.
//
// The decision code intersects the item's actual codecs with the
// effective Capabilities (declared OR default). That intersection IS
// the win — it's the difference between "burn CPU re-encoding HEVC to
// H.264 for a Chromecast that can decode HEVC natively" and "send the
// HEVC file as-is".
func DefaultWebCapabilities() *Capabilities {
	return &Capabilities{
		VideoCodecs: map[string]bool{"h264": true, "vp8": true, "vp9": true, "av1": true},
		AudioCodecs: map[string]bool{"aac": true, "mp3": true, "opus": true, "vorbis": true, "flac": true},
		Containers:  map[string]bool{"mp4": true, "webm": true, "mov": true},
	}
}

// effectiveCapabilities returns the caps to use for a decision: the
// declared set when present, the web default when nil. Pulled out so
// the Decide() body reads as one expression.
func effectiveCapabilities(c *Capabilities) *Capabilities {
	if c == nil {
		return DefaultWebCapabilities()
	}
	// Partial declarations: a client that sends only `video=...`
	// without `audio=...` should still get sensible audio defaults
	// rather than failing every audio check. Backfill missing buckets
	// from the web default so the intersection logic stays simple.
	out := &Capabilities{
		VideoCodecs: c.VideoCodecs,
		AudioCodecs: c.AudioCodecs,
		Containers:  c.Containers,
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
	return out
}
