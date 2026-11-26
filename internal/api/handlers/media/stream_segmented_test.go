package media

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hubplay/internal/auth"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/stream"
)

// Remux por segmentos: el manifest lleva los cortes reales y los
// segmentos se generan bajo demanda (sin reinicios de ffmpeg).

func segmentedTestPlan() *stream.SegmentPlan {
	// Keyframes cada 4 s → segmentos de 8 s (no la rejilla de 6 s).
	pts := make([]stream.KeyPoint, 0, 8)
	for i := 0; i < 8; i++ {
		t := float64(i) * 4
		pts = append(pts, stream.KeyPoint{PTS: t, Lead: t})
	}
	return stream.BuildSegmentPlan(&stream.KeyframeIndex{Points: pts}, 32, 6)
}

func registerSegmentedSession(env *streamTestEnv, dir string) {
	sess := stream.NewSegmentedSessionForTest("k", "item-1", dir, segmentedTestPlan())
	env.t.Cleanup(sess.Stop)
	env.manager.sessions[stream.SessionKey("user-1", "item-1", "720p", -1, -1)] = &stream.ManagedSession{
		Session:  sess,
		Decision: stream.PlaybackDecision{Method: stream.MethodDirectStream, CopyVideo: true},
	}
}

func TestStreamHandler_QualityPlaylist_SegmentedRemuxUsesRealCuts(t *testing.T) {
	env := newStreamTestEnv(t)
	env.items.byID["item-1"] = &librarymodel.Item{
		ID: "item-1", Path: "/tmp/x.mkv", IsAvailable: true, DurationTicks: 32 * 10_000_000,
	}
	sess := stream.NewSegmentedSessionForTest("k", "item-1", t.TempDir(), segmentedTestPlan())
	t.Cleanup(sess.Stop)
	env.manager.startSessionFn = func(_ context.Context, _ stream.StartSessionRequest) (*stream.ManagedSession, error) {
		return &stream.ManagedSession{
			Session:  sess,
			Decision: stream.PlaybackDecision{Method: stream.MethodDirectStream, CopyVideo: true},
		}, nil
	}
	rr := env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/720p/stream.m3u8?audio=1",
		&auth.Claims{UserID: "user-1"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if got := strings.Count(body, "#EXTINF:8.000000,"); got != 4 {
		t.Errorf("want 4 segments of 8 s (real cuts), got %d:\n%s", got, body)
	}
	if strings.Contains(body, "#EXTINF:6.000") {
		t.Errorf("manifest still uses the synthetic 6 s grid:\n%s", body)
	}
	if !strings.Contains(body, "/api/v1/stream/item-1/720p/segment00003.ts?audio=1") {
		t.Errorf("segment URLs must carry the audio param:\n%s", body)
	}
}

func TestStreamHandler_Segment_SegmentedRemux(t *testing.T) {
	env := newStreamTestEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "segment00002.ts"), []byte("TS_BYTES"), 0o644); err != nil {
		t.Fatal(err)
	}
	registerSegmentedSession(env, dir)
	claims := &auth.Claims{UserID: "user-1"}

	rr := env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/720p/segment00002.ts", claims)
	if rr.Code != http.StatusOK || rr.Body.String() != "TS_BYTES" {
		t.Fatalf("existing segment: %d %q", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Errorf("content-type %q", ct)
	}

	// Fuera del plan (4 segmentos) → 404, no un reinicio de ffmpeg.
	if rr := env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/720p/segment00009.ts", claims); rr.Code != http.StatusNotFound {
		t.Fatalf("out of range: %d", rr.Code)
	}
	// Generación fallida (ffmpeg inexistente en el seam) → 503.
	if rr := env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/720p/segment00001.ts", claims); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed generation: %d %s", rr.Code, rr.Body.String())
	}
}
