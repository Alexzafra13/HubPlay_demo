package federation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hubplay/internal/clock"
)

// inMemoryAuditRepo + inMemoryFedRepo glue together what the
// middleware needs: peer lookup, audit insert. Kept here (not in a
// shared testutil) because the surface is small and these tests are
// the only consumer for now.

type inMemoryFedRepo struct {
	mu      sync.Mutex
	id      *Identity
	peers   []*Peer
	audit   []*AuditEntry
	invites []*Invite
	shares  []*LibraryShare
	libs    []*SharedLibrary
	items   map[string][]*SharedItem // library_id → items
	cache   map[string]cacheEntry    // (peer_id|library_id) → entry
}

type cacheEntry struct {
	items    []*SharedItem
	cachedAt time.Time
}

func (r *inMemoryFedRepo) GetIdentity(_ context.Context) (*Identity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.id, nil
}

func (r *inMemoryFedRepo) InsertIdentity(_ context.Context, id *Identity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.id = id
	return nil
}

func (r *inMemoryFedRepo) InsertAuditEntry(_ context.Context, e *AuditEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *e
	r.audit = append(r.audit, &cp)
	return nil
}

func (r *inMemoryFedRepo) InsertInvite(_ context.Context, inv *Invite) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invites = append(r.invites, inv)
	return nil
}
func (r *inMemoryFedRepo) GetInviteByCode(_ context.Context, code string) (*Invite, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, inv := range r.invites {
		if inv.Code == code {
			return inv, nil
		}
	}
	return nil, nil
}
func (r *inMemoryFedRepo) MarkInviteUsed(_ context.Context, _ string, _ string, _ time.Time) error {
	return nil
}
func (r *inMemoryFedRepo) ListActiveInvites(_ context.Context) ([]*Invite, error) {
	return nil, nil
}
func (r *inMemoryFedRepo) InsertPeer(_ context.Context, p *Peer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peers = append(r.peers, p)
	return nil
}
func (r *inMemoryFedRepo) UpdatePeerPaired(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (r *inMemoryFedRepo) UpdatePeerRevoked(_ context.Context, peerID string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.peers {
		if p.ID == peerID {
			p.Status = PeerRevoked
		}
	}
	return nil
}
func (r *inMemoryFedRepo) UpdatePeerLastSeen(_ context.Context, _ string, _ time.Time, _ int) error {
	return nil
}
func (r *inMemoryFedRepo) GetPeerByID(_ context.Context, id string) (*Peer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.peers {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, nil
}
func (r *inMemoryFedRepo) GetPeerByServerUUID(_ context.Context, uuid string) (*Peer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.peers {
		if p.ServerUUID == uuid {
			return p, nil
		}
	}
	return nil, nil
}
func (r *inMemoryFedRepo) ListPeers(_ context.Context) ([]*Peer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]*Peer, len(r.peers))
	copy(cp, r.peers)
	return cp, nil
}

func (r *inMemoryFedRepo) UpsertLibraryShare(_ context.Context, s *LibraryShare) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.shares {
		if existing.PeerID == s.PeerID && existing.LibraryID == s.LibraryID {
			r.shares[i] = s
			return nil
		}
	}
	r.shares = append(r.shares, s)
	return nil
}
func (r *inMemoryFedRepo) DeleteLibraryShare(_ context.Context, peerID, shareID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.shares[:0]
	for _, s := range r.shares {
		if s.PeerID == peerID && s.ID == shareID {
			continue
		}
		out = append(out, s)
	}
	r.shares = out
	return nil
}
func (r *inMemoryFedRepo) GetLibraryShare(_ context.Context, peerID, libraryID string) (*LibraryShare, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.shares {
		if s.PeerID == peerID && s.LibraryID == libraryID {
			return s, nil
		}
	}
	return nil, nil
}
func (r *inMemoryFedRepo) ListSharesByPeer(_ context.Context, peerID string) ([]*LibraryShare, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*LibraryShare{}
	for _, s := range r.shares {
		if s.PeerID == peerID {
			out = append(out, s)
		}
	}
	return out, nil
}
func (r *inMemoryFedRepo) ListSharedLibrariesForPeer(_ context.Context, peerID string) ([]*SharedLibrary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*SharedLibrary{}
	// JOIN-equivalent: only return libraries this peer has a share for.
	for _, s := range r.shares {
		if s.PeerID != peerID {
			continue
		}
		for _, lib := range r.libs {
			if lib.ID == s.LibraryID {
				cp := *lib
				cp.Scopes = ShareScopes{
					CanBrowse:   s.CanBrowse,
					CanPlay:     s.CanPlay,
					CanDownload: s.CanDownload,
					CanLiveTV:   s.CanLiveTV,
				}
				out = append(out, &cp)
				break
			}
		}
	}
	return out, nil
}
func (r *inMemoryFedRepo) UpsertCachedItems(_ context.Context, peerID, libraryID string, items []*SharedItem, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cache == nil {
		r.cache = make(map[string]cacheEntry)
	}
	cp := make([]*SharedItem, len(items))
	copy(cp, items)
	r.cache[peerID+"|"+libraryID] = cacheEntry{items: cp, cachedAt: at}
	return nil
}
func (r *inMemoryFedRepo) ListCachedItems(_ context.Context, peerID, libraryID string, offset, limit int) ([]*SharedItem, int, time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[peerID+"|"+libraryID]
	if !ok {
		return []*SharedItem{}, 0, time.Time{}, nil
	}
	total := len(entry.items)
	if offset >= total {
		return []*SharedItem{}, total, entry.cachedAt, nil
	}
	end := offset + limit
	if end > total || limit <= 0 {
		end = total
	}
	return entry.items[offset:end], total, entry.cachedAt, nil
}
func (r *inMemoryFedRepo) PurgeCachedItemsForLibrary(_ context.Context, peerID, libraryID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cache, peerID+"|"+libraryID)
	return nil
}
func (r *inMemoryFedRepo) ListSharedItems(_ context.Context, peerID, libraryID string, offset, limit int) ([]*SharedItem, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Defence in depth: confirm share exists + can_browse.
	visible := false
	for _, s := range r.shares {
		if s.PeerID == peerID && s.LibraryID == libraryID && s.CanBrowse {
			visible = true
			break
		}
	}
	if !visible {
		return []*SharedItem{}, 0, nil
	}
	all := r.items[libraryID]
	total := len(all)
	if offset >= total {
		return []*SharedItem{}, total, nil
	}
	end := offset + limit
	if end > total || limit <= 0 {
		end = total
	}
	return all[offset:end], total, nil
}

// setupManagerWithPeer creates a manager pre-paired with one peer
// whose Ed25519 keys are returned so the test can sign tokens.
func setupManagerWithPeer(t *testing.T) (*Manager, *Peer, ed25519.PrivateKey) {
	t.Helper()
	clk := clock.New()
	repo := &inMemoryFedRepo{}

	if _, err := LoadOrCreate(context.Background(), repo, clk, "TestServer"); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(context.Background(), DefaultConfig(), repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	peerID := "peer-A"
	now := clk.Now()
	peer := &Peer{
		ID:         peerID,
		ServerUUID: "remote-server-uuid",
		Name:       "Remote",
		BaseURL:    "https://remote.example",
		PublicKey:  pub,
		Status:     PeerPaired,
		CreatedAt:  now,
		PairedAt:   &now,
	}
	if err := repo.InsertPeer(context.Background(), peer); err != nil {
		t.Fatal(err)
	}
	if err := mgr.refreshPeerCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	return mgr, peer, priv
}

func TestRequirePeerJWT_RejectsMissingHeader(t *testing.T) {
	mgr, _, _ := setupManagerWithPeer(t)
	mw := RequirePeerJWT(mgr)

	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/peer/ping", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if called {
		t.Error("downstream handler should not have been invoked")
	}
	if !strings.Contains(rec.Body.String(), "PEER_AUTH_REQUIRED") {
		t.Errorf("expected PEER_AUTH_REQUIRED in body, got %s", rec.Body.String())
	}
}

func TestRequirePeerJWT_AcceptsValidTokenAndExposesPeer(t *testing.T) {
	mgr, peer, priv := setupManagerWithPeer(t)
	mw := RequirePeerJWT(mgr)

	tok, err := IssuePeerToken(mgr.clock, priv, peer.ServerUUID, mgr.identity.Current().ServerUUID)
	if err != nil {
		t.Fatal(err)
	}

	var contextPeer *Peer
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextPeer = PeerFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/peer/ping", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if contextPeer == nil {
		t.Fatal("expected peer in context")
	}
	if contextPeer.ID != peer.ID {
		t.Errorf("peer ID = %s, want %s", contextPeer.ID, peer.ID)
	}
}

func TestRequirePeerJWT_RejectsRevokedPeer(t *testing.T) {
	mgr, peer, priv := setupManagerWithPeer(t)
	mw := RequirePeerJWT(mgr)

	// Revoke the peer AFTER issuing the manager.
	if err := mgr.RevokePeer(context.Background(), peer.ID); err != nil {
		t.Fatal(err)
	}

	tok, _ := IssuePeerToken(mgr.clock, priv, peer.ServerUUID, mgr.identity.Current().ServerUUID)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler for revoked peer")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/peer/ping", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "PEER_UNKNOWN") &&
		!strings.Contains(rec.Body.String(), "PEER_REVOKED") {
		t.Errorf("expected PEER_REVOKED or PEER_UNKNOWN in body, got %s", rec.Body.String())
	}
}

func TestRequirePeerJWT_FiresRateLimitAfterBurst(t *testing.T) {
	clk := clock.New()
	repo := &inMemoryFedRepo{}
	if _, err := LoadOrCreate(context.Background(), repo, clk, "TestServer"); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.PeerRequestsPerMinute = 60
	cfg.PeerBurst = 2 // tight burst for testability
	mgr, err := NewManager(context.Background(), cfg, repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	now := clk.Now()
	peer := &Peer{
		ID: "peer-A", ServerUUID: "remote-server-uuid", Name: "Remote",
		BaseURL: "https://remote.example", PublicKey: pub, Status: PeerPaired,
		CreatedAt: now, PairedAt: &now,
	}
	if err := repo.InsertPeer(context.Background(), peer); err != nil {
		t.Fatal(err)
	}
	if err := mgr.refreshPeerCache(context.Background()); err != nil {
		t.Fatal(err)
	}

	mw := RequirePeerJWT(mgr)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tok, _ := IssuePeerToken(clk, priv, peer.ServerUUID, mgr.identity.Current().ServerUUID)

	statuses := []int{}
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/peer/ping", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		statuses = append(statuses, rec.Code)
	}

	// First 2 should pass (burst), then 429s.
	if statuses[0] != http.StatusOK || statuses[1] != http.StatusOK {
		t.Errorf("burst should pass, got %v", statuses)
	}
	if statuses[2] != http.StatusTooManyRequests {
		t.Errorf("3rd request should be rate limited, got %d", statuses[2])
	}
}
