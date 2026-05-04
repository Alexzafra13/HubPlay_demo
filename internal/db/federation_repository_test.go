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
	if hits[0].LibraryID != "lib-shared" {
		t.Errorf("expected library_id=lib-shared on hit, got %q", hits[0].LibraryID)
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

// TestFederationRepository_Progress exercises Upsert/Get/Delete +
// the cross-peer Continue Watching join. Also pins the duration
// preservation contract: a second upsert with duration_ticks=0 must
// keep the previously-stored non-zero value.
func TestFederationRepository_Progress(t *testing.T) {
	database := testutil.NewTestDB(t)
	ctx := context.Background()
	fedRepo := db.NewFederationRepository(database)

	insertTestUser(t, database, "u-1")

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	peer := &federation.Peer{
		ID: "peer-A", ServerUUID: "uuid-A", Name: "A",
		BaseURL: "https://a.test", PublicKey: pub,
		Status: federation.PeerPaired, CreatedAt: now, PairedAt: &now,
	}
	if err := fedRepo.InsertPeer(ctx, peer); err != nil {
		t.Fatal(err)
	}
	// Seed a cache row so the Continue Watching JOIN matches.
	if err := fedRepo.UpsertCachedItems(ctx, "peer-A", "lib-1", []*federation.SharedItem{
		{ID: "remote-1", Type: "movie", Title: "Inception", Year: 2010, HasPoster: true},
		{ID: "remote-2", Type: "movie", Title: "Tenet", Year: 2020, HasPoster: false},
	}, now); err != nil {
		t.Fatal(err)
	}

	// First upsert: position only, duration unknown.
	if err := fedRepo.UpsertProgress(ctx, &federation.Progress{
		UserID:        "u-1",
		PeerID:        "peer-A",
		RemoteItemID:  "remote-1",
		PositionTicks: 1_000,
		DurationTicks: 0,
		LastPlayedAt:  now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := fedRepo.GetProgress(ctx, "u-1", "peer-A", "remote-1")
	if err != nil || got == nil {
		t.Fatalf("get progress: got=%v err=%v", got, err)
	}
	if got.PositionTicks != 1_000 || got.DurationTicks != 0 {
		t.Fatalf("first upsert: pos=%d dur=%d", got.PositionTicks, got.DurationTicks)
	}

	// Second upsert with duration. Should pin it.
	if err := fedRepo.UpsertProgress(ctx, &federation.Progress{
		UserID:        "u-1",
		PeerID:        "peer-A",
		RemoteItemID:  "remote-1",
		PositionTicks: 2_000,
		DurationTicks: 10_000,
		LastPlayedAt:  now.Add(time.Second),
		UpdatedAt:     now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	// Third upsert with duration=0. Must preserve 10_000.
	if err := fedRepo.UpsertProgress(ctx, &federation.Progress{
		UserID:        "u-1",
		PeerID:        "peer-A",
		RemoteItemID:  "remote-1",
		PositionTicks: 3_000,
		DurationTicks: 0,
		LastPlayedAt:  now.Add(2 * time.Second),
		UpdatedAt:     now.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = fedRepo.GetProgress(ctx, "u-1", "peer-A", "remote-1")
	if got.PositionTicks != 3_000 || got.DurationTicks != 10_000 {
		t.Fatalf("duration preservation broken: pos=%d dur=%d", got.PositionTicks, got.DurationTicks)
	}

	// Continue Watching: one in-progress row.
	rail, err := fedRepo.ListContinueWatching(ctx, "u-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rail) != 1 {
		t.Fatalf("rail expected 1 row, got %d", len(rail))
	}
	if rail[0].Title != "Inception" || rail[0].PeerName != "A" || !rail[0].HasPoster {
		t.Fatalf("rail metadata wrong: %+v", rail[0])
	}

	// Mark completed -- must drop from rail.
	if err := fedRepo.UpsertProgress(ctx, &federation.Progress{
		UserID:        "u-1",
		PeerID:        "peer-A",
		RemoteItemID:  "remote-1",
		PositionTicks: 9_500,
		DurationTicks: 10_000,
		Completed:     true,
		LastPlayedAt:  now.Add(3 * time.Second),
		UpdatedAt:     now.Add(3 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	rail, _ = fedRepo.ListContinueWatching(ctx, "u-1", 10)
	if len(rail) != 0 {
		t.Fatalf("completed row should be off the rail, got %d", len(rail))
	}

	// Near-complete (>=90%) without explicit completed flag also drops.
	if err := fedRepo.UpsertProgress(ctx, &federation.Progress{
		UserID:        "u-1",
		PeerID:        "peer-A",
		RemoteItemID:  "remote-2",
		PositionTicks: 9_500, // 95%
		DurationTicks: 10_000,
		LastPlayedAt:  now.Add(4 * time.Second),
		UpdatedAt:     now.Add(4 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	rail, _ = fedRepo.ListContinueWatching(ctx, "u-1", 10)
	if len(rail) != 0 {
		t.Fatalf("near-complete row should be off the rail, got %d", len(rail))
	}

	// Delete clears the row entirely.
	if err := fedRepo.DeleteProgress(ctx, "u-1", "peer-A", "remote-1"); err != nil {
		t.Fatal(err)
	}
	got, _ = fedRepo.GetProgress(ctx, "u-1", "peer-A", "remote-1")
	if got != nil {
		t.Fatalf("delete failed, still got: %+v", got)
	}
}

// TestFederationRepository_Progress_PeerRevokedDropsFromRail asserts
// that revoking a peer removes its rows from the Continue Watching
// rail without an explicit purge -- the JOIN gates on status='paired'.
func TestFederationRepository_Progress_PeerRevokedDropsFromRail(t *testing.T) {
	database := testutil.NewTestDB(t)
	ctx := context.Background()
	fedRepo := db.NewFederationRepository(database)

	insertTestUser(t, database, "u-1")

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	peer := &federation.Peer{
		ID: "peer-A", ServerUUID: "uuid-A", Name: "A",
		BaseURL: "https://a.test", PublicKey: pub,
		Status: federation.PeerPaired, CreatedAt: now, PairedAt: &now,
	}
	if err := fedRepo.InsertPeer(ctx, peer); err != nil {
		t.Fatal(err)
	}
	if err := fedRepo.UpsertCachedItems(ctx, "peer-A", "lib-1", []*federation.SharedItem{
		{ID: "remote-1", Type: "movie", Title: "Inception"},
	}, now); err != nil {
		t.Fatal(err)
	}
	if err := fedRepo.UpsertProgress(ctx, &federation.Progress{
		UserID: "u-1", PeerID: "peer-A", RemoteItemID: "remote-1",
		PositionTicks: 1_000, DurationTicks: 10_000,
		LastPlayedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if rows, _ := fedRepo.ListContinueWatching(ctx, "u-1", 10); len(rows) != 1 {
		t.Fatalf("paired rail should have 1 row, got %d", len(rows))
	}
	if err := fedRepo.UpdatePeerRevoked(ctx, "peer-A", now); err != nil {
		t.Fatal(err)
	}
	if rows, _ := fedRepo.ListContinueWatching(ctx, "u-1", 10); len(rows) != 0 {
		t.Fatalf("revoked peer should be off the rail, got %d", len(rows))
	}
}
