package torrentstream

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"hubplay/internal/probe"
)

type fakePlayable struct {
	ih      string
	touched atomic.Int32
}

func (f *fakePlayable) InfoHash() string { return f.ih }
func (f *fakePlayable) FileName() string { return "movie.mkv" }
func (f *fakePlayable) Length() int64    { return 1024 }
func (f *fakePlayable) Touch()           { f.touched.Add(1) }
func (f *fakePlayable) Reader() io.ReadSeekCloser {
	return nopCloser{bytes.NewReader([]byte("fake media bytes"))}
}

type nopCloser struct{ io.ReadSeeker }

func (nopCloser) Close() error { return nil }

type fakeProber struct {
	res *probe.Result
	err error
}

func (f fakeProber) Probe(context.Context, string) (*probe.Result, error) { return f.res, f.err }

func newTestVOD(t *testing.T, p probe.Prober) *VODTransmux {
	t.Helper()
	m, err := NewVODTransmux(VODConfig{
		WorkRoot:    t.TempDir(),
		Prober:      p,
		IdleTimeout: time.Minute,
	}, nil)
	if err != nil {
		t.Fatalf("NewVODTransmux: %v", err)
	}
	t.Cleanup(m.Shutdown)
	return m
}

// fakeStarter writes a playlist + one segment so waitFirstSegment succeeds,
// and records whether stop was called.
func fakeStarter(stopped *atomic.Bool) transcodeStarter {
	return func(_ context.Context, _, workDir string, _ bool) (func(), error) {
		_ = os.WriteFile(filepath.Join(workDir, "index.m3u8"), []byte("#EXTM3U\n"), 0o644)
		_ = os.WriteFile(filepath.Join(workDir, "seg-00000.ts"), []byte("ts"), 0o644)
		return func() { stopped.Store(true) }, nil
	}
}

func probeResult(container, vcodec, acodec string) *probe.Result {
	return &probe.Result{
		Format:  probe.Format{FormatName: container},
		Streams: []probe.Stream{{CodecType: "video", CodecName: vcodec}, {CodecType: "audio", CodecName: acodec}},
	}
}

func TestVODPrepare_Direct(t *testing.T) {
	m := newTestVOD(t, fakeProber{res: probeResult("mov,mp4,m4a", "h264", "aac")})
	res, err := m.Prepare(context.Background(), &fakePlayable{ih: "abc"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if res.Mode != PlayDirect {
		t.Fatalf("mode: got %v want direct", res.Mode)
	}
	if _, ok := m.PlaylistPath("abc"); ok {
		t.Error("direct play must not create a remux session")
	}
}

func TestVODPrepare_Remux(t *testing.T) {
	var stopped atomic.Bool
	m := newTestVOD(t, fakeProber{res: probeResult("matroska,webm", "h264", "ac3")})
	m.start = fakeStarter(&stopped)

	res, err := m.Prepare(context.Background(), &fakePlayable{ih: "dead"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if res.Mode != PlayRemux {
		t.Fatalf("mode: got %v want remux", res.Mode)
	}
	pl, ok := m.PlaylistPath("dead")
	if !ok || !fileExists(pl) {
		t.Fatalf("playlist should exist: %q ok=%v", pl, ok)
	}
	if _, ok := m.SegmentPath("dead", "seg-00000.ts"); !ok {
		t.Error("valid segment should resolve")
	}
	if _, ok := m.SegmentPath("dead", "../etc/passwd"); ok {
		t.Error("path traversal segment must be rejected")
	}

	// Re-Prepare reuses the running session (no new probe needed).
	if r2, err := m.Prepare(context.Background(), &fakePlayable{ih: "dead"}); err != nil || r2.Mode != PlayRemux {
		t.Errorf("re-prepare should reuse session: %v %v", r2.Mode, err)
	}

	m.Stop("dead")
	if !stopped.Load() {
		t.Error("Stop should kill the transcoder")
	}
	if _, ok := m.PlaylistPath("dead"); ok {
		t.Error("session should be gone after Stop")
	}
}

func TestVODPrepare_Reencode(t *testing.T) {
	m := newTestVOD(t, fakeProber{res: probeResult("matroska,webm", "hevc", "aac")})
	res, err := m.Prepare(context.Background(), &fakePlayable{ih: "ff"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if res.Mode != PlayReencode {
		t.Fatalf("mode: got %v want reencode", res.Mode)
	}
	if _, ok := m.PlaylistPath("ff"); ok {
		t.Error("reencode (not yet wired) must not create a session")
	}
}

func TestVODReapIdle(t *testing.T) {
	var stopped atomic.Bool
	m := newTestVOD(t, fakeProber{res: probeResult("matroska,webm", "h264", "aac")})
	m.start = fakeStarter(&stopped)
	if _, err := m.Prepare(context.Background(), &fakePlayable{ih: "aa"}); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	// Force the session well past the idle window.
	m.reapIdle(time.Now().Add(2 * time.Hour))
	if _, ok := m.PlaylistPath("aa"); ok {
		t.Error("idle session should be reaped")
	}
	if !stopped.Load() {
		t.Error("reaped session's transcoder should be stopped")
	}
}

func TestMediaInfoFromProbe_SkipsAttachedPic(t *testing.T) {
	res := &probe.Result{
		Format: probe.Format{FormatName: "mov,mp4"},
		Streams: []probe.Stream{
			{CodecType: "video", CodecName: "mjpeg", IsAttachedPic: true},
			{CodecType: "video", CodecName: "h264"},
			{CodecType: "audio", CodecName: "aac"},
		},
	}
	mi := mediaInfoFromProbe(res)
	if mi.VideoCodec != "h264" {
		t.Errorf("cover-art mjpeg should be skipped, got %q", mi.VideoCodec)
	}
	if mi.AudioCodec != "aac" {
		t.Errorf("audio: got %q", mi.AudioCodec)
	}
}
