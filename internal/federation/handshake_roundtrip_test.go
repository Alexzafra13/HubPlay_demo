package federation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/clock"
	"hubplay/internal/domain"
)

// TestHandshakeRoundtrip_TwoLiveServers — the integration smoke test
// that's been missing. We spin up TWO completely independent managers,
// each with its own httptest.Server wiring the public federation
// endpoints + the authenticated peer endpoint, then drive the full
// pair flow end-to-end.
//
// What this catches that unit tests miss:
//
//   1. JSON wire-format symmetry — what one side sends, the other
//      decodes correctly. Pubkey base64 round-trip in particular.
//   2. Manager.ProbePeer + Manager.AcceptInvite wired correctly
//      through real HTTP (the unit tests stub the HTTP client).
//   3. Both sides end with the OTHER server in their peers table
//      with status='paired'. The handshake is bidirectional —
//      a bug that persists only one side would land production.
//   4. The invite is consumed exactly once. A second AcceptInvite
//      with the same code returns 403.
//   5. After pairing, an outbound peer JWT signed by B and sent to
//      A's /peer/ping passes auth + rate limit + audit.
func TestHandshakeRoundtrip_TwoLiveServers(t *testing.T) {
	ctx := context.Background()
	clk := clock.New()

	a := startTestServer(t, ctx, clk, "Alex's Server")
	b := startTestServer(t, ctx, clk, "Pedro's Server")

	// Step 1: A generates an invite. The admin reads its fingerprint
	// out-of-band; we simulate that by capturing it for assertion.
	invite, err := a.mgr.GenerateInvite(ctx, "admin-uuid-A")
	if err != nil {
		t.Fatalf("A generate invite: %v", err)
	}

	// Step 2: B probes A's URL. This is what the admin UI does after
	// the operator pastes A's URL — it fetches A's ServerInfo so the
	// admin can compare fingerprints out-of-band before committing.
	aInfo, err := b.mgr.ProbePeer(ctx, a.url)
	if err != nil {
		t.Fatalf("B probe A: %v", err)
	}
	if aInfo.PubkeyFingerprint != a.mgr.PublicServerInfo().PubkeyFingerprint {
		t.Fatalf("fingerprint mismatch: probed=%q, A's own=%q",
			aInfo.PubkeyFingerprint, a.mgr.PublicServerInfo().PubkeyFingerprint)
	}
	if aInfo.ServerUUID != a.mgr.identity.Current().ServerUUID {
		t.Fatalf("server_uuid mismatch")
	}

	// Step 3: B accepts the invite. This is the trust commit — B
	// has confirmed A's fingerprint and is asking A to register B
	// as a peer + getting A's ServerInfo back to register A on B's
	// side. After this, both servers have each other paired.
	aAsPeerOnB, err := b.mgr.AcceptInvite(ctx, a.url, invite.Code)
	if err != nil {
		t.Fatalf("B accept A's invite: %v", err)
	}
	if aAsPeerOnB.Status != PeerPaired {
		t.Errorf("A on B side: status=%v, want paired", aAsPeerOnB.Status)
	}
	if aAsPeerOnB.ServerUUID != a.mgr.identity.Current().ServerUUID {
		t.Errorf("A on B side: server_uuid mismatch")
	}

	// Step 4: A's side. The handshake handler should have persisted B
	// during step 3's POST to /peer/handshake.
	aPeers, err := a.mgr.ListPeers(ctx)
	if err != nil {
		t.Fatalf("list peers on A: %v", err)
	}
	if len(aPeers) != 1 {
		t.Fatalf("A should have 1 peer, got %d", len(aPeers))
	}
	bAsPeerOnA := aPeers[0]
	if bAsPeerOnA.Status != PeerPaired {
		t.Errorf("B on A side: status=%v, want paired", bAsPeerOnA.Status)
	}
	if bAsPeerOnA.ServerUUID != b.mgr.identity.Current().ServerUUID {
		t.Errorf("B on A side: server_uuid mismatch")
	}
	if bAsPeerOnA.BaseURL != b.url {
		t.Errorf("B on A side: base_url=%q, want %q", bAsPeerOnA.BaseURL, b.url)
	}

	// Step 5: invite single-use enforcement. A second AcceptInvite
	// with the same code MUST fail — otherwise a leaked code could
	// pair an attacker too.
	_, err = b.mgr.AcceptInvite(ctx, a.url, invite.Code)
	if err == nil {
		t.Error("second use of the same invite should have failed")
	}

	// Step 6: outbound peer JWT roundtrip. B signs a token addressed
	// to A and hits A's /peer/ping. This exercises the middleware
	// end-to-end through real HTTP. IssuePeerToken takes the AUDIENCE
	// peer's LOCAL id on the issuing server — from B's side, A is
	// stored as aAsPeerOnB.
	tok, err := b.mgr.IssuePeerToken(aAsPeerOnB.ID)
	if err != nil {
		t.Fatalf("B issue peer token: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, a.url+"/api/v1/peer/ping", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call A /peer/ping: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/peer/ping status=%d, want 200", resp.StatusCode)
	}
	var pingResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&pingResp); err != nil {
		t.Fatal(err)
	}
	if pingResp["server_uuid"] != a.mgr.identity.Current().ServerUUID {
		t.Errorf("ping response server_uuid mismatch")
	}
}

// TestHandshakeRoundtrip_RejectsExpiredInvite ensures the time check
// in HandleInboundHandshake is honored when invites cross the wire.
func TestHandshakeRoundtrip_RejectsExpiredInvite(t *testing.T) {
	ctx := context.Background()
	frozen := &fixedClock{now: time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)}

	a := startTestServer(t, ctx, frozen, "ServerA")
	b := startTestServer(t, ctx, frozen, "ServerB")

	invite, err := a.mgr.GenerateInvite(ctx, "admin-A")
	if err != nil {
		t.Fatal(err)
	}

	// Fast-forward past the invite TTL.
	frozen.now = invite.ExpiresAt.Add(time.Minute)

	_, err = b.mgr.AcceptInvite(ctx, a.url, invite.Code)
	if err == nil {
		t.Fatal("expected expired-invite rejection")
	}
}

// TestHandshakeRoundtrip_PubkeyBase64Survives verifies that A's pubkey
// emitted in JSON, decoded by B, re-encoded for handshake POST, and
// stored on A's side matches the original bytes — protects against
// any future regression in the wire format.
func TestHandshakeRoundtrip_PubkeyBase64Survives(t *testing.T) {
	ctx := context.Background()
	clk := clock.New()
	a := startTestServer(t, ctx, clk, "ServerA")
	b := startTestServer(t, ctx, clk, "ServerB")

	originalAPub := []byte(a.mgr.identity.Current().PublicKey)

	invite, _ := a.mgr.GenerateInvite(ctx, "admin")

	// Probe + accept.
	if _, err := b.mgr.ProbePeer(ctx, a.url); err != nil {
		t.Fatal(err)
	}
	if _, err := b.mgr.AcceptInvite(ctx, a.url, invite.Code); err != nil {
		t.Fatal(err)
	}

	// Whatever B persisted as A's pubkey must equal the original bytes.
	bPeers, _ := b.mgr.ListPeers(ctx)
	if len(bPeers) != 1 {
		t.Fatalf("B should have 1 peer")
	}
	got := []byte(bPeers[0].PublicKey)
	if string(got) != string(originalAPub) {
		t.Errorf("pubkey survived poorly:\n  original: %x\n  on B:     %x", originalAPub, got)
	}
}

// ─── test rig ──────────────────────────────────────────────────────────

type testServer struct {
	mgr *Manager
	url string
}

func startTestServer(t *testing.T, ctx context.Context, clk clock.Clock, name string) *testServer {
	t.Helper()

	repo := &inMemoryFedRepo{}
	if _, err := LoadOrCreate(ctx, repo, clk, name); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.HTTPTimeout = 5 * time.Second
	mgr, err := NewManager(ctx, cfg, repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	r := chi.NewRouter()
	r.Get("/api/v1/federation/info", makeInfoHandler(mgr))
	r.Post("/api/v1/peer/handshake", makeHandshakeHandler(mgr))
	r.Group(func(r chi.Router) {
		r.Use(RequirePeerJWT(mgr))
		r.Get("/api/v1/peer/ping", makePingHandler(mgr))
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	mgr.SetAdvertisedURL(srv.URL)

	return &testServer{mgr: mgr, url: srv.URL}
}

// makeInfoHandler / makeHandshakeHandler / makePingHandler mirror the
// real handlers in `internal/api/handlers/federation_public.go` but
// keep this test self-contained (no import cycle on the handlers
// package, which itself imports federation).
//
// The wire shape MUST match what production emits — if these
// diverge from the production handlers, this test no longer reflects
// reality. The wireInfo struct definition below is the source of
// truth for the JSON shape and is mirrored in the production handler.

type wireInfo struct {
	ServerUUID        string   `json:"server_uuid"`
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	PublicKey         string   `json:"public_key"`
	PubkeyFingerprint string   `json:"pubkey_fingerprint"`
	PubkeyWords       []string `json:"pubkey_words"`
	SupportedScopes   []string `json:"supported_scopes"`
	AdvertisedURL     string   `json:"advertised_url"`
	AdminContact      string   `json:"admin_contact,omitempty"`
}

func toWire(info *ServerInfo) wireInfo {
	return wireInfo{
		ServerUUID:        info.ServerUUID,
		Name:              info.Name,
		Version:           info.Version,
		PublicKey:         base64.StdEncoding.EncodeToString(info.PublicKey),
		PubkeyFingerprint: info.PubkeyFingerprint,
		PubkeyWords:       info.PubkeyWords,
		SupportedScopes:   info.SupportedScopes,
		AdvertisedURL:     info.AdvertisedURL,
		AdminContact:      info.AdminContact,
	}
}

func fromWire(w wireInfo) (*ServerInfo, error) {
	pub, err := base64.StdEncoding.DecodeString(w.PublicKey)
	if err != nil {
		return nil, err
	}
	return &ServerInfo{
		ServerUUID:        w.ServerUUID,
		Name:              w.Name,
		Version:           w.Version,
		PublicKey:         pub,
		PubkeyFingerprint: Fingerprint(pub),
		PubkeyWords:       FingerprintWords(pub),
		SupportedScopes:   w.SupportedScopes,
		AdvertisedURL:     w.AdvertisedURL,
		AdminContact:      w.AdminContact,
	}, nil
}

func makeInfoHandler(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toWire(mgr.PublicServerInfo()))
	}
}

func makeHandshakeHandler(mgr *Manager) http.HandlerFunc {
	type req struct {
		Code       string   `json:"code"`
		RemoteInfo wireInfo `json:"remote_info"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var body req
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		remote, err := fromWire(body.RemoteInfo)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, ours, err := mgr.HandleInboundHandshake(r.Context(), body.Code, remote)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, domain.ErrInviteExpired) ||
				errors.Is(err, domain.ErrInviteAlreadyUsed) ||
				errors.Is(err, domain.ErrInviteNotFound) {
				status = http.StatusForbidden
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toWire(ours))
	}
}

func makePingHandler(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		peer := PeerFromContext(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"server_uuid":     mgr.identity.Current().ServerUUID,
			"now":             mgr.NowUTC().Format(time.RFC3339),
			"acknowledged_to": peer.ServerUUID,
		})
	}
}

