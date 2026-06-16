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
