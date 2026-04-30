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
