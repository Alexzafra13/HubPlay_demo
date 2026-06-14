package federation

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"hubplay/internal/clock"
)

// TestSweepStreamSessions_DropsExpired verifies the core invariant of
// stream session bookkeeping: an entry whose LastSeenAt is older than
// peerStreamSessionTTL is reclaimed; one within the window is kept.
//
// Without this guarantee a peer that opens sessions and never returns
// would grow streamSessions unbounded; the JWT expires upstream but
// the in-memory mapping does not.
func TestSweepStreamSessions_DropsExpired(t *testing.T) {
	start := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	clk := &clock.Mock{CurrentTime: start}
	repo := &inMemoryFedRepo{}
	if _, err := LoadOrCreate(context.Background(), repo, clk, "T"); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(context.Background(), DefaultConfig(), repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	stale := mgr.RegisterPeerStreamSession("peer-1", "item-1", "auto")
	clk.Advance(peerStreamSessionTTL + time.Second)
	fresh := mgr.RegisterPeerStreamSession("peer-2", "item-2", "auto")

	mgr.SweepStreamSessions()

	if got := mgr.LookupPeerStreamSession(stale.ID); got != nil {
		t.Fatalf("expected stale session %s to be reclaimed, got %#v", stale.ID, got)
	}
	if got := mgr.LookupPeerStreamSession(fresh.ID); got == nil {
		t.Fatalf("expected fresh session %s to survive sweep", fresh.ID)
	}
}

// TestSweepStreamSessions_LookupBumpsLastSeen confirms the touch
// semantics: a session lookup mid-stream resets its idle window so
// the sweeper does not reclaim a session whose player is still
// actively fetching segments.
func TestSweepStreamSessions_LookupBumpsLastSeen(t *testing.T) {
	start := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	clk := &clock.Mock{CurrentTime: start}
	repo := &inMemoryFedRepo{}
	if _, err := LoadOrCreate(context.Background(), repo, clk, "T"); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(context.Background(), DefaultConfig(), repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	s := mgr.RegisterPeerStreamSession("peer-1", "item-1", "auto")

	// Almost-expired, then a player segment fetch lands → LastSeenAt
	// must move forward and the next sweep must keep the session.
	clk.Advance(peerStreamSessionTTL - time.Second)
	if got := mgr.LookupPeerStreamSession(s.ID); got == nil {
		t.Fatalf("session disappeared before TTL")
	}
	clk.Advance(peerStreamSessionTTL - time.Second)
	mgr.SweepStreamSessions()
	if got := mgr.LookupPeerStreamSession(s.ID); got == nil {
		t.Fatal("session reclaimed despite recent touch")
	}
}

// TestManager_CloseStopsSweeperGoroutine asserts the background
// sweeper goroutine started in NewManager is actually stopped by
// Close. We sample the runtime goroutine count before/after a tight
// create-and-destroy loop; the deltas must not accumulate. This
// catches the regression where Close forgets to cancel the ticker.
func TestManager_CloseStopsSweeperGoroutine(t *testing.T) {
	clk := clock.New()
	repo := &inMemoryFedRepo{}
	if _, err := LoadOrCreate(context.Background(), repo, clk, "T"); err != nil {
		t.Fatal(err)
	}

	// Warm-up: one cycle to allow lazy package init goroutines to settle.
	mgr, err := NewManager(context.Background(), DefaultConfig(), repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mgr.Close()

	baseline := runtime.NumGoroutine()
	for i := 0; i < 25; i++ {
		mgr, err := NewManager(context.Background(), DefaultConfig(), repo, clk, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		mgr.Close()
	}
	// Sleep LEGÍTIMO (F15-1 batch 4): el test compara
	// runtime.NumGoroutine() pre/post un loop de Close. Aunque Close
	// sea síncrono, las goroutines lazy (timer drains, GC roots) tardan
	// unos ms en desaparecer del runtime. Goleak cubre el caso de
	// regresión real vía CI; este sleep solo amortigua el ruido de
	// scheduler en la aserción "delta > 5".
	time.Sleep(50 * time.Millisecond)

	if delta := runtime.NumGoroutine() - baseline; delta > 5 {
		buf := make([]byte, 1<<16)
		n := runtime.Stack(buf, true)
		// Only dump frames mentioning federation to keep the failure
		// readable.
		var trimmed []string
		for _, line := range strings.Split(string(buf[:n]), "\n") {
			if strings.Contains(line, "federation") {
				trimmed = append(trimmed, line)
			}
		}
		t.Fatalf("goroutine leak: +%d after 25 NewManager/Close cycles\n%s",
			delta, strings.Join(trimmed, "\n"))
	}
}

// TestManager_CloseIdempotent guards against a panic if Close is
// called twice (graceful-shutdown handlers occasionally do this
// when a teardown path is shared between SIGTERM and a defer).
func TestManager_CloseIdempotent(t *testing.T) {
	clk := clock.New()
	repo := &inMemoryFedRepo{}
	if _, err := LoadOrCreate(context.Background(), repo, clk, "T"); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(context.Background(), DefaultConfig(), repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mgr.Close()
	mgr.Close() // must not panic, must not deadlock
}

// TestAdmitPeerStream_EnforcesPerPeerCap is the F-2 regression: a peer
// already streaming `cap` distinct items is refused a new one, but
// re-requesting an item it's already streaming (retry/reconnect) is
// allowed, and other peers are unaffected.
func TestAdmitPeerStream_EnforcesPerPeerCap(t *testing.T) {
	repo := &inMemoryFedRepo{}
	clk := clock.New()
	if _, err := LoadOrCreate(context.Background(), repo, clk, "T"); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.MaxConcurrentStreamsPerPeer = 2
	mgr, err := NewManager(context.Background(), cfg, repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	// Peer A fills its cap with two distinct items.
	mgr.RegisterPeerStreamSession("peerA", "item1", "1080p")
	mgr.RegisterPeerStreamSession("peerA", "item2", "1080p")

	// A third DISTINCT item is refused.
	if err := mgr.AdmitPeerStream("peerA", "item3"); !errors.Is(err, ErrPeerStreamLimit) {
		t.Errorf("third distinct item should hit the cap, got: %v", err)
	}
	// Re-requesting an already-active item is always allowed.
	if err := mgr.AdmitPeerStream("peerA", "item1"); err != nil {
		t.Errorf("re-requesting an active item must be admitted, got: %v", err)
	}
	// A different peer has its own budget.
	if err := mgr.AdmitPeerStream("peerB", "item9"); err != nil {
		t.Errorf("a different peer must not be affected, got: %v", err)
	}
}

// TestAdmitPeerStream_UnlimitedWhenZero pins that cap=0 disables the
// ceiling entirely (back-compat for deployments that don't set it).
func TestAdmitPeerStream_UnlimitedWhenZero(t *testing.T) {
	repo := &inMemoryFedRepo{}
	clk := clock.New()
	if _, err := LoadOrCreate(context.Background(), repo, clk, "T"); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.MaxConcurrentStreamsPerPeer = 0
	mgr, err := NewManager(context.Background(), cfg, repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	for i := 0; i < 50; i++ {
		mgr.RegisterPeerStreamSession("peerA", "item"+strconv.Itoa(i), "1080p")
	}
	if err := mgr.AdmitPeerStream("peerA", "another"); err != nil {
		t.Errorf("cap=0 means unlimited, got: %v", err)
	}
}
