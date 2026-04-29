package federation

import (
	"sync"
	"testing"
	"time"
)

func TestRateLimiter_AllowsBurstThenShapes(t *testing.T) {
	clk := &fixedClock{now: time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)}
	rl := NewRateLimiter(clk, 60, 10) // 1 req/sec, burst 10

	// First 10 should pass instantly (burst).
	for i := 0; i < 10; i++ {
		if !rl.Allow("peer-A") {
			t.Fatalf("burst request %d denied", i)
		}
	}
	// 11th should be denied — bucket empty.
	if rl.Allow("peer-A") {
		t.Fatal("11th request should be rate limited")
	}
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {
	clk := &fixedClock{now: time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)}
	rl := NewRateLimiter(clk, 60, 5) // 1 req/sec, burst 5

	// Drain.
	for i := 0; i < 5; i++ {
		if !rl.Allow("peer-A") {
			t.Fatalf("burst %d denied", i)
		}
	}
	if rl.Allow("peer-A") {
		t.Fatal("expected denial right after burst")
	}

	// Advance 3s — should have ~3 tokens.
	clk.now = clk.now.Add(3 * time.Second)
	pass := 0
	for i := 0; i < 5; i++ {
		if rl.Allow("peer-A") {
			pass++
		}
	}
	// Floating-point arithmetic: we should see exactly 3 passes (refill was 3s × 1/s = 3 tokens).
	if pass != 3 {
		t.Errorf("expected 3 refilled tokens, got %d", pass)
	}
}

func TestRateLimiter_BucketsAreIsolatedPerPeer(t *testing.T) {
	clk := &fixedClock{now: time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)}
	rl := NewRateLimiter(clk, 60, 3)

	// peer-A burns its bucket.
	for i := 0; i < 3; i++ {
		rl.Allow("peer-A")
	}
	if rl.Allow("peer-A") {
		t.Fatal("peer-A should be exhausted")
	}
	// peer-B should still have full burst.
	for i := 0; i < 3; i++ {
		if !rl.Allow("peer-B") {
			t.Fatalf("peer-B request %d should pass", i)
		}
	}
}

func TestRateLimiter_ResetClearsBucket(t *testing.T) {
	clk := &fixedClock{now: time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)}
	rl := NewRateLimiter(clk, 60, 3)

	for i := 0; i < 3; i++ {
		rl.Allow("peer-A")
	}
	if rl.Allow("peer-A") {
		t.Fatal("expected denial before reset")
	}
	rl.Reset("peer-A")
	if !rl.Allow("peer-A") {
		t.Fatal("reset should hand back a full bucket")
	}
}

func TestRateLimiter_NilSafe(t *testing.T) {
	var rl *RateLimiter
	if !rl.Allow("anything") {
		t.Error("nil RateLimiter must allow everything (no-op)")
	}
	rl.Reset("anything") // must not panic
}

func TestRateLimiter_ConcurrentAccessDoesNotCorruptBucket(t *testing.T) {
	clk := &fixedClock{now: time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)}
	rl := NewRateLimiter(clk, 60, 100)

	var wg sync.WaitGroup
	allowed := make(chan bool, 200)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- rl.Allow("peer-A")
		}()
	}
	wg.Wait()
	close(allowed)

	count := 0
	for ok := range allowed {
		if ok {
			count++
		}
	}
	// Burst is 100; clock didn't advance so no refill. Should be exactly 100.
	if count != 100 {
		t.Errorf("expected exactly 100 allowed under concurrent load, got %d", count)
	}
}
