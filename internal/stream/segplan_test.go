package stream

import (
	"strings"
	"testing"
)

func pointsEvery(step float64, n int) []KeyPoint {
	pts := make([]KeyPoint, n)
	for i := range pts {
		t := float64(i) * step
		pts[i] = KeyPoint{PTS: t, Lead: t}
	}
	return pts
}

func TestBuildSegmentPlan_GroupsKeyframesToTarget(t *testing.T) {
	// Keyframes cada 4 s con objetivo 6 s: el corte es el primer
	// keyframe a ≥6 s del anterior → segmentos de 8 s (lo que el remux
	// puede cortar de verdad), no la rejilla de 6 s.
	plan := BuildSegmentPlan(&KeyframeIndex{Points: pointsEvery(4, 30), MaxPTS: 119}, 120, 6)
	if plan == nil {
		t.Fatal("nil plan")
	}
	if plan.Count() != 15 {
		t.Fatalf("Count = %d, want 15", plan.Count())
	}
	for i := 0; i < plan.Count(); i++ {
		if got, want := plan.Start(i), float64(i)*8; got != want {
			t.Fatalf("Start(%d) = %v, want %v", i, got, want)
		}
		if plan.Seek(i) != plan.Start(i) {
			t.Fatalf("closed GOP: Seek(%d) = %v, want = Start", i, plan.Seek(i))
		}
	}
	if plan.End(plan.Count()-1) != 120 || !plan.IsLast(plan.Count()-1) {
		t.Fatalf("last segment end = %v", plan.End(plan.Count()-1))
	}
}

func TestBuildSegmentPlan_OpenGOPCutsOnLead(t *testing.T) {
	// GOP abierto: el segmento empieza en el primer fotograma visible de
	// su grupo (Lead), y el salto va al keyframe (Seek).
	idx := &KeyframeIndex{Points: []KeyPoint{
		{PTS: 0, Lead: 0},
		{PTS: 7.0, Lead: 6.9},
		{PTS: 14.0, Lead: 13.9},
	}}
	plan := BuildSegmentPlan(idx, 20, 6)
	if plan.Count() != 3 {
		t.Fatalf("Count = %d", plan.Count())
	}
	if plan.Start(1) != 6.9 || plan.Seek(1) != 7.0 || plan.End(0) != 6.9 {
		t.Fatalf("segment 1: start %v seek %v; seg0 end %v", plan.Start(1), plan.Seek(1), plan.End(0))
	}
}

func TestBuildSegmentPlan_MergesTinyLastSegment(t *testing.T) {
	plan := BuildSegmentPlan(&KeyframeIndex{Points: pointsEvery(6, 3)}, 12.4, 6)
	// Cortes 0, 6, 12 → el último duraría 0,4 s: se funde con el anterior.
	if plan.Count() != 2 || plan.End(1) != 12.4 {
		t.Fatalf("Count = %d, last end = %v", plan.Count(), plan.End(plan.Count()-1))
	}
}

func TestBuildSegmentPlan_IgnoresKeyframesPastDuration(t *testing.T) {
	plan := BuildSegmentPlan(&KeyframeIndex{Points: pointsEvery(6, 10)}, 30, 6)
	if plan.Count() != 5 || plan.End(4) != 30 {
		t.Fatalf("Count = %d, end = %v", plan.Count(), plan.End(plan.Count()-1))
	}
}

func TestBuildSegmentPlan_FallsBackToIndexDuration(t *testing.T) {
	plan := BuildSegmentPlan(&KeyframeIndex{Points: pointsEvery(6, 4), MaxPTS: 23.9}, 0, 6)
	if plan == nil || plan.End(plan.Count()-1) != 23.9 {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestBuildSegmentPlan_Unusable(t *testing.T) {
	cases := map[string]*KeyframeIndex{
		"nil":   nil,
		"empty": {},
		"zero":  {Points: []KeyPoint{{PTS: 5, Lead: 5}}}, // sin duración y MaxPTS 0
	}
	for name, idx := range cases {
		if p := BuildSegmentPlan(idx, 0, 6); p != nil {
			t.Errorf("%s: want nil plan, got %d segments", name, p.Count())
		}
	}
}

func TestSegmentPlan_SegmentAt(t *testing.T) {
	plan := BuildSegmentPlan(&KeyframeIndex{Points: pointsEvery(4, 10)}, 40, 6) // 0,8,16,24,32
	for _, tc := range []struct {
		t    float64
		want int
	}{{0, 0}, {7.99, 0}, {8, 1}, {17, 2}, {39, 4}, {-1, 0}} {
		if got := plan.SegmentAt(tc.t); got != tc.want {
			t.Errorf("SegmentAt(%v) = %d, want %d", tc.t, got, tc.want)
		}
	}
}

func TestSegmentPlan_Manifest(t *testing.T) {
	idx := &KeyframeIndex{Points: []KeyPoint{{0, 0}, {6.2, 6.2}, {16.5, 16.5}}}
	plan := BuildSegmentPlan(idx, 20, 6)
	m := plan.Manifest("/s/segment%05d.ts?audio=1")
	for _, want := range []string{
		"#EXT-X-TARGETDURATION:11\n", // ceil(10.3)
		"#EXT-X-PLAYLIST-TYPE:VOD\n",
		"#EXTINF:6.200000,\n/s/segment00000.ts?audio=1\n",
		"#EXTINF:10.300000,\n/s/segment00001.ts?audio=1\n",
		"#EXTINF:3.500000,\n/s/segment00002.ts?audio=1\n",
		"#EXT-X-ENDLIST\n",
	} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest missing %q:\n%s", want, m)
		}
	}
}
