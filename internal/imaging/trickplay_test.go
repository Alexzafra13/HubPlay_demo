package imaging

import (
	"math"
	"testing"
)

// TestTrickplayParams_Adapt pins the math that decides how many
// thumbnails to ask ffmpeg for and what grid to pack them into. The
// 2026-05-07 bug was that the legacy generator hardcoded a 10×10
// grid with IntervalSec=10 → only the first 1000 s (≈16 min 40 s)
// of the source were ever covered, and every hover past that landed
// on the last thumbnail. The fix scales interval+grid to the source
// runtime so coverage is total.
func TestTrickplayParams_Adapt(t *testing.T) {
	cases := []struct {
		name           string
		duration       float64
		wantInterval   int
		wantTotal      int
		wantGridAtLeast int
		wantGridAtMost  int
	}{
		{
			name:            "30 minute episode covers full runtime at 10 s",
			duration:        1800,
			wantInterval:    10,
			wantTotal:       180,
			wantGridAtLeast: 14, // ceil(sqrt(180)) = 14
			wantGridAtMost:  14,
		},
		{
			name:            "2 hour movie stays under 200-thumb cap",
			duration:        7200,
			wantInterval:    40, // 7200/200=36 → rounded up to next 5
			wantTotal:       180,
			wantGridAtLeast: 14, // ceil(sqrt(180)) = 14
			wantGridAtMost:  14,
		},
		{
			name:            "4 hour epic scales interval up",
			duration:        14400,
			wantInterval:    75, // 14400/200=72 → rounded up to next 5
			wantTotal:       192,
			wantGridAtLeast: 14, // ceil(sqrt(192)) = 14
			wantGridAtMost:  14,
		},
		{
			name:            "very short clip still gets grid >= 2",
			duration:        20, // 2 thumbs
			wantInterval:    10,
			wantTotal:       2,
			wantGridAtLeast: 2,
			wantGridAtMost:  2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params, total := TrickplayParams{DurationSeconds: tc.duration}.adapt()
			if params.IntervalSec != tc.wantInterval {
				t.Errorf("IntervalSec = %d, want %d", params.IntervalSec, tc.wantInterval)
			}
			if total != tc.wantTotal {
				t.Errorf("total = %d, want %d", total, tc.wantTotal)
			}
			if params.GridSide < tc.wantGridAtLeast || params.GridSide > tc.wantGridAtMost {
				t.Errorf("GridSide = %d, want in [%d, %d]",
					params.GridSide, tc.wantGridAtLeast, tc.wantGridAtMost)
			}
			// Sanity: the chosen grid actually fits the thumbs.
			if params.GridSide*params.GridSide < total {
				t.Errorf("grid %d×%d = %d cells < %d thumbs (would truncate sprite)",
					params.GridSide, params.GridSide, params.GridSide*params.GridSide, total)
			}
		})
	}
}

// TestTrickplayParams_Adapt_NoDuration covers the back-compat path:
// a caller that didn't plumb the runtime gets the legacy 10×10 grid
// with Total=100. This is intentional — it keeps the generator usable
// from places that don't (yet) have an Item handle, while every code
// path that DOES have one is responsible for filling DurationSeconds.
func TestTrickplayParams_Adapt_NoDuration(t *testing.T) {
	params, total := TrickplayParams{}.adapt()
	if params.IntervalSec != 10 || params.GridSide != 10 || total != 100 {
		t.Errorf("zero-duration fallback = (interval=%d, grid=%d, total=%d), want (10, 10, 100)",
			params.IntervalSec, params.GridSide, total)
	}
}

// TestTrickplayParams_Adapt_GridAlwaysFits guarantees the grid never
// truncates the requested thumbnail count. Sweeps a range of
// durations to catch off-by-one regressions in the ceil(sqrt(N))
// math the next time someone "simplifies" it.
func TestTrickplayParams_Adapt_GridAlwaysFits(t *testing.T) {
	for sec := 60; sec <= 18000; sec += 60 {
		params, total := TrickplayParams{DurationSeconds: float64(sec)}.adapt()
		cells := params.GridSide * params.GridSide
		if cells < total {
			t.Errorf("duration=%ds: grid %d×%d=%d cells < %d thumbs",
				sec, params.GridSide, params.GridSide, cells, total)
		}
		// Total should always equal ceil(duration / interval).
		expected := int(math.Ceil(float64(sec) / float64(params.IntervalSec)))
		if total != expected {
			t.Errorf("duration=%ds: total=%d, expected ceil(%d/%d)=%d",
				sec, total, sec, params.IntervalSec, expected)
		}
	}
}

// TestTrickplayManifestVersion_NonZero is a tripwire: the version
// stamp must stay >= 1, otherwise the handler's stale-cache check
// (which treats 0 as missing) would never accept a freshly-generated
// manifest. Bump the constant when the output contract changes;
// this test only checks that someone didn't accidentally zero it.
func TestTrickplayManifestVersion_NonZero(t *testing.T) {
	if TrickplayManifestVersion < 1 {
		t.Errorf("TrickplayManifestVersion = %d, must be >= 1 so cache fast-path can detect stale (0) manifests",
			TrickplayManifestVersion)
	}
}
