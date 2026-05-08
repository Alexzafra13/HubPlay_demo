package library

import (
	"sort"
	"testing"
)

func TestAllowedRating(t *testing.T) {
	t.Run("empty cap allows everything", func(t *testing.T) {
		if !AllowedRating("R", "") {
			t.Error("R against empty cap should be allowed")
		}
		if !AllowedRating("", "") {
			t.Error("empty rating against empty cap should be allowed")
		}
	})

	t.Run("PG-13 cap blocks R", func(t *testing.T) {
		if AllowedRating("R", "PG-13") {
			t.Error("R must be blocked when cap is PG-13")
		}
		if AllowedRating("NC-17", "PG-13") {
			t.Error("NC-17 must be blocked when cap is PG-13")
		}
	})

	t.Run("PG-13 cap allows PG and G", func(t *testing.T) {
		if !AllowedRating("PG", "PG-13") {
			t.Error("PG must be allowed when cap is PG-13")
		}
		if !AllowedRating("G", "PG-13") {
			t.Error("G must be allowed when cap is PG-13")
		}
		if !AllowedRating("PG-13", "PG-13") {
			t.Error("PG-13 must be allowed when cap is PG-13")
		}
	})

	t.Run("unrated denied against any cap", func(t *testing.T) {
		if AllowedRating("", "PG-13") {
			t.Error("unrated content must be denied against a non-empty cap")
		}
	})

	t.Run("unknown cap fails open", func(t *testing.T) {
		// A rating literal we don't know (e.g. a future BBFC entry)
		// should not lock the user out of their library — fail-open
		// is the safer default than hard-deny.
		if !AllowedRating("R", "ICAA-18") {
			t.Error("unknown cap should fail-open to allowing the item")
		}
	})

	t.Run("TV ratings honour the table", func(t *testing.T) {
		if AllowedRating("TV-MA", "TV-PG") {
			t.Error("TV-MA must be blocked when cap is TV-PG")
		}
		if !AllowedRating("TV-Y7", "TV-PG") {
			t.Error("TV-Y7 must be allowed when cap is TV-PG")
		}
	})
}

func TestAllowedRatingsAtMost(t *testing.T) {
	t.Run("empty cap returns nil (no filter)", func(t *testing.T) {
		got := AllowedRatingsAtMost("")
		if got != nil {
			t.Errorf("empty cap should return nil, got %v", got)
		}
	})

	t.Run("PG-13 cap covers G + PG + PG-13 plus equivalent TV ratings", func(t *testing.T) {
		got := AllowedRatingsAtMost("PG-13")
		sort.Strings(got)
		// PG-13 has rank 3 → covers all entries with rank ≤ 3.
		// Both rating systems share the table so TV-Y/Y7/G/PG/14 land
		// at ranks ≤ 3 too.
		want := map[string]bool{
			"G": true, "PG": true, "PG-13": true,
			"TV-Y": true, "TV-Y7": true, "TV-G": true, "TV-PG": true, "TV-14": true,
		}
		for _, r := range got {
			if !want[r] {
				t.Errorf("PG-13 allow-list contains %q which exceeds the cap", r)
			}
		}
		// Spot-check denials.
		for _, r := range []string{"R", "NC-17", "TV-MA"} {
			for _, g := range got {
				if g == r {
					t.Errorf("PG-13 allow-list incorrectly contains %q", r)
				}
			}
		}
	})

	t.Run("unknown cap fails open", func(t *testing.T) {
		got := AllowedRatingsAtMost("ICAA-18")
		if got != nil {
			t.Errorf("unknown cap should fail-open to nil (no filter), got %v", got)
		}
	})
}
