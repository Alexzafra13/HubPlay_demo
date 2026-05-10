package library

import (
	"math/rand"
	"testing"
)

// makeNoise builds a fingerprint of `n` frames where every hash is
// pseudo-random. Used to build the "non-intro" portion of an
// episode's first 10 min.
func makeNoise(seed int64, n int) []uint32 {
	r := rand.New(rand.NewSource(seed))
	out := make([]uint32, n)
	for i := range out {
		out[i] = r.Uint32()
	}
	return out
}

// flipBit XORs a single random bit into each hash. Models the
// chromaprint output drift you see between two re-encodings of the
// same source — Hamming distance 1, well under HammingThreshold.
func flipOneBit(in []uint32, seed int64) []uint32 {
	r := rand.New(rand.NewSource(seed))
	out := make([]uint32, len(in))
	for i, h := range in {
		out[i] = h ^ (1 << uint(r.Intn(32)))
	}
	return out
}

func TestFindLongestCommonRun_ExactMatch(t *testing.T) {
	intro := makeNoise(1, 200)
	a := append(append(makeNoise(2, 100), intro...), makeNoise(3, 100)...)
	b := append(append(makeNoise(4, 50), intro...), makeNoise(5, 200)...)

	got := FindLongestCommonRun(a, b)
	if got.Length != 200 {
		t.Fatalf("expected length 200, got %d", got.Length)
	}
	if got.A.Start != 100 || got.A.End != 300 {
		t.Errorf("A range: got %v, want {100, 300}", got.A)
	}
	if got.B.Start != 50 || got.B.End != 250 {
		t.Errorf("B range: got %v, want {50, 250}", got.B)
	}
}

func TestFindLongestCommonRun_TolerantToBitFlip(t *testing.T) {
	// Same intro audio re-encoded — every hash drifts by 1 bit.
	// Should still match at full length.
	intro := makeNoise(7, 150)
	introNoisy := flipOneBit(intro, 8)
	a := append(makeNoise(9, 80), intro...)
	b := append(makeNoise(10, 120), introNoisy...)

	got := FindLongestCommonRun(a, b)
	if got.Length < 140 {
		t.Fatalf("expected ~150-frame match through bit flip, got %d", got.Length)
	}
}

func TestFindLongestCommonRun_RejectsShortRun(t *testing.T) {
	// 50 matched frames < MinSegmentFrames(94) → no result
	intro := makeNoise(11, 50)
	a := append(append(makeNoise(12, 100), intro...), makeNoise(13, 100)...)
	b := append(append(makeNoise(14, 50), intro...), makeNoise(15, 100)...)

	got := FindLongestCommonRun(a, b)
	if got.Length != 0 {
		t.Fatalf("expected no result for sub-threshold run, got length %d", got.Length)
	}
}

func TestFindLongestCommonRun_NoOverlap(t *testing.T) {
	a := makeNoise(20, 500)
	b := makeNoise(21, 500)
	got := FindLongestCommonRun(a, b)
	if got.Length != 0 {
		t.Fatalf("unrelated noise should not match, got length %d", got.Length)
	}
}

func TestFindCommonSegments_FiveEpisodesShareIntro(t *testing.T) {
	intro := makeNoise(100, 200) // ~25 s
	prints := make([][]uint32, 5)
	for i := range prints {
		// Every episode: random pre-roll of varying length, then the
		// shared intro (slightly bit-flipped to model re-encoding),
		// then random body.
		preroll := makeNoise(int64(200+i), 50+i*10)
		body := makeNoise(int64(300+i), 500)
		prints[i] = append(append(preroll, flipOneBit(intro, int64(400+i))...), body...)
	}
	got := FindCommonSegments(prints)
	if len(got) < 4 {
		t.Fatalf("expected ≥4 of 5 episodes to detect intro, got %d", len(got))
	}
	for _, m := range got {
		runLen := m.Range.End - m.Range.Start
		if runLen < 180 {
			t.Errorf("ep %d run %d frames < expected ~200", m.EpisodeIndex, runLen)
		}
		if m.Confidence < 0.5 {
			t.Errorf("ep %d confidence %.2f below 0.5", m.EpisodeIndex, m.Confidence)
		}
	}
}

func TestFindCommonSegments_OnePremiereWithoutIntro(t *testing.T) {
	intro := makeNoise(500, 200)
	prints := make([][]uint32, 4)
	// ep 0 is the premiere — no intro, only a unique cold open
	prints[0] = append(makeNoise(600, 250), makeNoise(601, 500)...)
	for i := 1; i < 4; i++ {
		prints[i] = append(append(
			makeNoise(int64(700+i), 60),
			flipOneBit(intro, int64(800+i))...,
		), makeNoise(int64(900+i), 500)...)
	}
	got := FindCommonSegments(prints)
	// eps 1..3 should all have a match; ep 0 should not.
	if len(got) != 3 {
		t.Fatalf("expected 3 matches (skip premiere), got %d (matches=%v)", len(got), got)
	}
	for _, m := range got {
		if m.EpisodeIndex == 0 {
			t.Errorf("premiere shouldn't have a match, but EpisodeIndex 0 appeared")
		}
	}
}

func TestFindCommonSegments_NoCommonContent(t *testing.T) {
	prints := [][]uint32{
		makeNoise(1000, 600),
		makeNoise(1001, 600),
		makeNoise(1002, 600),
	}
	got := FindCommonSegments(prints)
	if len(got) != 0 {
		t.Fatalf("expected no matches across unrelated noise, got %d", len(got))
	}
}

func TestFindCommonSegments_SingleEpisodeReturnsNil(t *testing.T) {
	got := FindCommonSegments([][]uint32{makeNoise(1, 500)})
	if got != nil {
		t.Fatalf("single episode should return nil, got %v", got)
	}
}

func TestFramesToSeconds(t *testing.T) {
	// 200 frames @ 7.85 fps = ~25.5 s
	got := FramesToSeconds(200)
	if got < 25.0 || got > 26.0 {
		t.Fatalf("FramesToSeconds(200) = %.2f, want ~25.5", got)
	}
}
