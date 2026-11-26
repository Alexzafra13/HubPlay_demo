package fedhandler

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hubplay/internal/federation"
	"hubplay/internal/stream"
)

// Un remux por segmentos servido a un peer: manifest con los cortes
// reales (URLs relativas al index.m3u8) y segmentos bajo demanda. Antes
// el handler solo sabía servir el manifest que escribe ffmpeg, que en
// este modo no existe.
func TestFederationStream_SegmentedRemux_ManifestAndSegments(t *testing.T) {
	env := newFedTestEnv(t)
	env.share(federation.ShareScopes{CanBrowse: true, CanPlay: true})

	pts := make([]stream.KeyPoint, 0, 6)
	for i := 0; i < 6; i++ {
		v := float64(i) * 4
		pts = append(pts, stream.KeyPoint{PTS: v, Lead: v})
	}
	plan := stream.BuildSegmentPlan(&stream.KeyframeIndex{Points: pts}, 24, 6) // 0, 8, 16
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "segment00001.ts"), []byte("TS1"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := stream.NewSegmentedSessionForTest("k", env.itemID, dir, plan)
	t.Cleanup(sess.Stop)
	ms := &stream.ManagedSession{
		Session:  sess,
		Decision: stream.PlaybackDecision{Method: stream.MethodDirectStream, CopyVideo: true},
	}
	env.streams.startSessionFn = func(_ context.Context, req stream.StartSessionRequest) (*stream.ManagedSession, error) {
		env.streams.mu.Lock()
		env.streams.sessions[stream.SessionKey(req.UserID, req.ItemID, req.ProfileName, -1, -1)] = ms
		env.streams.mu.Unlock()
		return ms, nil
	}
	sid := env.startSessionAndGetSID(t)

	get := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/api/v1/peer/stream/session/"+sid+path, nil)
		req.Header.Set("Authorization", "Bearer "+env.peerToken())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close() //nolint:errcheck
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	code, body := get("/720p/index.m3u8")
	if code != http.StatusOK {
		t.Fatalf("manifest: %d %s", code, body)
	}
	if strings.Count(body, "#EXTINF:8.000000,") != 3 || !strings.Contains(body, "\nsegment00002.ts\n") {
		t.Fatalf("manifest sin los cortes reales o con URLs no relativas:\n%s", body)
	}
	if code, body := get("/720p/segment00001.ts"); code != http.StatusOK || body != "TS1" {
		t.Fatalf("segment: %d %q", code, body)
	}
	if code, _ := get("/720p/segment00007.ts"); code != http.StatusNotFound {
		t.Fatalf("out of range: %d", code)
	}
}
