package stream

import "strings"

// BurnSubtitleSpec describes a subtitle stream the transcoder should
// render directly into the video frames ("burn in") instead of
// surfacing as a sidecar track on the HLS manifest.
//
// Why this exists: a non-trivial slice of self-hosted libraries — every
// Blu-ray rip with its native subs, every anime release with .ass
// styled subs — ships subtitles in formats that no browser will render
// natively. The legacy code path only knew how to extract sub streams
// as WebVTT (text); image-based PGS / VOBSUB and styled ASS / SSA
// silently went missing from playback because the conversion is
// impossible / lossy. Burning the picked subtitle into the video at
// transcode time matches what Plex and Jellyfin do for the same case.
//
// The session-key consequence: a burn-in choice is *baked into the
// transcoded segments*, so changing the subtitle mid-playback requires
// a fresh transcode session — same constraint as switching audio
// tracks. See sessionKey() for the keying contract.
type BurnSubtitleSpec struct {
	// Index is the 0-based per-type subtitle stream index ffmpeg
	// addresses as `0:s:N`. Same convention as the existing
	// AudioStreamIndex field — NOT the absolute stream id. The
	// caller is responsible for mapping a DB MediaStream row
	// (filtered to type='subtitle') to this index.
	Index int
	// Codec is the lowercase ffmpeg codec name for the chosen
	// subtitle stream (e.g. "hdmv_pgs_subtitle", "dvd_subtitle",
	// "ass", "ssa"). Drives the filter strategy:
	//   - image formats → filter_complex overlay
	//   - text + styled (ass/ssa) → subtitles= filter (text-render
	//     into the video frames)
	Codec string
	// InputPath is the absolute path of the source media file, used
	// by the `subtitles` ffmpeg filter to re-read the subtitle
	// stream alongside the video. Required for ASS / SSA burn-in,
	// unused for the overlay path (which references the input
	// stream by 0:s:N inside filter_complex). Threaded through so
	// BuildFFmpegArgs doesn't need to know the call site's input
	// resolution logic.
	InputPath string
}

// IsImageSubtitleCodec reports whether codec is a bitmap subtitle
// format that must be composited over the video frame (no text to
// render natively).
//
// Recognised: HDMV PGS (Blu-ray), DVD VobSub, the DVB equivalents.
// The matrix matches what ffmpeg reports via `-codec` for sub
// streams in commodity rips. Case-insensitive — providers emit a
// mix of "pgs" and "hdmv_pgs_subtitle" depending on the demuxer.
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

// IsStyledTextSubtitleCodec reports whether codec is a styled-text
// subtitle format (ASS / SSA). These are technically text but carry
// inline styling (fonts, colours, positions, animations) that no
// browser sub renderer reproduces faithfully. The pragmatic choice
// — matching Plex/Jellyfin — is to burn them in at transcode time
// using ffmpeg's `subtitles` filter.
func IsStyledTextSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass", "ssa":
		return true
	default:
		return false
	}
}

// IsBurnableSubtitleCodec is the union: a subtitle of this codec
// can't render natively in a web player and must be burned in. The
// frontend uses the same check to decide whether picking a sub
// triggers a transcode-session restart (burn-in) or a plain HLS
// sub-track switch (SRT, WebVTT — which we extract as VTT and add
// to the manifest, no transcode disruption).
func IsBurnableSubtitleCodec(codec string) bool {
	return IsImageSubtitleCodec(codec) || IsStyledTextSubtitleCodec(codec)
}

// ffmpegInputPathEscape escapes characters that are special inside
// ffmpeg's `subtitles` filter argument syntax. The filter receives
// the file path as part of a filter-graph string, where unescaped
// `:` is the option separator and `\` / `'` / `[` / `]` / `,`
// terminate or restart filter expressions.
//
// ffmpeg's documented escaping rules require ' \ : to be quoted by
// prefixing with a backslash; the whole path then sits inside
// single-quotes so Windows backslashes and spaces survive.
// Returns the path WITHOUT surrounding quotes — the caller embeds
// it as `subtitles='<escaped>'` so the quotes are part of the
// filter-string, not the path argument.
func ffmpegInputPathEscape(path string) string {
	// Order matters: backslash first so the escapes we add below
	// don't get themselves doubled.
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
