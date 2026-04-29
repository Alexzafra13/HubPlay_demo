package federation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"hubplay/internal/clock"
	"hubplay/internal/domain"
)

func TestShareLibrary_HappyPath(t *testing.T) {
	mgr, peer, repo := setupShareTest(t)

	// Share a library with sensible defaults.
	share, err := mgr.ShareLibrary(context.Background(), peer.ID, "library-abc", "admin-uuid", DefaultShareScopes())
	if err != nil {
		t.Fatalf("share: %v", err)
	}
	if share.PeerID != peer.ID {
		t.Errorf("share.PeerID = %s, want %s", share.PeerID, peer.ID)
	}
	if !share.CanBrowse || !share.CanPlay {
		t.Errorf("default scopes should include browse + play, got browse=%v play=%v", share.CanBrowse, share.CanPlay)
	}
	if share.CanDownload || share.CanLiveTV {
		t.Errorf("default scopes should NOT include download/livetv, got dl=%v livetv=%v", share.CanDownload, share.CanLiveTV)
	}
	if len(repo.shares) != 1 {
		t.Errorf("repo should have 1 share, got %d", len(repo.shares))
	}
}

func TestShareLibrary_IsIdempotentAndUpdatesScopes(t *testing.T) {
	mgr, peer, repo := setupShareTest(t)

	ctx := context.Background()
	first, _ := mgr.ShareLibrary(ctx, peer.ID, "library-abc", "admin", DefaultShareScopes())

	// Re-share with download enabled. UPSERT semantics mean the same
	// (peer, library) pair stays as one row, scopes updated in place.
	second, err := mgr.ShareLibrary(ctx, peer.ID, "library-abc", "admin", ShareScopes{
		CanBrowse:   true,
		CanPlay:     true,
		CanDownload: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !second.CanDownload {
		t.Error("re-share should have updated scopes")
	}
	if len(repo.shares) != 1 {
		t.Errorf("re-share must NOT create a duplicate row, got %d", len(repo.shares))
	}
	// IDs should match because UPSERT preserves the existing row's ID
	// in our in-memory fake (and the SQL ON CONFLICT preserves it too).
	if first.LibraryID != second.LibraryID {
		t.Errorf("library_id changed across re-share")
	}
}

func TestShareLibrary_RejectsRevokedPeer(t *testing.T) {
	mgr, peer, _ := setupShareTest(t)

	if err := mgr.RevokePeer(context.Background(), peer.ID); err != nil {
		t.Fatal(err)
	}

	_, err := mgr.ShareLibrary(context.Background(), peer.ID, "library-abc", "admin", DefaultShareScopes())
	if err == nil {
		t.Fatal("expected error sharing with revoked peer")
	}
	if !errors.Is(err, domain.ErrPeerUnauthorized) {
		t.Errorf("err = %v, want ErrPeerUnauthorized", err)
	}
}

func TestUnshareLibrary_RemovesRow(t *testing.T) {
	mgr, peer, repo := setupShareTest(t)

	share, _ := mgr.ShareLibrary(context.Background(), peer.ID, "library-abc", "admin", DefaultShareScopes())
	if err := mgr.UnshareLibrary(context.Background(), peer.ID, share.ID); err != nil {
		t.Fatal(err)
	}
	if len(repo.shares) != 0 {
		t.Errorf("share row should be deleted, got %d remaining", len(repo.shares))
	}
}

func TestUnshareLibrary_IsIdempotent(t *testing.T) {
	mgr, peer, _ := setupShareTest(t)
	// Unsharing a non-existent share is a no-op (desired state already true).
	if err := mgr.UnshareLibrary(context.Background(), peer.ID, "nonexistent-share"); err != nil {
		t.Errorf("unshare of nonexistent share should be no-op, got %v", err)
	}
}

func TestListSharedItems_HiddenWithoutShare(t *testing.T) {
	mgr, peer, repo := setupShareTest(t)

	// Pre-populate the repo with libraries + items (the fake in-memory
	// repo gives us total control here).
	repo.libs = []*SharedLibrary{
		{ID: "library-abc", Name: "Movies", ContentType: "movies"},
		{ID: "library-def", Name: "Series", ContentType: "shows"},
	}
	repo.items = map[string][]*SharedItem{
		"library-abc": {
			{ID: "item-1", Type: "movie", Title: "Inception", Year: 2010},
			{ID: "item-2", Type: "movie", Title: "Tenet", Year: 2020},
		},
		"library-def": {
			{ID: "item-3", Type: "series", Title: "Severance", Year: 2022},
		},
	}

	// Share only `library-abc` with peer.
	if _, err := mgr.ShareLibrary(context.Background(), peer.ID, "library-abc", "admin", DefaultShareScopes()); err != nil {
		t.Fatal(err)
	}

	// Items in shared library — visible.
	items, total, err := mgr.ListSharedItems(context.Background(), peer.ID, "library-abc", 0, 10)
	if err != nil {
		t.Fatalf("list shared items: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Errorf("expected 2 items in shared library, got total=%d len=%d", total, len(items))
	}

	// Items in NON-shared library — 404 (peer not found, deliberately
	// conflated with library-not-shared so attackers can't enumerate).
	_, _, err = mgr.ListSharedItems(context.Background(), peer.ID, "library-def", 0, 10)
	if err == nil {
		t.Fatal("expected ErrPeerNotFound for unshared library")
	}
	if !errors.Is(err, domain.ErrPeerNotFound) {
		t.Errorf("err = %v, want ErrPeerNotFound", err)
	}
}

// setupShareTest spins a manager paired with one peer; returns the
// manager + the peer for share-target convenience + the underlying
// repo so the caller can inspect persisted state.
func setupShareTest(t *testing.T) (*Manager, *Peer, *inMemoryFedRepo) {
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

	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	now := clk.Now()
	peer := &Peer{
		ID: "peer-A", ServerUUID: "remote-uuid", Name: "Remote",
		BaseURL: "https://remote.example", PublicKey: pub, Status: PeerPaired,
		CreatedAt: now, PairedAt: &now,
	}
	if err := repo.InsertPeer(context.Background(), peer); err != nil {
		t.Fatal(err)
	}
	if err := mgr.refreshPeerCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	return mgr, peer, repo
}

// _ marker to keep time import used if helpers above evolve.
var _ = time.Second
