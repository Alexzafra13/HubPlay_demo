package system

import "testing"

func TestMemStatsCache_ThrottlesWithinTTL(t *testing.T) {
	var c memStatsCache

	a1, s1 := c.get()
	if a1 < 0 || s1 < 0 {
		t.Fatalf("negative mem figures: alloc=%d sys=%d", a1, s1)
	}
	snap1 := c.snap.Load()
	if snap1 == nil {
		t.Fatal("first get() should have stored a snapshot")
	}

	// A second call inside the TTL must reuse the snapshot — no fresh
	// (stop-the-world) ReadMemStats — so the stored pointer is identical.
	a2, s2 := c.get()
	if c.snap.Load() != snap1 {
		t.Fatal("snapshot refreshed within TTL — throttle is broken")
	}
	if a2 != a1 || s2 != s1 {
		t.Fatalf("values changed within TTL: (%d,%d) -> (%d,%d)", a1, s1, a2, s2)
	}
}
