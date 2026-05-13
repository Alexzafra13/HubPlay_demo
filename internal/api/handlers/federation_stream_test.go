// Federation stream + image origin handler integration tests.
//
// These tests stand up a real federation.Manager backed by a real
// SQLite DB (via testutil.NewTestDB) so the share / peer / audit
// plumbing exercises the same SQL the production server uses. The
// only stubs are the StreamManagerService (we don't want ffmpeg in
// unit tests) and the items repo (so we don't have to scan a real
// media file).
//
// What we cover that the share-level tests in internal/federation
// don't:
//
//   1. The HTTP layer's 404 conflation: a peer without can_play
//      should NOT be told the item exists. Same body as a missing
//      item -- attackers can't enumerate content by polling.
//   2. The HTTP layer's 200 path on can_play=true: the handler
//      builds the master_path and registers the session UUID.
//   3. The federation poster route's 404 path when the peer has no
//      share for the item's library.

package handlers

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"hubplay/internal/clock"
	"hubplay/internal/db"
	"hubplay/internal/federation"
	"hubplay/internal/stream"
	"hubplay/internal/testutil"
)

// fedTestEnv pairs everything a federation HTTP test needs: real
// manager, real DB, the per-test paired peer's keypair (so the test
// can mint outbound JWTs from the peer's perspective), the items
// repo stub, and the chi router with the handler mounted.
type fedTestEnv struct {
	t            *testing.T
	mgr          *federation.Manager
	clk          clock.Clock
	rawDB        *sql.DB
	peerID       string
	peerSrvUUID  string
	peerPriv     ed25519.PrivateKey
	libraryID    string
	itemID       string
	userID       string
	items        *streamFakeItemRepo
	streams      *fakeStreamManager
	mediaStreams *fakeMediaStreamRepoForFedTest
	fedHandler   *FederationStreamHandler
	srv          *httptest.Server
	imageDir     string
}

func newFedTestEnv(t *testing.T) *fedTestEnv {
	t.Helper()
	ctx := context.Background()
	clk := clock.New()
	rawDB := testutil.NewTestDB(t)
	fedRepo := db.NewFederationRepository(testutil.Driver(), rawDB)

	if _, err := federation.LoadOrCreate(ctx, fedRepo, clk, "TestServer"); err != nil {
		t.Fatalf("load or create identity: %v", err)
	}
	mgr, err := federation.NewManager(ctx, federation.DefaultConfig(), fedRepo, clk, nil, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(mgr.Close)

	// Pair a synthetic remote peer. We hold its private key so we
	// can mint JWTs as if the peer were calling us.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519: %v", err)
	}
	now := clk.Now()
	peerID := uuid.NewString()
	peerSrvUUID := uuid.NewString()
	peer := &federation.Peer{
		ID:         peerID,
		ServerUUID: peerSrvUUID,
		Name:       "TestPeer",
		BaseURL:    "https://test.peer.invalid",
		PublicKey:  pub,
		Status:     federation.PeerPaired,
		CreatedAt:  now,
		PairedAt:   &now,
	}
	if err := fedRepo.InsertPeer(ctx, peer); err != nil {
		t.Fatalf("insert peer: %v", err)
	}
	// Force the manager to refresh its in-memory cache of paired
	// peers so JWT validation finds the new peer.
	if err := mgr.RefreshPeerCache(ctx); err != nil {
		t.Fatalf("refresh peer cache: %v", err)
	}

	// Local content. Library + a single movie item; library_id is
	// what shares key on, item.parent_id is NULL so the catalog
	// browse path includes it.
	userID := uuid.NewString()
	testutil.Exec(t, rawDB, `INSERT INTO users (id, username, display_name, password_hash, role, created_at) VALUES (?, 'admin', 'Admin', 'x', 'admin', ?)`, userID, now)
	libraryID := uuid.NewString()
	testutil.Exec(t, rawDB, `INSERT INTO libraries (id, name, content_type) VALUES (?, 'Movies', 'movies')`, libraryID)
	itemID := uuid.NewString()
	testutil.Exec(t, rawDB, `
		INSERT INTO items (id, library_id, type, title, sort_title, year, path)
		VALUES (?, ?, 'movie', 'Test Movie', 'test movie', 2026, '/tmp/test.mkv')
	`, itemID, libraryID)

	// Items repo stub backed by a one-row map -- the handler only
	// reads .ID + .LibraryID.
	items := &streamFakeItemRepo{byID: map[string]*db.Item{
		itemID: {ID: itemID, LibraryID: libraryID, Type: "movie", Title: "Test Movie"},
	}}

	// Stream manager fake. Returns a synthetic ManagedSession for
	// the can_play=true path; the can_play=false path never reaches
	// it because the share gate fires first.
	streams := newFakeStreamManager()
	streams.startSessionFn = func(_ context.Context, _, _, _ string, _ *stream.Capabilities, _ float64) (*stream.ManagedSession, error) {
		return &stream.ManagedSession{
			Decision: stream.PlaybackDecision{Method: stream.MethodTranscode},
		}, nil
	}

	// Router mounting the handler under the same RequirePeerJWT
	// middleware production uses. Skipping the rate-limit + audit
	// middlewares -- they have their own dedicated tests.
	mediaStreams := &fakeMediaStreamRepoForFedTest{byItem: map[string][]*db.MediaStream{}}
	fedHandler := NewFederationStreamHandler(mgr, streams, items, mediaStreams, testutil.NopLogger())

	imageDir := t.TempDir()
	imgSrv := NewImageHandler(nil, nil, nil, nil, nil, imageDir, testutil.NopLogger())
	fedImg := NewFederationImageHandler(mgr, items,
		&fakeImageRepoForFedTest{primaryByItem: map[string]string{}},
		imgSrv, testutil.NopLogger())

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(federation.RequirePeerJWT(mgr))
		r.Post("/api/v1/peer/stream/{itemId}/session", fedHandler.StartSession)
		r.Get("/api/v1/peer/stream/session/{sessionId}/subtitles", fedHandler.Subtitles)
		r.Get("/api/v1/peer/items/{itemId}/poster", fedImg.ItemPoster)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &fedTestEnv{
		t:            t,
		mgr:          mgr,
		clk:          clk,
		rawDB:        rawDB,
		peerID:       peerID,
		peerSrvUUID:  peerSrvUUID,
		peerPriv:     priv,
		libraryID:    libraryID,
		itemID:       itemID,
		userID:       userID,
		items:        items,
		streams:      streams,
		mediaStreams: mediaStreams,
		fedHandler:   fedHandler,
		srv:          srv,
		imageDir:     imageDir,
	}
}

// share ensures (peer, library) has a share row with the given scopes.
// Idempotent — re-calling overrides the prior scopes.
func (e *fedTestEnv) share(scopes federation.ShareScopes) {
	e.t.Helper()
	if _, err := e.mgr.ShareLibrary(context.Background(), e.peerID, e.libraryID, e.userID, scopes); err != nil {
		e.t.Fatalf("share library: %v", err)
	}
}

// peerToken mints a JWT signed by the synthetic peer, audience = our
// server (so RequirePeerJWT accepts it).
func (e *fedTestEnv) peerToken() string {
	e.t.Helper()
	tok, err := federation.IssuePeerToken(
		e.clk, e.peerPriv, e.peerSrvUUID,
		e.mgr.PublicServerInfo().ServerUUID,
	)
	if err != nil {
		e.t.Fatalf("issue peer token: %v", err)
	}
	return tok
}

func (e *fedTestEnv) postSession(t *testing.T) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost,
		e.srv.URL+"/api/v1/peer/stream/"+e.itemID+"/session",
		strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.peerToken())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestFederationStream_StartSession_NoCanPlay_Returns404(t *testing.T) {
	env := newFedTestEnv(t)

	// can_browse but NOT can_play. The handler must conflate "can't
	// play" with "doesn't exist" so a peer can't enumerate items by
	// polling for 403 vs 404 differences.
	env.share(federation.ShareScopes{CanBrowse: true, CanPlay: false})

	resp := env.postSession(t)
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errObj, _ := body["error"].(map[string]any)
	if code, _ := errObj["code"].(string); code != "ITEM_NOT_FOUND" {
		t.Errorf("error.code = %q, want ITEM_NOT_FOUND", code)
	}
}

func TestFederationStream_StartSession_CanPlay_Returns200(t *testing.T) {
	env := newFedTestEnv(t)
	env.share(federation.ShareScopes{CanBrowse: true, CanPlay: true})

	resp := env.postSession(t)
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sid, _ := body["session_id"].(string)
	if sid == "" {
		t.Fatal("session_id empty in response")
	}
	mp, _ := body["master_path"].(string)
	if !strings.HasPrefix(mp, "/api/v1/peer/stream/session/") {
		t.Errorf("master_path = %q, missing peer-session prefix", mp)
	}
}

// ─── Federation poster route ──────────────────────────────────────

// fakeImageRepoForFedTest is a tiny ImageRepository implementation
// just for federation poster tests. We only need GetPrimaryURLs;
// the rest are no-ops to satisfy the interface.
type fakeImageRepoForFedTest struct {
	primaryByItem map[string]string // item_id -> image_id (empty = no poster → 404)
}

func (r *fakeImageRepoForFedTest) GetPrimaryURLs(_ context.Context, ids []string) (map[string]map[string]db.PrimaryImageRef, error) {
	out := make(map[string]map[string]db.PrimaryImageRef, len(ids))
	for _, id := range ids {
		fn, ok := r.primaryByItem[id]
		if !ok {
			continue
		}
		out[id] = map[string]db.PrimaryImageRef{
			"primary": {Path: "/api/v1/images/file/" + fn},
		}
	}
	return out, nil
}
func (r *fakeImageRepoForFedTest) ListByItem(context.Context, string) ([]*db.Image, error) {
	return nil, nil
}
func (r *fakeImageRepoForFedTest) Create(context.Context, *db.Image) error                  { return nil }
func (r *fakeImageRepoForFedTest) SetPrimary(context.Context, string, string, string) error { return nil }
func (r *fakeImageRepoForFedTest) SetLocked(context.Context, string, bool) error            { return nil }
func (r *fakeImageRepoForFedTest) GetByID(context.Context, string) (*db.Image, error) {
	return nil, nil
}
func (r *fakeImageRepoForFedTest) DeleteByID(context.Context, string) error { return nil }

func TestFederationImage_ItemPoster_NoCanBrowse_Returns404(t *testing.T) {
	env := newFedTestEnv(t)
	// No share at all -> peer has no library access -> 404 on poster.
	// Same conflation as the catalog: peer must not learn the item exists.

	req, _ := http.NewRequest(http.MethodGet,
		env.srv.URL+"/api/v1/peer/items/"+env.itemID+"/poster", nil)
	req.Header.Set("Authorization", "Bearer "+env.peerToken())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// Compile-time interface check.
var _ ImageRepository = (*fakeImageRepoForFedTest)(nil)

// ─── Federated subtitle list endpoint ─────────────────────────────────────

// fakeMediaStreamRepoForFedTest is a tiny MediaStreamRepository
// implementation backed by a per-itemID slice. The federation
// subtitle handler is the only consumer in these tests, so we don't
// need to model anything else.
type fakeMediaStreamRepoForFedTest struct {
	byItem map[string][]*db.MediaStream
}

func (r *fakeMediaStreamRepoForFedTest) ListByItem(_ context.Context, itemID string) ([]*db.MediaStream, error) {
	return r.byItem[itemID], nil
}

var _ MediaStreamRepository = (*fakeMediaStreamRepoForFedTest)(nil)

// startSessionAndGetSID drives StartSession through the handler and
// returns the freshly-minted session UUID. Reused by the subtitle
// list / track tests so each one doesn't have to re-derive how
// session UUIDs are produced.
func (e *fedTestEnv) startSessionAndGetSID(t *testing.T) string {
	t.Helper()
	resp := e.postSession(t)
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start session: status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sid, _ := body["session_id"].(string)
	if sid == "" {
		t.Fatal("session_id empty in start-session response")
	}
	return sid
}

// getSubtitles GETs the federated subtitle list for the given session
// UUID with a fresh peer JWT.
func (e *fedTestEnv) getSubtitles(t *testing.T, sessionID string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet,
		e.srv.URL+"/api/v1/peer/stream/session/"+sessionID+"/subtitles", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.peerToken())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestFederationStream_Subtitles_ReturnsOnlySubtitleStreams(t *testing.T) {
	env := newFedTestEnv(t)
	env.share(federation.ShareScopes{CanBrowse: true, CanPlay: true})

	// Mixed media-stream rows: video, audio, two subtitle tracks.
	// The handler must return only the subtitle ones, in the same
	// shape as the local /stream/{itemId}/subtitles handler.
	env.mediaStreams.byItem[env.itemID] = []*db.MediaStream{
		{StreamType: "video", StreamIndex: 0, Codec: "h264"},
		{StreamType: "audio", StreamIndex: 1, Codec: "ac3", Language: "eng"},
		{StreamType: "subtitle", StreamIndex: 2, Codec: "subrip", Language: "eng", Title: "English", IsDefault: true},
		{StreamType: "subtitle", StreamIndex: 3, Codec: "subrip", Language: "spa", Title: "Español"},
	}

	sid := env.startSessionAndGetSID(t)
	resp := env.getSubtitles(t, sid)
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			Index    int    `json:"index"`
			Codec    string `json:"codec"`
			Language string `json:"language"`
			Title    string `json:"title"`
			Forced   bool   `json:"forced"`
			Default  bool   `json:"default"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 2 {
		t.Fatalf("data length = %d, want 2 subtitle entries (got %+v)", len(body.Data), body.Data)
	}
	if body.Data[0].Index != 2 || body.Data[0].Language != "eng" || !body.Data[0].Default {
		t.Errorf("first track = %+v, want index=2 lang=eng default=true", body.Data[0])
	}
	if body.Data[1].Index != 3 || body.Data[1].Language != "spa" {
		t.Errorf("second track = %+v, want index=3 lang=spa", body.Data[1])
	}
}

func TestFederationStream_Subtitles_UnknownSession_Returns404(t *testing.T) {
	env := newFedTestEnv(t)
	env.share(federation.ShareScopes{CanBrowse: true, CanPlay: true})

	// Session UUID conflated to "not found" -- otherwise a peer
	// could enumerate other peers' active sessions.
	resp := env.getSubtitles(t, uuid.NewString())
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestFederationStream_Subtitles_ForeignPeerSession_Returns404(t *testing.T) {
	env := newFedTestEnv(t)
	env.share(federation.ShareScopes{CanBrowse: true, CanPlay: true})

	// Pair a SECOND peer and mint a JWT for it. Try to read the
	// first peer's session via the second peer's token -- must 404
	// (peer ID mismatch in lookupPeerSession). Same enumeration
	// guard as the StartSession path.
	sid := env.startSessionAndGetSID(t)

	pub2, priv2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519: %v", err)
	}
	now := env.clk.Now()
	peer2ID := uuid.NewString()
	peer2SrvUUID := uuid.NewString()
	if err := db.NewFederationRepository(testutil.Driver(), env.rawDB).InsertPeer(context.Background(), &federation.Peer{
		ID:         peer2ID,
		ServerUUID: peer2SrvUUID,
		Name:       "OtherPeer",
		BaseURL:    "https://other.peer.invalid",
		PublicKey:  pub2,
		Status:     federation.PeerPaired,
		CreatedAt:  now,
		PairedAt:   &now,
	}); err != nil {
		t.Fatalf("insert peer2: %v", err)
	}
	if err := env.mgr.RefreshPeerCache(context.Background()); err != nil {
		t.Fatalf("refresh peer cache: %v", err)
	}
	tok2, err := federation.IssuePeerToken(env.clk, priv2, peer2SrvUUID, env.mgr.PublicServerInfo().ServerUUID)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet,
		env.srv.URL+"/api/v1/peer/stream/session/"+sid+"/subtitles", nil)
	req.Header.Set("Authorization", "Bearer "+tok2)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (peer mismatch)", resp.StatusCode)
	}
}

func TestFederationStream_Subtitles_NoMatchingTracks_ReturnsEmpty(t *testing.T) {
	env := newFedTestEnv(t)
	env.share(federation.ShareScopes{CanBrowse: true, CanPlay: true})

	// Only audio + video, no subs. Handler must respond 200 with
	// an empty data array (NOT 404 -- the item exists, it just has
	// no embedded subs).
	env.mediaStreams.byItem[env.itemID] = []*db.MediaStream{
		{StreamType: "video", StreamIndex: 0, Codec: "h264"},
		{StreamType: "audio", StreamIndex: 1, Codec: "aac"},
	}

	sid := env.startSessionAndGetSID(t)
	resp := env.getSubtitles(t, sid)
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 0 {
		t.Errorf("data length = %d, want 0", len(body.Data))
	}
}
