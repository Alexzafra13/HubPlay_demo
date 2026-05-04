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
	t           *testing.T
	mgr         *federation.Manager
	clk         clock.Clock
	rawDB       *sql.DB
	peerID      string
	peerSrvUUID string
	peerPriv    ed25519.PrivateKey
	libraryID   string
	itemID      string
	userID      string
	items       *streamFakeItemRepo
	streams     *fakeStreamManager
	srv         *httptest.Server
	imageDir    string
}

func newFedTestEnv(t *testing.T) *fedTestEnv {
	t.Helper()
	ctx := context.Background()
	clk := clock.New()
	rawDB := testutil.NewTestDB(t)
	fedRepo := db.NewFederationRepository(rawDB)

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
	if _, err := rawDB.Exec(`INSERT INTO users (id, username, display_name, password_hash, role, created_at) VALUES (?, 'admin', 'Admin', 'x', 'admin', ?)`, userID, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	libraryID := uuid.NewString()
	if _, err := rawDB.Exec(`INSERT INTO libraries (id, name, content_type) VALUES (?, 'Movies', 'movies')`, libraryID); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	itemID := uuid.NewString()
	if _, err := rawDB.Exec(`
		INSERT INTO items (id, library_id, type, title, sort_title, year, path)
		VALUES (?, ?, 'movie', 'Test Movie', 'test movie', 2026, '/tmp/test.mkv')
	`, itemID, libraryID); err != nil {
		t.Fatalf("insert item: %v", err)
	}

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
	fedHandler := NewFederationStreamHandler(mgr, streams, items, testutil.NopLogger())

	imageDir := t.TempDir()
	imgSrv := NewImageHandler(nil, nil, nil, nil, nil, imageDir, testutil.NopLogger())
	fedImg := NewFederationImageHandler(mgr, items,
		&fakeImageRepoForFedTest{primaryByItem: map[string]string{}},
		imgSrv, testutil.NopLogger())

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(federation.RequirePeerJWT(mgr))
		r.Post("/api/v1/peer/stream/{itemId}/session", fedHandler.StartSession)
		r.Get("/api/v1/peer/items/{itemId}/poster", fedImg.ItemPoster)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &fedTestEnv{
		t:           t,
		mgr:         mgr,
		clk:         clk,
		rawDB:       rawDB,
		peerID:      peerID,
		peerSrvUUID: peerSrvUUID,
		peerPriv:    priv,
		libraryID:   libraryID,
		itemID:      itemID,
		userID:      userID,
		items:       items,
		streams:     streams,
		srv:         srv,
		imageDir:    imageDir,
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
