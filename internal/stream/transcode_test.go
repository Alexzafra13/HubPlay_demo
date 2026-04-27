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
	return stream.NewTranscoder(dir, "ffmpeg", 4*time.Hour, stream.HWAccelNone, "libx264", logger)
}

func TestNewTranscoder_DefaultFFmpeg(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	tc := stream.NewTranscoder(dir, "", 4*time.Hour, stream.HWAccelNone, "libx264", logger)
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
	tc := stream.NewTranscoder(dir, "/nonexistent/ffmpeg", 4*time.Hour, stream.HWAccelNone, "libx264", logger)

	_, err := tc.Start("sess-1", "item-1", "/some/video.mkv", stream.DefaultProfile(), 0)
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
	s := &stream.Session{OutputDir: "/tmp/sessions/abc"}
	expected := filepath.Join("/tmp/sessions/abc", "stream.m3u8")
	if got := s.ManifestPath(); got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestSession_SegmentPath(t *testing.T) {
	s := &stream.Session{OutputDir: "/tmp/sessions/abc"}

	tests := []struct {
		index    int
		expected string
	}{
		{0, "/tmp/sessions/abc/segment00000.ts"},
		{1, "/tmp/sessions/abc/segment00001.ts"},
		{99, "/tmp/sessions/abc/segment00099.ts"},
		{12345, "/tmp/sessions/abc/segment12345.ts"},
	}

	for _, tt := range tests {
		got := s.SegmentPath(tt.index)
		if got != tt.expected {
			t.Errorf("SegmentPath(%d) = %q, want %q", tt.index, got, tt.expected)
		}
	}
}

func TestBuildFFmpegArgs_Original(t *testing.T) {
	args := stream.BuildFFmpegArgs("/input.mkv", "/out", stream.Profiles["original"], 0, stream.HWAccelNone, "libx264")

	assertContains(t, args, "-c:v", "copy")
	assertContains(t, args, "-c:a", "copy")
	assertContains(t, args, "-f", "hls")
	assertNotContains(t, args, "-ss")
	assertNotContains(t, args, "libx264")
}

func TestBuildFFmpegArgs_720p(t *testing.T) {
	args := stream.BuildFFmpegArgs("/input.mkv", "/out", stream.Profiles["720p"], 0, stream.HWAccelNone, "libx264")

	assertContains(t, args, "-c:v", "libx264")
	assertContains(t, args, "-b:v", "2500k")
	assertContains(t, args, "-c:a", "aac")
	assertContains(t, args, "-b:a", "128k")
	assertContains(t, args, "-f", "hls")
}

func TestBuildFFmpegArgs_WithSeek(t *testing.T) {
	args := stream.BuildFFmpegArgs("/input.mkv", "/out", stream.Profiles["720p"], 30.5, stream.HWAccelNone, "libx264")

	assertContains(t, args, "-ss", "30.500")
}

func TestBuildFFmpegArgs_NoSeekAtZero(t *testing.T) {
	args := stream.BuildFFmpegArgs("/input.mkv", "/out", stream.Profiles["720p"], 0, stream.HWAccelNone, "libx264")

	assertNotContains(t, args, "-ss")
}

func TestBuildFFmpegArgs_HLSSettings(t *testing.T) {
	args := stream.BuildFFmpegArgs("/input.mkv", "/out", stream.Profiles["480p"], 0, stream.HWAccelNone, "libx264")

	assertContains(t, args, "-hls_time", "6")
	assertContains(t, args, "-hls_list_size", "0")
	assertContains(t, args, "-hls_flags", "independent_segments")
	assertContains(t, args, "-start_number", "0")
}

func TestBuildFFmpegArgs_HWAccel_NVENC_PrependsHwaccelAndSwapsEncoder(t *testing.T) {
	args := stream.BuildFFmpegArgs("/input.mkv", "/out", stream.Profiles["720p"], 0,
		stream.HWAccelNVENC, "h264_nvenc")

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
	args := stream.BuildFFmpegArgs("/input.mkv", "/out", stream.Profiles["480p"], 0,
		stream.HWAccelVAAPI, "h264_vaapi")
	assertContains(t, args, "-c:v", "h264_vaapi")
	assertContains(t, args, "-hwaccel", "vaapi")
}

func TestBuildFFmpegArgs_HWAccel_VideoToolbox_NoInputHwaccelFlag(t *testing.T) {
	// VideoToolbox provides only the encoder, not a decoder pipeline
	// declared via -hwaccel. The args list should swap the encoder
	// without prepending any -hwaccel flag — extra flags would just
	// log a warning and slow ffmpeg's startup.
	args := stream.BuildFFmpegArgs("/input.mkv", "/out", stream.Profiles["720p"], 0,
		stream.HWAccelVideoToolbox, "h264_videotoolbox")
	assertContains(t, args, "-c:v", "h264_videotoolbox")
	assertNotContains(t, args, "-hwaccel")
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
