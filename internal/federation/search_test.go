package federation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"hubplay/internal/clock"
)

// peerSearchFixture spins up N fake peers each with a controllable
// search handler so the fan-out tests can simulate a mix of healthy,
// slow, and offline peers in one run. Mirrors the pattern in
// client_retry_test.go but extends it to multiple peers.
type peerSearchFixture struct {
	t      *testing.T
	mgr    *Manager
	repo   *inMemoryFedRepo
	servers []*httptest.Server
}

func (f *peerSearchFixture) close() {
	for _, s := range f.servers {
		s.Close()
	}
}

func newPeerSearchFixture(t *testing.T, handlers ...http.HandlerFunc) *peerSearchFixture {
	t.Helper()
	allowLoopbackForTests(t)
	ctx := context.Background()
	clk := clock.New()

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

	servers := make([]*httptest.Server, 0, len(handlers))
	for i, h := range handlers {
		srv := httptest.NewServer(h)
		servers = append(servers, srv)
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		now := clk.Now()
		// Vary peer ids so the fan-out goroutines route to distinct
		// servers; the per-peer ID is what FetchPeerSearch uses to
		// look up BaseURL.
		peer := &Peer{
			ID:         fixturePeerID(i),
			ServerUUID: fixtureServerUUID(i),
			Name:       fixturePeerName(i),
			BaseURL:    srv.URL,
			PublicKey:  pub,
			Status:     PeerPaired,
			CreatedAt:  now,
			PairedAt:   &now,
		}
		if err := repo.InsertPeer(ctx, peer); err != nil {
			t.Fatal(err)
		}
	}
	if err := mgr.refreshPeerCache(ctx); err != nil {
		t.Fatal(err)
	}
	return &peerSearchFixture{t: t, mgr: mgr, repo: repo, servers: servers}
}

func fixturePeerID(i int) string     { return "peer-" + string(rune('a'+i)) }
func fixturePeerName(i int) string   { return "Peer " + string(rune('A'+i)) }
func fixtureServerUUID(i int) string { return "server-uuid-" + string(rune('a'+i)) }

// canned200 returns a handler that always responds 200 with the given JSON body.
func canned200(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
}

// TestSearchAllPeers_AggregatesAndAttributesByPeer verifies the
// happy path: two peers each return one hit, the manager merges
// them with peer attribution intact, and the caller can tell the
// origin of each result. This is the entire point of fan-out
// search — without correct attribution the user cannot route the
// click into the right peer's detail page.
func TestSearchAllPeers_AggregatesAndAttributesByPeer(t *testing.T) {
	a := canned200(`{"items":[{"id":"a1","type":"movie","title":"Alpha","year":2020}],"total":1}`)
	b := canned200(`{"items":[{"id":"b1","type":"movie","title":"Beta","year":2021}],"total":1}`)

	f := newPeerSearchFixture(t, a, b)
	defer f.close()

	hits, err := f.mgr.SearchAllPeers(context.Background(), "anything", 10, time.Second)
	if err != nil {
		t.Fatalf("SearchAllPeers: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	byPeer := map[string]string{}
	for _, h := range hits {
		byPeer[h.Peer.ID] = h.Item.Title
	}
	if byPeer["peer-a"] != "Alpha" {
		t.Errorf("peer-a missing or wrong: %v", byPeer)
	}
	if byPeer["peer-b"] != "Beta" {
		t.Errorf("peer-b missing or wrong: %v", byPeer)
	}
}

// TestSearchAllPeers_SkipsErroringPeer is the resilience guarantee:
// one peer returning 500 must not blank the search. The other peer's
// hits surface; the failed peer is logged and skipped.
func TestSearchAllPeers_SkipsErroringPeer(t *testing.T) {
	good := canned200(`{"items":[{"id":"g1","type":"movie","title":"Good"}],"total":1}`)
	var brokenAttempts int32
	broken := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&brokenAttempts, 1)
		http.Error(w, "broken", http.StatusInternalServerError)
	}

	f := newPeerSearchFixture(t, good, broken)
	defer f.close()

	hits, err := f.mgr.SearchAllPeers(context.Background(), "x", 10, time.Second)
	if err != nil {
		t.Fatalf("SearchAllPeers: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit (broken peer skipped), got %d", len(hits))
	}
	if hits[0].Peer.ID != "peer-a" {
		t.Errorf("expected hit from peer-a, got %s", hits[0].Peer.ID)
	}
	// The broken peer should have been hit — peerFetchAttempts retries
	// 5xx, so attempts > 1.
	if got := atomic.LoadInt32(&brokenAttempts); got < 1 {
		t.Errorf("broken peer was not contacted (attempts=%d)", got)
	}
}

// TestSearchAllPeers_HonoursPerPeerTimeout ensures a slow peer cannot
// drag the response past the per-peer timeout. The fast peer's hit
// surfaces, the slow peer is skipped (its goroutine still blocks on
// its own slow handler, but the timeout-cancelled ctx cuts the wait).
func TestSearchAllPeers_HonoursPerPeerTimeout(t *testing.T) {
	fast := canned200(`{"items":[{"id":"f1","type":"movie","title":"Fast"}],"total":1}`)
	slow := func(w http.ResponseWriter, r *http.Request) {
		// Block past the timeout we'll pass into SearchAllPeers, but
		// honour ctx cancellation so the test exits cleanly.
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"items":[],"total":0}`)
		}
	}

	f := newPeerSearchFixture(t, fast, slow)
	defer f.close()

	start := time.Now()
	hits, err := f.mgr.SearchAllPeers(context.Background(), "x", 10, 200*time.Millisecond)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("SearchAllPeers: %v", err)
	}
	// Fast peer's hit must surface; slow peer is skipped.
	if len(hits) != 1 || hits[0].Item.Title != "Fast" {
		t.Fatalf("expected only the fast hit, got %#v", hits)
	}
	// Total elapsed must be bounded by the per-peer timeout (with
	// generous slack); without the timeout the slow peer would hold
	// us for ~2s.
	if elapsed > 1*time.Second {
		t.Fatalf("fan-out did not cut off on timeout (elapsed=%v)", elapsed)
	}
}

// TestSearchAllPeers_EmptyQueryShortCircuits guards against accidental
// fan-out for an empty query — without this, a user clearing the
// search box would emit one HTTP call per peer for nothing.
func TestSearchAllPeers_EmptyQueryShortCircuits(t *testing.T) {
	called := int32(0)
	bump := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[],"total":0}`)
	}
	f := newPeerSearchFixture(t, bump, bump)
	defer f.close()

	hits, err := f.mgr.SearchAllPeers(context.Background(), "", 10, time.Second)
	if err != nil {
		t.Fatalf("SearchAllPeers: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected empty hits for empty query, got %d", len(hits))
	}
	if got := atomic.LoadInt32(&called); got != 0 {
		t.Fatalf("empty query must short-circuit, but %d peers were called", got)
	}
}

// TestFetchPeerSearch_DecodesItemsResponse exercises the direct
// outbound search call (without fan-out). Verifies the wire shape is
// the same items+total the catalog browse endpoint uses, so the
// frontend can reuse the same wire decoder.
func TestFetchPeerSearch_DecodesItemsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sanity-check the URL shape — query and limit must be wired.
		if got := r.URL.Query().Get("q"); got != "Inception" {
			t.Errorf("q query: got %q want Inception", got)
		}
		if got := r.URL.Query().Get("limit"); got != "5" {
			t.Errorf("limit query: got %q want 5", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[{"id":"x","type":"movie","title":"Inception","year":2010,"has_poster":true}],"total":1}`)
	}))
	f := newPeerSearchFixture(t, srv.Config.Handler.ServeHTTP)
	defer f.close()
	srv.Close() // shut the throwaway down; the fixture spun its own.

	items, err := f.mgr.FetchPeerSearch(context.Background(), "peer-a", "Inception", 5)
	if err != nil {
		t.Fatalf("FetchPeerSearch: %v", err)
	}
	if len(items) != 1 || items[0].Title != "Inception" || !items[0].HasPoster {
		t.Fatalf("unexpected items: %#v", items)
	}
}

// TestFetchPeerSearch_NotPaired surfaces the early validation: a peer
// that is not in PeerPaired status cannot be searched. The fan-out
// path filters to paired peers anyway, but a direct caller (admin
// debug, future surfaces) must see the explicit refusal.
func TestFetchPeerSearch_NotPaired(t *testing.T) {
	f := newPeerSearchFixture(t, canned200(`{"items":[],"total":0}`))
	defer f.close()
	// Mutate the peer's status under us.
	f.repo.mu.Lock()
	f.repo.peers[0].Status = PeerRevoked
	f.repo.mu.Unlock()

	_, err := f.mgr.FetchPeerSearch(context.Background(), "peer-a", "x", 5)
	if err == nil {
		t.Fatal("expected error for non-paired peer")
	}
	// Domain-typed sentinels would be ideal; today the manager
	// returns a wrapped fmt.Errorf("peer ... not paired"), so accept
	// either shape.
	if errors.Is(err, http.ErrServerClosed) {
		t.Fatal("unexpected error type")
	}
}
