package torrentstream

import "path/filepath"

// buildVODRemuxArgs builds the ffmpeg argv for the remux path: repackage a
// browser-decodable H.264 stream from `inputURL` into a growing HLS playlist
// in `workDir`, copying video byte-for-byte (near-zero CPU). Audio is copied
// when it's already AAC and cheaply re-encoded to AAC otherwise (AC3/DTS/… do
// not play in every browser).
//
// Unlike the IPTV transmux (live, sliding window) this is VOD over our own
// localhost torrent reader:
//   - no upstream UA spoofing / reconnect flags — the source is in-process;
//   - `hls_playlist_type event` + `hls_list_size 0` keep every segment so the
//     player can seek across the whole title as it transcodes;
//   - the input reader blocks until the needed pieces land (SetResponsive),
//     so ffmpeg naturally paces with the download.
func buildVODRemuxArgs(inputURL, workDir string, audioNeedsTranscode bool) []string {
	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-nostdin",
		"-fflags", "+genpts",
		"-i", inputURL,
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-c:v", "copy",
	}
	if audioNeedsTranscode {
		args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "160k")
	} else {
		args = append(args, "-c:a", "copy")
	}
	args = append(args, vodHLSOutputArgs(workDir)...)
	return args
}

// buildVODReencodeArgs is the codec-rescue path (P1b-2): video the browser
// can't decode (HEVC/AV1/XviD…) is transcoded to H.264 + AAC into HLS.
// `encoder` selects the output encoder ("libx264" software, or
// "h264_vaapi"/"h264_nvenc"/"h264_qsv"/"h264_videotoolbox"); `hwAccelInputArgs`
// are the matching `-hwaccel …` decode-side flags that MUST precede `-i` so
// the decoder runs on the same accelerator. Mirrors the IPTV reencode path.
func buildVODReencodeArgs(inputURL, workDir, encoder string, hwAccelInputArgs []string) []string {
	if encoder == "" {
		encoder = "libx264"
	}
	args := []string{"-hide_banner", "-loglevel", "warning", "-nostdin", "-fflags", "+genpts"}
	args = append(args, hwAccelInputArgs...) // decode-side flags BEFORE -i
	args = append(args, "-i", inputURL, "-map", "0:v:0", "-map", "0:a:0?", "-c:v", encoder)
	args = append(args, vodEncoderTuningArgs(encoder)...)
	// VAAPI encodes from GPU memory: upload the decoded frames first.
	if encoder == "h264_vaapi" {
		args = append(args, "-vf", "format=nv12,hwupload")
	}
	args = append(args,
		// GOP = HLS segment seconds × fps (hls_time 4, assume 24fps → 96),
		// scenecut off so segments align to IDR boundaries.
		"-g", "96",
		"-sc_threshold", "0",
		// Re-encode audio to AAC: mixing a copied audio track with a
		// transcoded video desyncs at startup; AAC encode is ~1% CPU.
		"-c:a", "aac", "-ac", "2", "-b:a", "160k",
	)
	return append(args, vodHLSOutputArgs(workDir)...)
}

// vodEncoderTuningArgs returns encoder-specific quality flags for the VOD
// reencode path (a touch higher quality than the IPTV live preset since VOD
// isn't latency-bound). Mirrors the encoder vocabulary in internal/stream and
// the IPTV transmux.
func vodEncoderTuningArgs(encoder string) []string {
	switch encoder {
	case "h264_nvenc":
		return []string{"-preset", "p4", "-rc", "vbr", "-pix_fmt", "yuv420p", "-profile:v", "main", "-level", "4.0"}
	case "h264_vaapi":
		return []string{"-quality", "4", "-profile:v", "main", "-level", "40"}
	case "h264_qsv":
		return []string{"-preset", "fast", "-pix_fmt", "yuv420p", "-profile:v", "main", "-level", "40"}
	case "h264_videotoolbox":
		return []string{"-allow_sw", "0", "-pix_fmt", "yuv420p", "-profile:v", "main", "-level", "4.0"}
	default: // libx264 + unknown
		return []string{"-preset", "veryfast", "-pix_fmt", "yuv420p", "-profile:v", "main", "-level", "4.0"}
	}
}

// vodHLSOutputArgs selects the HLS muxer tuned for VOD-while-downloading: an
// event playlist that keeps all segments (seekable), atomic segment writes
// (`temp_file`) so a half-written `.ts` is never served, and mpegts segments
// (the only container that carries copied H.264 + AAC for HLS).
func vodHLSOutputArgs(workDir string) []string {
	return []string{
		"-f", "hls",
		"-hls_time", "4",
		"-hls_playlist_type", "event",
		"-hls_list_size", "0",
		"-hls_flags", "independent_segments+temp_file",
		"-hls_segment_type", "mpegts",
		"-hls_segment_filename", filepath.Join(workDir, "seg-%05d.ts"),
		filepath.Join(workDir, "index.m3u8"),
	}
}
