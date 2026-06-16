package torrentstream

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// filePlayable is a Playable backed by a real file on disk, so the loopback
// server feeds ffmpeg/ffprobe an actual seekable media file (not a torrent).
type filePlayable struct {
	ih, path, name string
	size           int64
}

func (f *filePlayable) InfoHash() string { return f.ih }
func (f *filePlayable) FileName() string { return f.name }
func (f *filePlayable) Length() int64    { return f.size }
func (f *filePlayable) Touch()           {}
func (f *filePlayable) Reader() io.ReadSeekCloser {
	fh, err := os.Open(f.path)
	if err != nil {
		// Returning a nil reader would panic later; an empty reader makes the
		// loopback 404/short-read instead, which the test surfaces clearly.
		return nopCloser{strings.NewReader("")}
	}
	return fh
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("integration test needs %s in PATH", bin)
		}
	}
}

// genMedia synthesises a tiny 2s clip with the requested codecs via ffmpeg's
// lavfi test sources. Returns the file path.
func genMedia(t *testing.T, dir, name, vcodec, acodec string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=duration=2:size=160x120:rate=15",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", vcodec, "-c:a", acodec, "-shortest", path,
	}
	if vcodec == "libx264" {
		args = append(args[:len(args)-1], "-preset", "ultrafast", path)
	}
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg gen %s: %v\n%s", name, err, out)
	}
	return path
}

// probeVideoCodec returns the codec_name of the first video stream of a file.
func probeVideoCodec(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error",
		"-select_streams", "v:0", "-show_entries", "stream=codec_name",
		"-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	return strings.TrimSpace(string(out))
}

// TestVODIntegration_RealFFmpeg drives the WHOLE VOD path with real binaries:
// a real media file → real loopback reader server → real ffprobe decision →
// real ffmpeg transcode → a playable HLS playlist + segments on disk. This is
// the end-to-end check the unit tests (which stub ffmpeg) can't give.
func TestVODIntegration_RealFFmpeg(t *testing.T) {
	requireFFmpeg(t)

	cases := []struct {
		name     string
		vcodec   string // ffmpeg encoder for the source clip
		acodec   string
		wantMode PlayMode
		ih       string
	}{
		// H.264 video in MKV → cheap `-c copy` remux to HLS.
		{"remux_h264_in_mkv", "libx264", "ac3", PlayRemux, "aaaa01"},
		// MPEG-4 (XviD-era) video → must be re-encoded to H.264.
		{"reencode_mpeg4", "mpeg4", "ac3", PlayReencode, "bbbb02"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := genMedia(t, dir, "src.mkv", tc.vcodec, tc.acodec)
			st, err := os.Stat(src)
			if err != nil {
				t.Fatal(err)
			}

			m, err := NewVODTransmux(VODConfig{
				WorkRoot:      t.TempDir(),
				IdleTimeout:   time.Minute,
				AllowReencode: true,
			}, nil) // nil prober → real probe.New(); default startFFmpeg
			if err != nil {
				t.Fatalf("NewVODTransmux: %v", err)
			}
			t.Cleanup(m.Shutdown)

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			res, err := m.Prepare(ctx, &filePlayable{ih: tc.ih, path: src, name: "src.mkv", size: st.Size()})
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if res.Mode != tc.wantMode {
				t.Fatalf("mode: got %v want %v", res.Mode, tc.wantMode)
			}
			if !res.HLS {
				t.Fatalf("expected an HLS session, got HLS=false")
			}

			// Playlist must exist, be a valid m3u8, and list a segment.
			pl, ok := m.PlaylistPath(tc.ih)
			if !ok {
				t.Fatal("PlaylistPath not found")
			}
			body, err := os.ReadFile(pl)
			if err != nil {
				t.Fatalf("read playlist: %v", err)
			}
			if !strings.Contains(string(body), "#EXTM3U") {
				t.Errorf("playlist missing #EXTM3U header:\n%s", body)
			}
			if !strings.Contains(string(body), "seg-") || !strings.Contains(string(body), ".ts") {
				t.Errorf("playlist lists no segments:\n%s", body)
			}

			// First segment must exist, be non-empty, and decode as H.264 —
			// proving the transcode actually produced browser-playable video.
			segPath, ok := m.SegmentPath(tc.ih, "seg-00000.ts")
			if !ok {
				t.Fatal("seg-00000.ts did not resolve")
			}
			seg, err := os.Stat(segPath)
			if err != nil || seg.Size() == 0 {
				t.Fatalf("first segment missing/empty: %v size=%d", err, segFileSize(seg))
			}
			// MPEG-TS repeats stream info, so ffprobe may print the codec more
			// than once; assert H.264 is present and the source codec is gone.
			if got := probeVideoCodec(t, segPath); !strings.Contains(got, "h264") || strings.Contains(got, "mpeg4") {
				t.Errorf("output segment video codec: got %q want h264", got)
			}
		})
	}
}

func segFileSize(fi os.FileInfo) int64 {
	if fi == nil {
		return 0
	}
	return fi.Size()
}
