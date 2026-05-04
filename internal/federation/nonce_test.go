package federation

import (
	"testing"
	"time"

	"hubplay/internal/clock"
)

func TestNonceCache_FirstSeenIsAccepted(t *testing.T) {
	clk := &clock.Mock{CurrentTime: time.Now()}
	c := newNonceCache(clk)
	exp := clk.Now().Add(5 * time.Minute)

	if !c.checkAndStore("nonce-1", exp) {
		t.Fatal("first appearance of a nonce must be accepted")
	}
}

func TestNonceCache_ReplayIsRejected(t *testing.T) {
	clk := &clock.Mock{CurrentTime: time.Now()}
	c := newNonceCache(clk)
	exp := clk.Now().Add(5 * time.Minute)

	if !c.checkAndStore("nonce-1", exp) {
		t.Fatal("first call must accept")
	}
	if c.checkAndStore("nonce-1", exp) {
		t.Fatal("second call with same nonce must be rejected (replay)")
	}
}

func TestNonceCache_DifferentNoncesDoNotInterfere(t *testing.T) {
	clk := &clock.Mock{CurrentTime: time.Now()}
	c := newNonceCache(clk)
	exp := clk.Now().Add(5 * time.Minute)

	for _, n := range []string{"a", "b", "c", "d"} {
		if !c.checkAndStore(n, exp) {
			t.Fatalf("nonce %q should be fresh, got rejected", n)
		}
	}
	if c.size() != 4 {
		t.Fatalf("expected 4 entries, got %d", c.size())
	}
}

func TestNonceCache_EmptyNonceIsRejected(t *testing.T) {
	clk := &clock.Mock{CurrentTime: time.Now()}
	c := newNonceCache(clk)
	exp := clk.Now().Add(5 * time.Minute)

	if c.checkAndStore("", exp) {
		t.Fatal("empty nonce must be rejected as a replay")
	}
}

// TestNonceCache_EvictsOldestOnOverflow exercises the cap path: load
// the cache past nonceCacheMaxEntries with deliberately far-future
// exp values (simulating a hostile peer that mints long-lived tokens
// to pin memory), then assert the size after the next insert is
// bounded by maxEntries - evictBatch + 1, and that the evicted set
// is the soonest-to-expire prefix.
func TestNonceCache_EvictsOldestOnOverflow(t *testing.T) {
	clk := &clock.Mock{CurrentTime: time.Now()}
	c := newNonceCache(clk)

	// Fill with monotonically-increasing exp so we know exactly which
	// nonces should evict on overflow (the lowest-numbered ones).
	for i := 0; i < nonceCacheMaxEntries; i++ {
		exp := clk.Now().Add(time.Duration(i+1) * time.Second)
		if !c.checkAndStore(noncePadded(i), exp) {
			t.Fatalf("seed insert %d rejected", i)
		}
	}
	if c.size() != nonceCacheMaxEntries {
		t.Fatalf("seed: expected %d entries, got %d", nonceCacheMaxEntries, c.size())
	}

	// One more insert tips the cap → evictOldest fires.
	farFuture := clk.Now().Add(24 * time.Hour)
	if !c.checkAndStore("trigger", farFuture) {
		t.Fatal("trigger insert rejected")
	}

	// After eviction we should hold (max - batch) + 1 entries.
	wantSize := nonceCacheMaxEntries - nonceCacheEvictBatch + 1
	if c.size() != wantSize {
		t.Fatalf("post-eviction size: got %d, want %d", c.size(), wantSize)
	}

	// The lowest-numbered seed nonces are the soonest to expire and
	// should be gone; the highest-numbered should still be there.
	c.mu.Lock()
	_, evictedPresent := c.seen[noncePadded(0)]
	_, keptPresent := c.seen[noncePadded(nonceCacheMaxEntries-1)]
	_, triggerPresent := c.seen["trigger"]
	c.mu.Unlock()
	if evictedPresent {
		t.Error("oldest nonce should have been evicted")
	}
	if !keptPresent {
		t.Error("freshest seed nonce should have survived eviction")
	}
	if !triggerPresent {
		t.Error("triggering nonce should be admitted post-eviction")
	}
}

// noncePadded returns a stable, ordered nonce key for the cap test.
// Using a deterministic prefix keeps map iteration order out of the
// way of the assertions.
func noncePadded(i int) string {
	return "n-" + padInt(i, 8)
}

func padInt(i, width int) string {
	s := ""
	for n := i; n > 0 || len(s) == 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	for len(s) < width {
		s = "0" + s
	}
	return s
}

func TestNonceCache_SweepsExpiredEntries(t *testing.T) {
	clk := &clock.Mock{CurrentTime: time.Now()}
	c := newNonceCache(clk)

	earlyExp := clk.Now().Add(1 * time.Minute)
	if !c.checkAndStore("early", earlyExp) {
		t.Fatal("first insert should succeed")
	}

	// Advance past the early expiry. The next insert sweeps before
	// checking; 'early' is gone and re-using it succeeds.
	clk.Advance(2 * time.Minute)
	lateExp := clk.Now().Add(5 * time.Minute)
	if !c.checkAndStore("late", lateExp) {
		t.Fatal("late insert should succeed")
	}
	if !c.checkAndStore("early", lateExp) {
		t.Fatal("after sweep 'early' should be insertable again")
	}
	if c.size() != 2 {
		t.Fatalf("expected 2 entries (late + new early), got %d", c.size())
	}
}
