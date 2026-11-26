package stream

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	librarymodel "hubplay/internal/library/model"
)

func argAfter(args []string, flag string) (string, bool) {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1], true
		}
	}
	return "", false
}

func testPlan() *SegmentPlan {
	return BuildSegmentPlan(&KeyframeIndex{Points: []KeyPoint{
		{PTS: 0, Lead: 0}, {PTS: 8, Lead: 8}, {PTS: 16.1, Lead: 16.0}, {PTS: 24, Lead: 24},
	}}, 30, 6)
}

func TestBuildRemuxSegmentArgs_MiddleSegmentCopyAudio(t *testing.T) {
	spec := remuxSpec{Input: "/m/movie.mkv", VideoCodec: "h264", AudioOrdinal: 1, CopyAudio: true}
	args := BuildRemuxSegmentArgs(spec, testPlan(), 2, "/out/seg.tmp")
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"-copyts",
		"-i file:/m/movie.mkv",
		"-map 0:V:0", // V mayúscula: sin carátulas attached_pic
		"-map 0:a:1",
		"-c:v copy",
		"-c:a copy",
		"-avoid_negative_ts disabled",
		"-f mpegts /out/seg.tmp",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %s", want, joined)
		}
	}
	if v, _ := argAfter(args, "-ss"); v != "16.100000" {
		t.Errorf("-ss = %q, want the keyframe PTS 16.100000", v)
	}
	if v, _ := argAfter(args, "-to"); v != "26.000000" {
		t.Errorf("-to = %q, want end + read margin", v)
	}
	// Ventana [Lead_i, Lead_{i+1}) con el épsilon de redondeo.
	wantDrop := "noise=drop=lt(pts*tb\\,15.999000)+gte(pts*tb\\,23.999000)"
	if v, _ := argAfter(args, "-bsf:v"); v != "h264_mp4toannexb,dump_extra=freq=keyframe,"+wantDrop {
		t.Errorf("-bsf:v = %q", v)
	}
	if v, _ := argAfter(args, "-bsf:a"); v != wantDrop {
		t.Errorf("-bsf:a = %q", v)
	}
}

func TestBuildRemuxSegmentArgs_FirstAndLastSegments(t *testing.T) {
	spec := remuxSpec{Input: "/m/a.mp4", VideoCodec: "hevc", AudioOrdinal: -1}
	plan := testPlan()

	first := BuildRemuxSegmentArgs(spec, plan, 0, "/o")
	if _, ok := argAfter(first, "-ss"); ok {
		t.Error("segment 0 must not seek")
	}
	if v, _ := argAfter(first, "-bsf:v"); !strings.HasPrefix(v, "hevc_mp4toannexb,dump_extra=freq=keyframe,") {
		t.Errorf("hevc chain = %q", v)
	}
	if strings.Contains(strings.Join(first, " "), "0:a:") || strings.Contains(strings.Join(first, " "), "-bsf:a") {
		t.Error("no audio track: no audio map or filter")
	}

	last := BuildRemuxSegmentArgs(spec, plan, plan.Count()-1, "/o")
	if _, ok := argAfter(last, "-to"); ok {
		t.Error("last segment reads to EOF")
	}
	if v, _ := argAfter(last, "-bsf:v"); strings.Contains(v, "gte(") {
		t.Errorf("last segment has an upper bound: %q", v)
	}
}

func TestBuildRemuxSegmentArgs_UnknownCodecStillRepeatsHeaders(t *testing.T) {
	args := BuildRemuxSegmentArgs(remuxSpec{Input: "x", VideoCodec: "mpeg2video", AudioOrdinal: -1}, testPlan(), 1, "/o")
	if v, _ := argAfter(args, "-bsf:v"); !strings.HasPrefix(v, "dump_extra=freq=keyframe,noise=drop=") {
		t.Errorf("-bsf:v = %q", v)
	}
}

func TestBuildRemuxSegmentArgs_TranscodedAudioOnSharedGrid(t *testing.T) {
	spec := remuxSpec{Input: "x", VideoCodec: "h264", AudioOrdinal: 0, CopyAudio: false, AudioBitrate: "160k", AudioChannels: 6}
	args := BuildRemuxSegmentArgs(spec, testPlan(), 2, "/o") // Start 16.0
	af, ok := argAfter(args, "-af")
	if !ok {
		t.Fatal("no -af")
	}
	// T0 = múltiplo de la trama AAC (1024 muestras a 48 kHz) ≤ Start-2.
	t0s := strings.SplitN(strings.SplitN(af, "atrim=start=", 2)[1], ",", 2)[0]
	t0, err := strconv.ParseFloat(t0s, 64)
	if err != nil {
		t.Fatalf("atrim start %q: %v", t0s, err)
	}
	frames := t0 / aacFrameSeconds
	if math.Abs(frames-math.Round(frames)) > 1e-3 || t0 > 14.0 || t0 < 14.0-aacFrameSeconds {
		t.Fatalf("T0 = %v is not the grid frame just before 14.0", t0)
	}
	if !strings.Contains(af, "aresample=48000") || !strings.Contains(af, "asetpts=N/SR/TB+"+t0s+"/TB") {
		t.Errorf("-af = %q", af)
	}
	ss, _ := argAfter(args, "-ss")
	if v, _ := strconv.ParseFloat(ss, 64); v > t0-1+1e-6 {
		t.Errorf("-ss %v must start reading before the encoder warm-up (%v)", v, t0-1)
	}
	for flag, want := range map[string]string{"-c:a": "aac", "-b:a": "160k", "-ac": "6"} {
		if v, _ := argAfter(args, flag); v != want {
			t.Errorf("%s = %q, want %q", flag, v, want)
		}
	}
}

func TestSegmentRemuxer_ServesExistingAndRejectsOutOfRange(t *testing.T) {
	dir := t.TempDir()
	plan := testPlan()
	r := newSegmentRemuxer("__hubplay_nonexistent_ffmpeg__", remuxSpec{Input: "x", AudioOrdinal: -1}, plan, dir, testLogger())
	defer r.Stop()

	if err := os.WriteFile(filepath.Join(dir, "segment00001.ts"), []byte("TS"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := r.Ensure(context.Background(), 1)
	if err != nil || filepath.Base(p) != "segment00001.ts" {
		t.Fatalf("Ensure(existing) = %q, %v", p, err)
	}
	if _, err := r.Ensure(context.Background(), plan.Count()); !IsSegmentOutOfRange(err) {
		t.Fatalf("out of range err = %v", err)
	}
	if _, err := r.Ensure(context.Background(), -1); !IsSegmentOutOfRange(err) {
		t.Fatalf("negative index err = %v", err)
	}
	// Generar sin ffmpeg falla limpio y no deja .tmp.
	if _, err := r.Ensure(context.Background(), 0); err == nil {
		t.Fatal("generation without ffmpeg succeeded")
	}
	if _, err := os.Stat(filepath.Join(dir, "segment00000.ts.tmp")); !os.IsNotExist(err) {
		t.Fatalf("leftover tmp: %v", err)
	}
}

func TestSegmentRemuxer_StopIsIdempotentAndRefusesWork(t *testing.T) {
	r := newSegmentRemuxer("__hubplay_nonexistent_ffmpeg__", remuxSpec{Input: "x", AudioOrdinal: -1}, testPlan(), t.TempDir(), testLogger())
	r.Stop()
	r.Stop()
	if _, err := r.Ensure(context.Background(), 0); err == nil {
		t.Fatal("stopped remuxer produced a segment")
	}
}

func TestDefaultAudioOrdinal(t *testing.T) {
	ms := func(typ string, def bool) *librarymodel.MediaStream {
		return &librarymodel.MediaStream{StreamType: typ, IsDefault: def}
	}
	streams := []*librarymodel.MediaStream{
		ms("video", false), ms("audio", false), ms("subtitle", true), ms("audio", true), ms("audio", false),
	}
	// La misma pista que evalúa Decide (pickStreams): la marcada por
	// defecto → ordinal 1 entre las de audio.
	if got := defaultAudioOrdinal(streams, -1); got != 1 {
		t.Errorf("default-flagged audio ordinal = %d, want 1", got)
	}
	if got := defaultAudioOrdinal(streams, 2); got != 2 {
		t.Errorf("explicit = %d, want 2", got)
	}
	if got := defaultAudioOrdinal([]*librarymodel.MediaStream{ms("audio", false), ms("audio", false)}, -1); got != 0 {
		t.Errorf("no default flag → first = %d", got)
	}
	if got := defaultAudioOrdinal([]*librarymodel.MediaStream{ms("video", true)}, -1); got != -1 {
		t.Errorf("no audio = %d, want -1", got)
	}
}

func TestRestartSessionAt_NoOpForSegmentedRemux(t *testing.T) {
	m := newTestManager(t)
	sess := NewSegmentedSessionForTest("k", "item", t.TempDir(), testPlan())
	defer sess.Stop()
	m.sessions["k"] = &ManagedSession{Session: sess, Decision: PlaybackDecision{CopyVideo: true}}
	// Sin proceso que reiniciar: ni error ni intento de lanzar ffmpeg.
	if err := m.RestartSessionAt("k", 3, 6); err != nil {
		t.Fatalf("RestartSessionAt = %v", err)
	}
	if m.sessions["k"].Session != sess {
		t.Fatal("segmented session was replaced")
	}
}

func TestTrySegmentedRemux_DisabledWithoutKeyframeStore(t *testing.T) {
	m := newTestManager(t) // sin keyframes (como todos los tests del manager)
	item := &librarymodel.Item{ID: "i", Path: "/nonexistent.mkv", DurationTicks: 60 * 10_000_000}
	if ms := m.trySegmentedRemux(context.Background(), "k", "u", item, nil, -1, PlaybackDecision{CopyVideo: true}); ms != nil {
		t.Fatal("segmented remux without a keyframe store")
	}
}
