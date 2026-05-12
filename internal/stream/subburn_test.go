package stream

import "testing"

// TestIsImageSubtitleCodec pins which ffmpeg codec strings are
// recognised as bitmap subs. The frontend reads the same list (mapped
// in TypeScript) and a silent drift between the two would mean a
// subtitle that the backend WOULD burn in is never offered to the
// user in the picker.
func TestIsImageSubtitleCodec(t *testing.T) {
	yes := []string{
		"hdmv_pgs_subtitle", "pgs",
		"dvd_subtitle", "dvdsub",
		"dvb_subtitle", "dvbsub",
		"xsub",
		// Case + whitespace robustness — providers vary.
		"HDMV_PGS_SUBTITLE", "  pgs  ",
	}
	no := []string{
		"subrip", "srt",
		"webvtt", "vtt",
		"ass", "ssa", // styled text — handled by IsStyledTextSubtitleCodec
		"mov_text",
		"", "unknown",
	}
	for _, c := range yes {
		if !IsImageSubtitleCodec(c) {
			t.Errorf("IsImageSubtitleCodec(%q) = false, want true", c)
		}
	}
	for _, c := range no {
		if IsImageSubtitleCodec(c) {
			t.Errorf("IsImageSubtitleCodec(%q) = true, want false", c)
		}
	}
}

func TestIsStyledTextSubtitleCodec(t *testing.T) {
	for _, c := range []string{"ass", "ssa", "ASS", "SSA"} {
		if !IsStyledTextSubtitleCodec(c) {
			t.Errorf("IsStyledTextSubtitleCodec(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"subrip", "webvtt", "pgs", "hdmv_pgs_subtitle", ""} {
		if IsStyledTextSubtitleCodec(c) {
			t.Errorf("IsStyledTextSubtitleCodec(%q) = true, want false", c)
		}
	}
}

func TestIsBurnableSubtitleCodec(t *testing.T) {
	// Image + styled → burnable. Plain text formats → NOT burnable
	// (they ride as native HLS sub tracks).
	burn := []string{
		"hdmv_pgs_subtitle", "pgs", "dvd_subtitle", "dvdsub",
		"ass", "ssa",
	}
	skip := []string{"subrip", "srt", "webvtt", "vtt", "mov_text"}
	for _, c := range burn {
		if !IsBurnableSubtitleCodec(c) {
			t.Errorf("IsBurnableSubtitleCodec(%q) = false, want true", c)
		}
	}
	for _, c := range skip {
		if IsBurnableSubtitleCodec(c) {
			t.Errorf("IsBurnableSubtitleCodec(%q) = true, want false", c)
		}
	}
}

// TestFFmpegInputPathEscape covers the path characters that would
// otherwise break filter-graph parsing. Windows-style backslashes
// and Unix-style colons both have to survive intact.
func TestFFmpegInputPathEscape(t *testing.T) {
	cases := map[string]string{
		"/mnt/media/movie.mkv":           `/mnt/media/movie.mkv`,
		`C:\media\movie.mkv`:             `C\:\\media\\movie.mkv`,
		`/mnt/path with spaces/file.mkv`: `/mnt/path with spaces/file.mkv`,
		// A colon must be escaped so subtitles=filename=/a:b:si=0
		// is parsed as filename='/a:b' rather than truncated at /a.
		`/mnt/x:y.mkv`: `/mnt/x\:y.mkv`,
		// Brackets, commas, and quotes terminate filter expressions.
		`/mnt/[weird],path/'movie'.mkv`: `/mnt/\[weird\]\,path/\'movie\'.mkv`,
	}
	for in, want := range cases {
		got := ffmpegInputPathEscape(in)
		if got != want {
			t.Errorf("ffmpegInputPathEscape(%q) = %q, want %q", in, got, want)
		}
	}
}
