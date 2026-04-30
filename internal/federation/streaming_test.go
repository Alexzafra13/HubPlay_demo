package federation

import (
	"testing"
	"time"

	"hubplay/internal/clock"
)

// TestPeerStreamGate_DedupesSameKey pins that re-opening with the
// same (peer, user, item, profile) returns the SAME session — a
// follow-up segment request from the same client doesn't accidentally
// allocate a fresh session-id and burn a slot.
func TestPeerStreamGate_DedupesSameKey(t *testing.T) {
	g := newPeerStreamGate(3)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	s1, ok := g.open("peer-A", "remote-1", "item-x", "1080p", now)
	if !ok || s1 == nil {
		t.Fatal("first open should succeed")
	}
	s2, ok := g.open("peer-A", "remote-1", "item-x", "1080p", now)
	if !ok || s2 == nil {
		t.Fatal("second open with same key should succeed")
	}
	if s1.SessionID != s2.SessionID {
		t.Errorf("re-open returned different session IDs: %s vs %s", s1.SessionID, s2.SessionID)
	}
}

// TestPeerStreamGate_EnforcesPerPeerCap pins the security guarantee
// — peer A streaming N concurrent items can't take a slot peer B
// would otherwise have, AND once peer A hits the cap the next open
// fails (returns ok=false).
func TestPeerStreamGate_EnforcesPerPeerCap(t *testing.T) {
	g := newPeerStreamGate(2) // small cap for test
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	// Peer A opens 2 — both succeed.
	if _, ok := g.open("peer-A", "u1", "item-1", "1080p", now); !ok {
		t.Fatal("peer-A first open: should succeed")
	}
	if _, ok := g.open("peer-A", "u2", "item-2", "1080p", now); !ok {
		t.Fatal("peer-A second open: should succeed")
	}
	// Peer A opens a third — should fail.
	if _, ok := g.open("peer-A", "u3", "item-3", "1080p", now); ok {
		t.Fatal("peer-A third open: should be rejected (cap exceeded)")
	}
	// Peer B is unaffected — independent counter.
	if _, ok := g.open("peer-B", "u1", "item-1", "1080p", now); !ok {
		t.Fatal("peer-B first open: should succeed (different peer)")
	}
}

// TestPeerStreamGate_CloseReleasesSlot pins that close returns a slot
// to the per-peer counter. Without this the cap would be a one-way
// ratchet — every close-and-reopen would consume a new slot until
// the sweep ran.
func TestPeerStreamGate_CloseReleasesSlot(t *testing.T) {
	g := newPeerStreamGate(1)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	s, ok := g.open("peer-A", "u1", "item-1", "1080p", now)
	if !ok {
		t.Fatal("first open should succeed")
	}
	// Cap full now.
	if _, ok := g.open("peer-A", "u2", "item-2", "1080p", now); ok {
		t.Fatal("second open should be at-cap")
	}
	g.close(s.SessionID)
	// Slot freed — next open should succeed.
	if _, ok := g.open("peer-A", "u2", "item-2", "1080p", now); !ok {
		t.Fatal("after close, next open should succeed")
	}
}

// TestPeerStreamGate_SweepDropsOldSessions pins the safety-net path —
// a session that's been alive past maxAge gets dropped + counter
// released, even without an explicit close.
func TestPeerStreamGate_SweepDropsOldSessions(t *testing.T) {
	g := newPeerStreamGate(1)
	t0 := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	g.open("peer-A", "u1", "item-1", "1080p", t0)
	// Cap full.
	if _, ok := g.open("peer-A", "u2", "item-2", "1080p", t0); ok {
		t.Fatal("expected at-cap")
	}
	// Sweep at t0+5h (older than peerStreamMaxAge=4h) should drop.
	dropped := g.sweepIdle(t0.Add(5*time.Hour), 4*time.Hour)
	if len(dropped) != 1 {
		t.Fatalf("expected 1 dropped session, got %d", len(dropped))
	}
	if _, ok := g.open("peer-A", "u3", "item-3", "1080p", t0.Add(5*time.Hour)); !ok {
		t.Fatal("after sweep, slot should be free")
	}
}

// TestManager_PeerStreamCount exposes the count via the manager API
// — keeps the read path tested end-to-end (constructor wiring +
// pass-through method).
func TestManager_PeerStreamCount(t *testing.T) {
	repo := &inMemoryFedRepo{}
	clk := clock.New()
	if _, err := LoadOrCreate(t.Context(), repo, clk, "TestServer"); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.MaxConcurrentStreamsPerPeer = 5
	mgr, err := NewManager(t.Context(), cfg, repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	if got := mgr.PeerStreamCount("peer-A"); got != 0 {
		t.Errorf("initial count should be 0, got %d", got)
	}
	if _, ok := mgr.OpenPeerStream("peer-A", "u1", "item-1", "1080p"); !ok {
		t.Fatal("open should succeed")
	}
	if got := mgr.PeerStreamCount("peer-A"); got != 1 {
		t.Errorf("after one open, count should be 1, got %d", got)
	}
}
