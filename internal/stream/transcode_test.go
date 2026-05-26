package stream_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hubplay/internal/stream"
)

func newTestTranscoder(t *testing.T) *stream.Transcoder {
	t.Helper()
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return stream.NewTranscoder(stream.TranscoderConfig{
		BaseDir:          dir,
		FFmpegPath:       "ffmpeg",
		TranscodeTimeout: 4 * time.Hour,
		HWAccel:          stream.HWAccelNone,
		Encoder:          "libx264",
		Logger:           logger,
	})
}

// baseRequest devuelve un `TranscodeRequest` con los defaults que la
// mayoría de los tests de `BuildFFmpegArgs` usan: input/output
// canónicos, software libx264, sin seek, sin burn-in, audio
// stream auto-pick (-1). Cada test override los campos que le importan
// antes de pasar el struct a `BuildFFmpegArgs`. Centralizar los
// defaults aquí cierra el olor F14-2-a del audit 2026-05-14
// (positional args = renombrar = tocar 18 sites; struct + helper =
// renombrar = 1 site).
func baseRequest(profile stream.Profile) stream.TranscodeRequest {
	return stream.TranscodeRequest{
		Input:            "/input.mkv",
		OutputDir:        "/out",
		Profile:          profile,
		HWAccel:          stream.HWAccelNone,
		Encoder:          "libx264",
		AudioStreamIndex: -1,
	}
}

func TestNewTranscoder_DefaultFFmpeg(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	tc := stream.NewTranscoder(stream.TranscoderConfig{
		BaseDir:          dir,
		TranscodeTimeout: 4 * time.Hour,
		HWAccel:          stream.HWAccelNone,
		Encoder:          "libx264",
		Logger:           logger,
	})
	if tc == nil {
		t.Fatal("expected non-nil transcoder")
	}
}

func TestTranscoder_GetSession_NotFound(t *testing.T) {
	tc := newTestTranscoder(t)

	_, ok := tc.GetSession("nonexistent")
	if ok {
		t.Error("expected session not found")
	}
}

func TestTranscoder_ActiveSessions_Empty(t *testing.T) {
	tc := newTestTranscoder(t)

	if n := tc.ActiveSessions(); n != 0 {
		t.Errorf("expected 0 active sessions, got %d", n)
	}
}

func TestTranscoder_Stop_NonExistent(t *testing.T) {
	tc := newTestTranscoder(t)

	// Should not panic
	tc.Stop("nonexistent")
}

func TestTranscoder_StopAll_Empty(t *testing.T) {
	tc := newTestTranscoder(t)

	// Should not panic
	tc.StopAll()
}

func TestTranscoder_Start_InvalidFFmpeg(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	tc := stream.NewTranscoder(stream.TranscoderConfig{
		BaseDir:          dir,
		FFmpegPath:       "/nonexistent/ffmpeg",
		TranscodeTimeout: 4 * time.Hour,
		HWAccel:          stream.HWAccelNone,
		Encoder:          "libx264",
		Logger:           logger,
	})

	_, err := tc.Start("sess-1", "item-1", stream.TranscodeRequest{
		Input:            "/some/video.mkv",
		Profile:          stream.DefaultProfile(),
		AudioStreamIndex: -1,
	})
	if err == nil {
		t.Fatal("expected error for invalid ffmpeg path")
	}
	if !strings.Contains(err.Error(), "starting ffmpeg") {
		t.Errorf("expected 'starting ffmpeg' error, got: %v", err)
	}

	// Session should not be registered
	if n := tc.ActiveSessions(); n != 0 {
		t.Errorf("expected 0 active sessions after failed start, got %d", n)
	}
}

func TestSession_ManifestPath(t *testing.T) {
	dir := t.TempDir()
	s := &stream.Session{OutputDir: dir}
	expected := filepath.Join(dir, "stream.m3u8")
	if got := s.ManifestPath(); got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestSession_SegmentPath(t *testing.T) {
	// Build the OutputDir + expected paths through filepath.Join so the
	// assertions match what SegmentPath actually emits — production
	// code uses filepath.Join, which yields backslashes on Windows
	// and forward slashes on POSIX. The previous hard-coded "/tmp/..."
	// strings passed on Linux CI but failed locally on Windows.
	outDir := filepath.Join(string(filepath.Separator)+"tmp", "sessions", "abc")
	s := &stream.Session{OutputDir: outDir}

	tests := []struct {
		index    int
		expected string
	}{
		{0, filepath.Join(outDir, "segment00000.ts")},
		{1, filepath.Join(outDir, "segment00001.ts")},
		{99, filepath.Join(outDir, "segment00099.ts")},
		{12345, filepath.Join(outDir, "segment12345.ts")},
	}

	for _, tt := range tests {
		got := s.SegmentPath(tt.index)
		if got != tt.expected {
			t.Errorf("SegmentPath(%d) = %q, want %q", tt.index, got, tt.expected)
		}
	}
}

func TestBuildFFmpegArgs_Original(t *testing.T) {
	args := stream.BuildFFmpegArgs(baseRequest(stream.Profiles["original"]))

	assertContains(t, args, "-c:v", "copy")
	assertContains(t, args, "-c:a", "copy")
	assertContains(t, args, "-f", "hls")
	assertNotContains(t, args, "-ss")
	assertNotContains(t, args, "libx264")
}

func TestBuildFFmpegArgs_720p(t *testing.T) {
	args := stream.BuildFFmpegArgs(baseRequest(stream.Profiles["720p"]))

	assertContains(t, args, "-c:v", "libx264")
	assertContains(t, args, "-b:v", "2500k")
	assertContains(t, args, "-c:a", "aac")
	assertContains(t, args, "-b:a", "128k")
	assertContains(t, args, "-f", "hls")
}

func TestBuildFFmpegArgs_WithSeek(t *testing.T) {
	req := baseRequest(stream.Profiles["720p"])
	req.StartTime = 30.5
	args := stream.BuildFFmpegArgs(req)

	assertContains(t, args, "-ss", "30.500")
}

// TestBuildFFmpegArgs_AlwaysIncludesCopyts pins the regression for
// the 2026-05-08 seek cascade: without `-copyts`, a restart at
// -ss <T> -start_number N produces segments whose internal PTS
// resets to 0, NOT to N*hls_time as the synthesized VOD manifest
// claims. MSE then picks up the actual PTS (not the manifest's
// claim), the timeline becomes Frankenstein, and hls.js fires
// fan-out segment requests at multiples of the seek target trying
// to fill the resulting "buffer holes". Visible in production as
// "queda sin ir y se pausa" with +297-segment cadence in server
// logs. -copyts must be on every codepath, not just the restarts —
// initial sessions with startTime>0 (resume from saved progress)
// have the same issue.
func TestBuildFFmpegArgs_AlwaysIncludesCopyts(t *testing.T) {
	cases := []struct {
		name      string
		startTime float64
	}{
		{"initial-session-from-zero", 0},
		{"resume-from-saved-position", 42},
		{"seek-restart-mid-movie", 1776},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := baseRequest(stream.Profiles["720p"])
			req.StartTime = tc.startTime
			args := stream.BuildFFmpegArgs(req)
			found := false
			for _, a := range args {
				if a == "-copyts" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("ffmpeg args missing -copyts; got %v", args)
			}
		})
	}
}

func TestBuildFFmpegArgs_NoSeekAtZero(t *testing.T) {
	args := stream.BuildFFmpegArgs(baseRequest(stream.Profiles["720p"]))

	assertNotContains(t, args, "-ss")
}

func TestBuildFFmpegArgs_HLSSettings(t *testing.T) {
	args := stream.BuildFFmpegArgs(baseRequest(stream.Profiles["480p"]))

	assertContains(t, args, "-hls_time", "6")
	assertContains(t, args, "-hls_list_size", "0")
	assertContains(t, args, "-hls_flags", "independent_segments")
	assertContains(t, args, "-start_number", "0")
}

func TestBuildFFmpegArgs_HWAccel_NVENC_PrependsHwaccelAndSwapsEncoder(t *testing.T) {
	req := baseRequest(stream.Profiles["720p"])
	req.HWAccel = stream.HWAccelNVENC
	req.Encoder = "h264_nvenc"
	args := stream.BuildFFmpegArgs(req)

	// Encoder swapped from libx264 to the NVENC variant.
	assertContains(t, args, "-c:v", "h264_nvenc")
	assertNotContains(t, args, "libx264")
	// libx264-only tuning flags must NOT leak into the NVENC path.
	assertNotContains(t, args, "veryfast")
	assertNotContains(t, args, "zerolatency")
	// `-hwaccel cuda` declared on the input side so NVDEC is used
	// for decode when the codec supports it.
	assertContains(t, args, "-hwaccel", "cuda")
}

func TestBuildFFmpegArgs_HWAccel_VAAPI_PrependsHwaccel(t *testing.T) {
	req := baseRequest(stream.Profiles["480p"])
	req.HWAccel = stream.HWAccelVAAPI
	req.Encoder = "h264_vaapi"
	args := stream.BuildFFmpegArgs(req)
	assertContains(t, args, "-c:v", "h264_vaapi")
	assertContains(t, args, "-hwaccel", "vaapi")
}

func TestBuildFFmpegArgs_HWAccel_VideoToolbox_NoInputHwaccelFlag(t *testing.T) {
	// VideoToolbox provides only the encoder, not a decoder pipeline
	// declared via -hwaccel. The args list should swap the encoder
	// without prepending any -hwaccel flag — extra flags would just
	// log a warning and slow ffmpeg's startup.
	req := baseRequest(stream.Profiles["720p"])
	req.HWAccel = stream.HWAccelVideoToolbox
	req.Encoder = "h264_videotoolbox"
	args := stream.BuildFFmpegArgs(req)
	assertContains(t, args, "-c:v", "h264_videotoolbox")
	assertNotContains(t, args, "-hwaccel")
}

// TestBuildFFmpegArgs_ToneMap_PrependsZscaleChain pins the HDR→SDR
// filter graph. When the decision flags ToneMap=true the `-vf` value
// must start with the zscale → tonemap(hable) → zscale chain BEFORE
// the regular scale+pad. ffmpeg evaluates filters left to right, so
// any reordering would feed PQ-coded floats to scale/pad and
// produce washed-out output.
func TestBuildFFmpegArgs_ToneMap_PrependsZscaleChain(t *testing.T) {
	req := baseRequest(stream.Profiles["720p"])
	req.ToneMap = true
	args := stream.BuildFFmpegArgs(req)

	vf := flagValue(args, "-vf")
	if vf == "" {
		t.Fatal("missing -vf in args")
	}
	// Tonemap chain must come first.
	if !strings.HasPrefix(vf, "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=hable,zscale=t=bt709:m=bt709:r=tv,format=yuv420p,") {
		t.Errorf("-vf must start with the tonemap chain; got %q", vf)
	}
	// Scale+pad must still be there afterwards (same expression as the SDR path).
	if !strings.Contains(vf, "scale=1280:720:force_original_aspect_ratio=decrease,pad=1280:720:(ow-iw)/2:(oh-ih)/2") {
		t.Errorf("-vf missing the scale+pad after tonemap chain; got %q", vf)
	}
}

// TestBuildFFmpegArgs_NoToneMap_NoZscale pins the negative side: an
// SDR transcode (toneMap=false) must NOT add zscale/tonemap filters.
// Spurious zscale filters need libzimg in the ffmpeg build and would
// break setups that don't have it.
func TestBuildFFmpegArgs_NoToneMap_NoZscale(t *testing.T) {
	args := stream.BuildFFmpegArgs(baseRequest(stream.Profiles["720p"]))
	vf := flagValue(args, "-vf")
	if strings.Contains(vf, "zscale") || strings.Contains(vf, "tonemap") {
		t.Errorf("SDR -vf must not contain zscale/tonemap; got %q", vf)
	}
}

// TestBuildFFmpegArgs_ToneMap_IgnoredOnCopyVideo guards against a
// future caller passing ToneMap=true alongside CopyVideo=true. There
// is no decoded frame to filter on the stream-copy path; ffmpeg would
// reject `-vf` with `-c:v copy`. Today the decision code never
// produces this combination, but the encoder side stays defensive.
func TestBuildFFmpegArgs_ToneMap_IgnoredOnCopyVideo(t *testing.T) {
	req := baseRequest(stream.Profiles["720p"])
	req.CopyVideo = true
	req.ToneMap = true
	args := stream.BuildFFmpegArgs(req)
	for _, a := range args {
		if a == "-vf" {
			t.Errorf("copyVideo=true must not emit -vf; got %v", args)
		}
	}
}

// TestBuildFFmpegArgs_LibX264Preset_Threaded pins that the
// libx264Preset argument actually flows to ffmpeg's -preset on the
// software path. Before this argument existed the value was hardcoded
// to "veryfast" in BuildFFmpegArgs, which silently defeated the
// `streaming.transcode_preset` config knob — a dead config bug for
// the lifetime of the project until 2026-05-12.
func TestBuildFFmpegArgs_LibX264Preset_Threaded(t *testing.T) {
	cases := []struct {
		name    string
		preset  string
		wantArg string
	}{
		{"explicit medium", "medium", "medium"},
		{"explicit ultrafast", "ultrafast", "ultrafast"},
		{"empty falls back to veryfast", "", "veryfast"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := baseRequest(stream.Profiles["720p"])
			req.Libx264Preset = tc.preset
			args := stream.BuildFFmpegArgs(req)
			got := flagValue(args, "-preset")
			if got != tc.wantArg {
				t.Errorf("-preset = %q, want %q (full args: %v)", got, tc.wantArg, args)
			}
		})
	}
}

// TestBuildFFmpegArgs_LibX264Preset_IgnoredOnHWAccel pins that the
// preset value is NOT emitted when the encoder is a hardware path —
// libx264 -preset names mean nothing to NVENC / VAAPI / QSV and
// leaking them into the args list logs spurious warnings.
func TestBuildFFmpegArgs_LibX264Preset_IgnoredOnHWAccel(t *testing.T) {
	req := baseRequest(stream.Profiles["720p"])
	req.HWAccel = stream.HWAccelNVENC
	req.Encoder = "h264_nvenc"
	req.Libx264Preset = "medium"
	args := stream.BuildFFmpegArgs(req)
	for i, a := range args {
		if a == "-preset" {
			t.Errorf("HW encoder must not emit -preset; got %q at %d in %v", args[i+1], i, args)
		}
	}
}

// TestBuildFFmpegArgs_BurnSub_Bitmap_UsesFilterComplex pins that
// PGS / DVDSUB burn-in emits a filter_complex chain with the overlay
// node + explicit -map [burned] for video. The order matters: the
// scale chain runs FIRST so the subtitle overlay receives
// already-downscaled frames (matches output size).
func TestBuildFFmpegArgs_BurnSub_Bitmap_UsesFilterComplex(t *testing.T) {
	req := baseRequest(stream.Profiles["720p"])
	req.Input = "/in.mkv"
	req.BurnSub = &stream.BurnSubtitleSpec{Index: 2, Codec: "hdmv_pgs_subtitle", InputPath: "/in.mkv"}
	args := stream.BuildFFmpegArgs(req)

	fc := flagValue(args, "-filter_complex")
	if fc == "" {
		t.Fatalf("missing -filter_complex; args=%v", args)
	}
	if !strings.Contains(fc, "[0:s:2]overlay[burned]") {
		t.Errorf("filter_complex missing overlay node referencing sub stream 2: %q", fc)
	}
	if !strings.Contains(fc, "scale=1280:720") {
		t.Errorf("filter_complex must include the regular scale chain before overlay: %q", fc)
	}
	// Video map comes from the filter_complex output, not 0:v:0.
	assertContains(t, args, "-map", "[burned]")
	// Audio map must be present (filter_complex disables ffmpeg's
	// default stream picker). Default audio uses the optional ?
	// suffix so video-only files don't fail the start.
	assertContains(t, args, "-map", "0:a:0?")
	// -vf must NOT also appear — ffmpeg rejects both flags together.
	for _, a := range args {
		if a == "-vf" {
			t.Errorf("burn-in path must not emit -vf alongside -filter_complex; args=%v", args)
		}
	}
}

// TestBuildFFmpegArgs_BurnSub_ASS_UsesSubtitlesFilter pins that
// ASS / SSA burn-in uses the `subtitles` filter prepended to -vf.
// The path inside the filter expression must be single-quoted and
// have colons / backslashes escaped or ffmpeg will silently parse
// half of it as filter options.
func TestBuildFFmpegArgs_BurnSub_ASS_UsesSubtitlesFilter(t *testing.T) {
	req := baseRequest(stream.Profiles["720p"])
	req.Input = "/mnt/x:y/anime.mkv"
	req.BurnSub = &stream.BurnSubtitleSpec{Index: 0, Codec: "ass", InputPath: "/mnt/x:y/anime.mkv"}
	args := stream.BuildFFmpegArgs(req)

	vf := flagValue(args, "-vf")
	if vf == "" {
		t.Fatalf("missing -vf; args=%v", args)
	}
	// subtitles filter must come first (full-resolution frames →
	// crisper text after downscale).
	if !strings.HasPrefix(vf, "subtitles=filename='") {
		t.Errorf("-vf should start with subtitles= filter, got %q", vf)
	}
	// Colon in path must be escaped — otherwise ffmpeg parses
	// :y/anime.mkv as a separate filter option and breaks.
	if !strings.Contains(vf, `'/mnt/x\:y/anime.mkv'`) {
		t.Errorf("colon in path not escaped inside subtitles= filter: %q", vf)
	}
	if !strings.Contains(vf, ":si=0,") {
		t.Errorf("subtitles filter missing si=0 index marker: %q", vf)
	}
	// filter_complex must NOT be used for ASS.
	for _, a := range args {
		if a == "-filter_complex" {
			t.Errorf("ASS burn-in should use -vf, not -filter_complex; args=%v", args)
		}
	}
}

// TestBuildFFmpegArgs_BurnSub_ForcesReencode pins that passing a
// BurnSubtitleSpec overrides copyVideo=true. Stream-copy paths have
// no decoded frames to composite onto; ffmpeg would error mid-run
// otherwise. The handler should already convert the decision before
// calling us, but the encoder side stays defensive.
func TestBuildFFmpegArgs_BurnSub_ForcesReencode(t *testing.T) {
	req := baseRequest(stream.Profiles["720p"])
	req.Input = "/in.mkv"
	req.CopyVideo = true // caller asked copyVideo — should be overridden by BurnSub
	req.BurnSub = &stream.BurnSubtitleSpec{Index: 0, Codec: "pgs", InputPath: "/in.mkv"}
	args := stream.BuildFFmpegArgs(req)
	// -c:v copy must NOT appear; the real encoder must be selected.
	for i, a := range args {
		if a == "-c:v" && i+1 < len(args) && args[i+1] == "copy" {
			t.Errorf("burn-in must override copyVideo; got -c:v copy in %v", args)
		}
	}
	assertContains(t, args, "-c:v", "libx264")
}

// TestBuildFFmpegArgs_InputUsesFileProtocol pins the `file:` prefix
// in front of -i. Without it, ffmpeg parses any input that begins
// with a dash as a flag — a real risk for filenames like
// "-loglevel.mp4" or paths that some weirdly creative scanner might
// produce. With the prefix the path is always treated verbatim.
func TestBuildFFmpegArgs_InputUsesFileProtocol(t *testing.T) {
	req := baseRequest(stream.Profiles["720p"])
	req.Input = "/path/to/input.mkv"
	args := stream.BuildFFmpegArgs(req)
	assertContains(t, args, "-i", "file:/path/to/input.mkv")
	assertNotContains(t, args, "/path/to/input.mkv") // raw path must NOT appear after -i
}

// assertContains checks that key and value appear consecutively in args.
func assertContains(t *testing.T, args []string, key, value string) {
	t.Helper()
	for i, a := range args {
		if a == key && i+1 < len(args) && args[i+1] == value {
			return
		}
	}
	t.Errorf("expected args to contain %q %q, got %v", key, value, args)
}

// assertNotContains checks that key does not appear in args.
func assertNotContains(t *testing.T, args []string, key string) {
	t.Helper()
	for _, a := range args {
		if a == key {
			t.Errorf("expected args NOT to contain %q, but it was found", key)
			return
		}
	}
}

// flagValue returns the argument that follows `flag` in args, or "" if
// the flag isn't present (or has no value after it). Lets tone-map
// tests inspect the constructed -vf string without re-walking args.
func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
