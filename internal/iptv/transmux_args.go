package iptv

// ffmpeg argv builders for the transmux session manager.
//
// Two pipelines:
//   - direct (`-c copy`): byte-level repackaging, near-zero CPU. Used
//     for the vast majority of Xtream feeds whose codec already fits
//     HLS.
//   - reencode: codec-rescue path for upstreams whose codec / container
//     combination doesn't survive `-c copy`. Optionally hardware-
//     accelerated when the host has VAAPI / NVENC / QSV / VideoToolbox.
//
// The mode-aware dispatch + per-encoder tuning live here so the
// session lifecycle code in transmux.go stays focused on goroutine
// orchestration.

import (
	"path/filepath"
)

// defaultTransmuxUserAgent is the UA we send upstream when the caller
// doesn't override it. Many Xtream Codes panels gate on UA and serve
// either an HTML error page or a different codec to the default
// `Lavf/<version>` ffmpeg sends, which manifests downstream as the
// dreaded "Invalid data found when processing input" / exit status 8.
// Mirroring the prober's UA keeps both planes consistent.
const defaultTransmuxUserAgent = "VLC/3.0.20 LibVLC/3.0.20"

// buildTransmuxFFmpegArgs constructs the argv for the default direct
// (`-c copy`) pipeline. Thin wrapper around the mode-aware variant so
// callers / tests that only care about the fast path don't thread an
// extra parameter.
func buildTransmuxFFmpegArgs(upstreamURL, workDir, userAgent string) []string {
	return buildTransmuxFFmpegArgsForMode(upstreamURL, workDir, userAgent, "", nil, decodeModeDirect)
}

// buildTransmuxFFmpegArgsForMode dispatches to the mode-specific argv
// builder. `direct` is the cheap path (`-c copy`, near-zero CPU);
// `reencode` is the codec-rescue path that turns whatever the upstream
// sends into H.264 + AAC at the lowest CPU preset.
//
// Common flags (input shaping, reconnection, HLS window) live in
// commonTransmuxArgs / hlsOutputArgs so the two modes diverge only on
// the codec section, which is the actually-different decision.
//
// `encoder` and `hwAccelInputArgs` only apply to the reencode path:
// pass "" / nil for direct and they're ignored. Direct mode never
// decodes (`-c copy` is byte-level repackaging), so a `-hwaccel` flag
// would be both pointless and risky on certain backends that demand
// `-hwaccel_output_format`.
func buildTransmuxFFmpegArgsForMode(upstreamURL, workDir, userAgent, encoder string, hwAccelInputArgs []string, mode decodeMode) []string {
	if mode == decodeModeReencode {
		return buildReencodeArgs(upstreamURL, workDir, userAgent, encoder, hwAccelInputArgs)
	}
	return buildDirectArgs(upstreamURL, workDir, userAgent)
}

// commonTransmuxArgs returns the input-side ffmpeg flags shared by
// both decode modes (input URL, reconnection, buffering, UA).
//
// Reconnection: `-reconnect_at_eof 1` + `-reconnect_streamed 1` make
// a flaky upstream recover without ffmpeg exiting; the session manager
// only re-spawns on a hard exit.
//
// Buffering: `-rtbufsize 50M` absorbs upstream jitter (~50 MB RAM per
// active session) so we don't drop packets when input arrives faster
// than the muxer can drain. `-max_delay 5000000` (5 s) gives the
// demuxer slack for reordered packets — without it, noisy providers
// produce "non-monotonic DTS" warnings and segment dropouts.
//
// `-user_agent` matters more than it looks. Many Xtream Codes panels
// gate on UA: with the default `Lavf/<version>` ffmpeg sends they
// return an HTML error page (decoded as "Invalid data" → exit 8) or
// a codec profile that doesn't survive `-c copy`. Mirroring the
// prober's `VLC/3.0.20` UA is the same workaround every IPTV player
// ships.
func commonTransmuxArgs(upstreamURL, workDir, userAgent string) []string {
	if userAgent == "" {
		userAgent = defaultTransmuxUserAgent
	}
	return []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-nostdin",
		"-fflags", "+genpts+discardcorrupt",
		"-user_agent", userAgent,
		"-rtbufsize", "50M",
		"-max_delay", "5000000",
		"-reconnect", "1",
		"-reconnect_at_eof", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-rw_timeout", "10000000", // 10 s I/O timeout in microseconds
		"-i", upstreamURL,
	}
}

// hlsOutputArgs returns the ffmpeg flags that select the HLS muxer +
// sliding-window settings, identical for both decode modes.
//
// HLS window choices (tuned for live Xtream playback):
//   - `hls_time 2` — short segments halve buffer-underrun recovery
//     time and reduce live latency.
//   - `hls_list_size 20` — 40 s manifest window absorbs the
//     ~10 s background-tab stalls Chrome / Firefox produce without
//     the player falling out of range.
//   - `hls_delete_threshold 5` — keep 5 extra segments past the tail
//     so a slow client whose manifest parse cycle is behind can still
//     fetch what it asked for instead of getting 404.
//   - `+temp_file` flag writes each segment to `.tmp` first and
//     atomically renames into place. Without it, http.ServeFile can
//     serve a partially-written `.ts`, triggering bufferStalledError.
//   - `omit_endlist` keeps the manifest live (no EXT-X-ENDLIST).
//     `delete_segments` keeps disk usage bounded.
func hlsOutputArgs(workDir string) []string {
	return []string{
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "20",
		"-hls_delete_threshold", "5",
		"-hls_flags", "delete_segments+independent_segments+omit_endlist+program_date_time+temp_file",
		"-hls_segment_type", "mpegts",
		"-hls_segment_filename", filepath.Join(workDir, "seg-%05d.ts"),
		"-hls_allow_cache", "0",
		filepath.Join(workDir, "index.m3u8"),
	}
}

func buildDirectArgs(upstreamURL, workDir, userAgent string) []string {
	args := commonTransmuxArgs(upstreamURL, workDir, userAgent)
	args = append(args,
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-c", "copy",
		"-bsf:v", "h264_mp4toannexb",
	)
	return append(args, hlsOutputArgs(workDir)...)
}

// buildReencodeArgs is the codec-rescue path for upstreams whose
// codec / container combination doesn't survive `-c copy`. We still
// pass through audio when it's already AAC (cheap) and only fall
// back to a video re-encode (the part that actually costs CPU).
//
// `encoder` selects the output video encoder ("libx264" for software,
// "h264_nvenc" / "h264_vaapi" / "h264_qsv" / "h264_videotoolbox" for
// hardware). Empty defaults to libx264. `hwAccelInputArgs` are the
// matching `-hwaccel ...` flags that go before `-i` so the decoder
// runs on the same accelerator — without those, ffmpeg would decode
// in software and only encode on the GPU, losing most of the gain.
//
// Per-encoder tuning: libx264 wants -preset/-tune; the hardware
// encoders use their own preset names. Mirroring the VOD transcoder
// pattern in internal/stream/transcode.go.BuildFFmpegArgs.
func buildReencodeArgs(upstreamURL, workDir, userAgent, encoder string, hwAccelInputArgs []string) []string {
	if encoder == "" {
		encoder = "libx264"
	}
	args := commonTransmuxArgs(upstreamURL, workDir, userAgent)
	// Splice -hwaccel BEFORE the trailing -i URL (which
	// commonTransmuxArgs appended last). The decode-side flag must
	// precede its input or ffmpeg ignores it.
	args = insertBeforeInput(args, hwAccelInputArgs)
	args = append(args,
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-c:v", encoder,
	)
	args = append(args, encoderTuningArgs(encoder)...)
	args = append(args,
		// Keyframe interval = HLS segment duration × frame rate.
		// hls_time=2, assume 24 fps → 48-frame GOP. -sc_threshold 0
		// disables scenecut so segments stay aligned to GOP
		// boundaries (HLS players require segments to start with an
		// IDR; a stray scenecut produces partial segments).
		"-g", "48",
		"-sc_threshold", "0",
		// Audio: re-encode to AAC stereo. Most Xtream feeds carry
		// AAC LC already which would survive `-c:a copy`, but mixing
		// copy + transcode video produces a/v desync for a few seconds
		// at startup; AAC re-encode is cheap (~1% CPU) and avoids it.
		"-c:a", "aac",
		"-ac", "2",
		"-b:a", "128k",
	)
	return append(args, hlsOutputArgs(workDir)...)
}

// encoderTuningArgs returns encoder-specific quality / latency flags.
// Each hardware encoder has its own preset vocabulary, so the libx264
// "veryfast / zerolatency" doesn't translate directly. Defaults are
// chosen for live transcode (low latency, "good enough" quality) —
// not for archive or top-bitrate masters.
//
// libx264:
//   - veryfast preset, zerolatency tune. Standard Jellyfin / Threadfin
//     trade-off — ~10-20% of one core for 1080p H.264 → H.264.
//   - keyint=48 + scenecut=0 forces predictable GOP boundaries, which
//     -g/-sc_threshold also do globally; we keep both for clarity.
//   - Main profile / Level 4.0 covers every browser + Chromecast.
//
// h264_nvenc:
//   - p4 preset is NVIDIA's "medium" tier under the new perf ladder
//     (p1=fastest/lowest-quality, p7=slowest/highest); p4 matches the
//     CPU/quality trade-off of libx264 veryfast.
//   - tune ll = "low latency". Without this NVENC defaults to high
//     quality with B-frames → adds startup delay we don't want here.
//
// h264_vaapi:
//   - VAAPI exposes a coarse `quality` knob (1-7, lower is faster).
//     `quality 4` is "balanced".
//   - We don't set `-bsf:v h264_mp4toannexb` because VAAPI emits
//     Annex-B natively for HLS.
//
// h264_qsv:
//   - QSV's preset names mirror libx264 (`veryfast`, `fast`, …).
//   - look_ahead=0 disables Intel's lookahead which adds latency.
//
// h264_videotoolbox:
//   - macOS-only. allow_sw=0 forces hardware path; we'd rather fail
//     than silently fall back when the operator asked for VT.
//   - realtime=1 picks the low-latency rate-control mode.
func encoderTuningArgs(encoder string) []string {
	switch encoder {
	case "h264_nvenc":
		return []string{
			"-preset", "p4",
			"-tune", "ll",
			"-rc", "cbr",
			"-pix_fmt", "yuv420p",
			"-profile:v", "main",
			"-level", "4.0",
		}
	case "h264_vaapi":
		return []string{
			"-quality", "4",
			"-profile:v", "main",
			"-level", "40",
		}
	case "h264_qsv":
		return []string{
			"-preset", "veryfast",
			"-look_ahead", "0",
			"-pix_fmt", "yuv420p",
			"-profile:v", "main",
			"-level", "40",
		}
	case "h264_videotoolbox":
		return []string{
			"-allow_sw", "0",
			"-realtime", "1",
			"-pix_fmt", "yuv420p",
			"-profile:v", "main",
			"-level", "4.0",
		}
	default: // libx264 + any unknown
		return []string{
			"-preset", "veryfast",
			"-tune", "zerolatency",
			"-pix_fmt", "yuv420p",
			"-profile:v", "main",
			"-level", "4.0",
			"-x264-params", "keyint=48:min-keyint=48:scenecut=0",
		}
	}
}

// insertBeforeInput returns a copy of args with `extra` inserted just
// before the trailing `-i <url>` pair that commonTransmuxArgs appends.
// Falls back to appending if no `-i` is found (defensive — should
// not happen in practice, but a panic on argv construction would
// take the whole transmux subsystem down).
func insertBeforeInput(args, extra []string) []string {
	if len(extra) == 0 {
		return args
	}
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-i" {
			out := make([]string, 0, len(args)+len(extra))
			out = append(out, args[:i]...)
			out = append(out, extra...)
			out = append(out, args[i:]...)
			return out
		}
	}
	return append(args, extra...)
}
