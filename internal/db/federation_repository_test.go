package db_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/federation"
	"hubplay/internal/testutil"
)

// TestFederationRepository_SearchSharedItems exercises the FTS5 search
// path with the real schema + items_fts triggers. Two libraries; one
// shared with peer-A (CanBrowse), one not. A query that matches a
// title in the unshared library must NOT surface — that's the share
// ACL gate.
func TestFederationRepository_SearchSharedItems(t *testing.T) {
	database := testutil.NewTestDB(t)
	ctx := context.Background()

	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	fedRepo := db.NewFederationRepository(database)

	// ── Schema seed ──────────────────────────────────────────────
	now := time.Now().UTC()
	for _, l := range []db.Library{
		{ID: "lib-shared", Name: "Shared", ContentType: "movies", ScanMode: "auto", ScanInterval: "6h", CreatedAt: now, UpdatedAt: now, Paths: []string{"/m1"}},
		{ID: "lib-private", Name: "Private", ContentType: "movies", ScanMode: "auto", ScanInterval: "6h", CreatedAt: now, UpdatedAt: now, Paths: []string{"/m2"}},
	} {
		l := l
		if err := libRepo.Create(ctx, &l); err != nil {
			t.Fatal(err)
		}
	}
	for _, it := range []db.Item{
		{ID: "i-shared-match", LibraryID: "lib-shared", Type: "movie", Title: "Federation Forever", SortTitle: "Federation Forever", Path: "/m1/a.mkv", AddedAt: now, UpdatedAt: now, IsAvailable: true},
		{ID: "i-shared-miss", LibraryID: "lib-shared", Type: "movie", Title: "Quantum Drift", SortTitle: "Quantum Drift", Path: "/m1/b.mkv", AddedAt: now, UpdatedAt: now, IsAvailable: true},
		{ID: "i-private-match", LibraryID: "lib-private", Type: "movie", Title: "Federation Underground", SortTitle: "Federation Underground", Path: "/m2/c.mkv", AddedAt: now, UpdatedAt: now, IsAvailable: true},
	} {
		it := it
		if err := itemRepo.Create(ctx, &it); err != nil {
			t.Fatal(err)
		}
	}

	// ── User + paired peer + share (only lib-shared) ─────────────
	insertTestUser(t, database, "u-admin")

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	peer := &federation.Peer{
		ID:         "peer-A",
		ServerUUID: "uuid-A",
		Name:       "A",
		BaseURL:    "https://a.test",
		PublicKey:  pub,
		Status:     federation.PeerPaired,
		CreatedAt:  now,
		PairedAt:   &now,
	}
	if err := fedRepo.InsertPeer(ctx, peer); err != nil {
		t.Fatal(err)
	}
	share := &federation.LibraryShare{
		ID:              "share-1",
		PeerID:          "peer-A",
		LibraryID:       "lib-shared",
		CanBrowse:       true,
		CanPlay:         true,
		CreatedByUserID: "u-admin",
		CreatedAt:       now,
	}
	if err := fedRepo.UpsertLibraryShare(ctx, share); err != nil {
		t.Fatal(err)
	}

	// ── The actual search — query matches one shared item and
	//    one unshared item; only the shared one must surface.
	hits, err := fedRepo.SearchSharedItems(ctx, "peer-A", "Federation", 10)
	if err != nil {
		t.Fatalf("SearchSharedItems: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit (shared library only), got %d: %#v", len(hits), hits)
	}
	if hits[0].ID != "i-shared-match" {
		t.Errorf("expected i-shared-match, got %s", hits[0].ID)
	}

	// ── Empty query short-circuits without touching FTS. ─────────
	empty, err := fedRepo.SearchSharedItems(ctx, "peer-A", "", 10)
	if err != nil {
		t.Fatalf("SearchSharedItems empty: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty query must return zero hits, got %d", len(empty))
	}

	// ── A query matching nothing returns empty, not error. ──────
	none, err := fedRepo.SearchSharedItems(ctx, "peer-A", "ZZZZNoSuchTitle", 10)
	if err != nil {
		t.Fatalf("SearchSharedItems no match: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("expected zero hits, got %d", len(none))
	}

	// ── A peer with no share at all sees nothing. ───────────────
	otherPeer := &federation.Peer{
		ID:         "peer-Z",
		ServerUUID: "uuid-Z",
		Name:       "Z",
		BaseURL:    "https://z.test",
		PublicKey:  pub,
		Status:     federation.PeerPaired,
		CreatedAt:  now,
		PairedAt:   &now,
	}
	if err := fedRepo.InsertPeer(ctx, otherPeer); err != nil {
		t.Fatal(err)
	}
	hitsZ, err := fedRepo.SearchSharedItems(ctx, "peer-Z", "Federation", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hitsZ) != 0 {
		t.Fatalf("peer with no share must see 0 hits, got %d", len(hitsZ))
	}
}

// insertTestUser writes a minimal users row so federation_library_shares.
// created_by FK is satisfiable. The auth tests have richer fixtures;
// federation just needs the ID present.
func insertTestUser(t *testing.T, database *sql.DB, id string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := database.ExecContext(context.Background(), `
		INSERT INTO users (id, username, display_name, password_hash, role, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, id, id, "x", "admin", now); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
}
