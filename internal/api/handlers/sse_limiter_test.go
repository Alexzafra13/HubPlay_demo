package handlers

import (
	"errors"
	"sync"
	"testing"
)

func TestSSELimiter_AcquireUnderCaps(t *testing.T) {
	l := NewSSELimiter(3, 2)

	releases := make([]func(), 0, 3)
	for i := 0; i < 3; i++ {
		rel, err := l.Acquire("u-1")
		if i < 2 {
			// First two acquisitions for u-1 fit under per-user cap (2).
			if err != nil {
				t.Fatalf("acquire #%d: unexpected err %v", i, err)
			}
			releases = append(releases, rel)
		} else {
			// Third acquisition for u-1 trips the per-user cap before
			// reaching the global cap (3).
			if !errors.Is(err, ErrSSEPerUserCap) {
				t.Fatalf("acquire #%d: want ErrSSEPerUserCap, got %v", i, err)
			}
		}
	}

	// A different user can still acquire — the global cap has 1 slot
	// left (2 used by u-1).
	rel, err := l.Acquire("u-2")
	if err != nil {
		t.Fatalf("u-2 acquire: %v", err)
	}
	releases = append(releases, rel)

	// Now the global cap (3) is exhausted, even for a fresh user.
	if _, err := l.Acquire("u-3"); !errors.Is(err, ErrSSEGlobalCap) {
		t.Fatalf("u-3 acquire after global cap: want ErrSSEGlobalCap, got %v", err)
	}

	for _, r := range releases {
		r()
	}

	// After releasing everything, fresh acquire succeeds again.
	if _, err := l.Acquire("u-3"); err != nil {
		t.Fatalf("post-release acquire: %v", err)
	}
}

func TestSSELimiter_ReleaseIsIdempotent(t *testing.T) {
	l := NewSSELimiter(2, 2)
	rel, err := l.Acquire("u-1")
	if err != nil {
		t.Fatal(err)
	}
	rel()
	rel() // second call must not double-decrement.

	global, perUser := l.Snapshot()
	if global != 0 {
		t.Errorf("global after double-release: got %d want 0", global)
	}
	if len(perUser) != 0 {
		t.Errorf("perUser after double-release: got %v want empty", perUser)
	}
}

func TestSSELimiter_AnonymousCountsGlobalOnly(t *testing.T) {
	l := NewSSELimiter(2, 1)

	rel1, err := l.Acquire("")
	if err != nil {
		t.Fatal(err)
	}
	rel2, err := l.Acquire("")
	if err != nil {
		// Two anonymous acquisitions: per-user cap (1) must NOT trip
		// because the empty userID is excluded from per-user tracking.
		t.Fatalf("second anonymous acquire: %v", err)
	}
	if _, err := l.Acquire(""); !errors.Is(err, ErrSSEGlobalCap) {
		t.Fatalf("third anonymous: want ErrSSEGlobalCap, got %v", err)
	}
	rel1()
	rel2()
}

func TestSSELimiter_PerUserMapCleansUp(t *testing.T) {
	l := NewSSELimiter(10, 5)
	rel, err := l.Acquire("u-clean")
	if err != nil {
		t.Fatal(err)
	}
	rel()
	_, perUser := l.Snapshot()
	if _, ok := perUser["u-clean"]; ok {
		t.Errorf("perUser map kept zero entry for u-clean: %v", perUser)
	}
}

func TestSSELimiter_ConcurrentAcquireAndRelease(t *testing.T) {
	// Race detector smoke test: hammer Acquire/Release with many
	// goroutines and make sure counts converge to zero. Doesn't
	// assert anything about ordering — just that the limiter
	// doesn't corrupt its own state under contention.
	l := NewSSELimiter(50, 5)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			userID := "u-" + string(rune('a'+i%10))
			for j := 0; j < 50; j++ {
				rel, err := l.Acquire(userID)
				if err != nil {
					continue
				}
				rel()
			}
		}(i)
	}
	wg.Wait()

	global, perUser := l.Snapshot()
	if global != 0 {
		t.Errorf("global non-zero after all releases: %d", global)
	}
	if len(perUser) != 0 {
		t.Errorf("perUser non-empty after all releases: %v", perUser)
	}
}

func TestSSELimiter_DefaultsWhenZero(t *testing.T) {
	l := NewSSELimiter(0, 0)
	global, _ := l.Snapshot()
	if global != 0 {
		t.Fatal("fresh limiter must start at 0")
	}
	// We can't observe the constants directly via Snapshot, but we
	// can probe the boundary: the (DefaultSSEPerUserMax + 1)th
	// acquire for the same user must fail with the per-user cap.
	rels := make([]func(), 0, DefaultSSEPerUserMax)
	for i := 0; i < DefaultSSEPerUserMax; i++ {
		rel, err := l.Acquire("u-1")
		if err != nil {
			t.Fatalf("acquire %d under default cap: %v", i, err)
		}
		rels = append(rels, rel)
	}
	if _, err := l.Acquire("u-1"); !errors.Is(err, ErrSSEPerUserCap) {
		t.Fatalf("want ErrSSEPerUserCap at default per-user cap, got %v", err)
	}
	for _, r := range rels {
		r()
	}
}
