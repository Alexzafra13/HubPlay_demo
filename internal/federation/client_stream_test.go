package federation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hubplay/internal/clock"
)

// TestStartPeerStreamSession_Roundtrip verifies the wire contract for
// the Phase 5 stream-session start call from peer A's perspective:
//   1. The outbound request is a POST to /api/v1/peer/stream/{itemId}/session.
//   2. The Authorization header carries a peer JWT.
//   3. The body is the documented PeerStreamSessionRequest shape with
//      capabilities forwarded verbatim.
//   4. A 200 with {session_id, method, master_path} is decoded
//      correctly into a PeerStreamSessionResponse.
//
// What this test does NOT cover (deferred to slice 2):
//   - The HLS proxy round-trip (master.m3u8 + segments). That needs a
//     real stream.Manager + ffmpeg, neither of which we want to spin
//     up under unit tests.
//   - Per-peer caps + federation_progress. Those features aren't
//     in slice 1 at all.
func TestStartPeerStreamSession_Roundtrip(t *testing.T) {
	allowLoopbackForTests(t)
	ctx := context.Background()
	clk := clock.New()

	// Stand up a fake "peer B" HTTP server. It records the inbound
	// request so we can assert on shape, then returns canned JSON.
	var (
		gotMethod      string
		gotPath        string
		gotAuthScheme  string
		gotContentType string
		gotBody        peerStreamSessionRequestWireForTest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if h := r.Header.Get("Authorization"); h != "" {
			parts := strings.SplitN(h, " ", 2)
			gotAuthScheme = parts[0]
		}
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"session_id": "11111111-1111-1111-1111-111111111111",
			"method": "transcode",
			"master_path": "/api/v1/peer/stream/session/11111111-1111-1111-1111-111111111111/master.m3u8"
		}`)
	}))
	defer srv.Close()

	// Manager wired with a single paired peer pointing at srv.URL.
	// Pubkeys: a real Ed25519 pair so the JWT we sign is well-formed
	// (we don't validate it on the receiving side here, but we don't
	// want IssuePeerToken to fail for missing keys either).
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	repo := &inMemoryFedRepo{}
	if _, err := LoadOrCreate(ctx, repo, clk, "Tester"); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.HTTPTimeout = 5 * time.Second
	mgr, err := NewManager(ctx, cfg, repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	pairedAt := clk.Now()
	peer := &Peer{
		ID:         "peer-b",
		ServerUUID: "00000000-0000-0000-0000-00000000000b",
		Name:       "B",
		BaseURL:    srv.URL,
		PublicKey:  pub,
		Status:     PeerPaired,
		CreatedAt:  pairedAt,
		PairedAt:   &pairedAt,
	}
	if err := repo.InsertPeer(ctx, peer); err != nil {
		t.Fatal(err)
	}
	if err := mgr.refreshPeerCache(ctx); err != nil {
		t.Fatal(err)
	}

	// Drive the call.
	resp, err := mgr.StartPeerStreamSession(ctx, "peer-b", "item-42", PeerStreamSessionRequest{
		Profile: "1080p",
		Capabilities: &PeerStreamCapabilities{
			Video:     []string{"h264", "hevc"},
			Audio:     []string{"aac", "eac3"},
			Container: []string{"mp4", "mkv"},
		},
	})
	if err != nil {
		t.Fatalf("StartPeerStreamSession: %v", err)
	}

	// ── Outbound request shape ─────────────────────────────────
	if gotMethod != http.MethodPost {
		t.Errorf("method: got %q want POST", gotMethod)
	}
	if gotPath != "/api/v1/peer/stream/item-42/session" {
		t.Errorf("path: got %q want /api/v1/peer/stream/item-42/session", gotPath)
	}
	if gotAuthScheme != "Bearer" {
		t.Errorf("auth scheme: got %q want Bearer", gotAuthScheme)
	}
	if !strings.HasPrefix(gotContentType, "application/json") {
		t.Errorf("content-type: got %q want application/json", gotContentType)
	}
	if gotBody.Profile != "1080p" {
		t.Errorf("profile in body: got %q want 1080p", gotBody.Profile)
	}
	if gotBody.Capabilities == nil {
		t.Fatal("capabilities missing in body")
	}
	if !sliceEq(gotBody.Capabilities.Video, []string{"h264", "hevc"}) {
		t.Errorf("video caps mismatch: %v", gotBody.Capabilities.Video)
	}
	if !sliceEq(gotBody.Capabilities.Audio, []string{"aac", "eac3"}) {
		t.Errorf("audio caps mismatch: %v", gotBody.Capabilities.Audio)
	}

	// ── Response decoding ─────────────────────────────────────
	if resp.SessionID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("session id: got %q", resp.SessionID)
	}
	if resp.Method != "transcode" {
		t.Errorf("method: got %q", resp.Method)
	}
	if !strings.HasSuffix(resp.MasterPath, "/master.m3u8") {
		t.Errorf("master path shape: got %q", resp.MasterPath)
	}
}

// peerStreamSessionRequestWireForTest mirrors the handler-side wire
// shape (handlers.peerStreamSessionRequestWire) so we can decode the
// outbound body in this test without importing the handlers package.
// Same fields, same JSON tags.
type peerStreamSessionRequestWireForTest struct {
	Profile      string                  `json:"profile,omitempty"`
	Capabilities *PeerStreamCapabilities `json:"client_capabilities,omitempty"`
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
