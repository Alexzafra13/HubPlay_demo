package iptv

// Codec-error classifier for transmux. The processWatcher feeds the
// captured stderr tail through looksLikeCodecError; when it matches,
// the manager promotes the channel into reencode mode for the next
// spawn (see TransmuxManager.promoteToReencode). Kept in its own file
// so the regex + the reasoning behind it are easy to audit and tune
// without scrolling past lifecycle code.

import "regexp"

// codecErrorPattern matches the stderr fragments ffmpeg emits when a
// `-c copy` pipeline can't repackage the upstream — typically a codec
// the H264 bitstream filter rejects, an audio profile that won't fit
// the destination container, or a missing PMT entry. Matched on the
// captured stderr tail so we promote to reencode mode only when the
// upstream is reachable enough to negotiate codecs.
//
// Patterns are kept conservative: we'd rather miss a few promotion
// opportunities than burn CPU re-encoding a TCP-refused dead host.
// The breaker handles the latter cleanly already.
var codecErrorPattern = regexp.MustCompile(
	`(?i)(invalid data found|could not find codec parameters|h264_mp4toannexb|hevc.*not.*supported|non-monotonic dts.*aborting|stream specifier.*matches no streams|codec not currently supported|" hevc"| ac3 |eac3|hevc_mp4toannexb|invalid nal unit size)`,
)

// looksLikeCodecError reports whether the captured stderr tail
// resembles a codec-incompatibility crash (vs. a network / auth
// problem). Empty tail returns false — we never promote on no
// evidence.
func looksLikeCodecError(stderrTail string) bool {
	if stderrTail == "" {
		return false
	}
	return codecErrorPattern.MatchString(stderrTail)
}
