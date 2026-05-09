package library

import (
	"math/bits"
)

// Pure matching helpers for the chromaprint fingerprint detector.
// Kept in their own file so they can be exercised in isolation
// without spinning up fpcalc — synthetic fingerprints feed straight
// in. The real fingerprint extractor lives in fingerprint.go and
// the orchestrator in segment_fingerprinter.go.

// chromaprint produces 32-bit hashes at ~7.85 frames/sec
// (sample rate 11025 / step 1408). One frame ≈ 0.1238 s. The exact
// value is platform-stable, so we hard-code it instead of probing.
const FramesPerSecond = 7.85

// HammingThreshold: two chromaprint hashes are considered "the same
// frame" if their popcount XOR is ≤ this. Empirically 4 keeps
// false-positives near zero on intro music while tolerating minor
// re-encoding noise (different bitrates of the same source).
const HammingThreshold = 4

// MinSegmentFrames is the shortest match we'll commit as an intro
// or outro. ~12 s — below that and we're more likely matching a
// recurring sting (production logo, scene transition) than the
// actual intro. 12 s × 7.85 fps ≈ 94 frames.
const MinSegmentFrames = 94

// HashBucketShift drops the noisy low bits when bucketing hashes
// for the seed-and-extend matcher. 12 keeps the top 20 bits as the
// bucket key, which empirically clusters re-encoded variants of
// the same audio while still discriminating between unrelated
// frames. Tunable; lower values waste seed work, higher values
// miss matches across encoders.
const HashBucketShift = 12

// MatchedRange is the [Start, End) slice of frames where one
// episode's audio aligns with another's. Frames are zero-indexed
// into the source fingerprint window (NOT the episode timeline —
// the orchestrator adds the window offset).
type MatchedRange struct {
	Start int
	End   int
}

// pairMatch is the longest matching diagonal between two
// fingerprints, expressed in each one's own frame indices.
type pairMatch struct {
	A      MatchedRange
	B      MatchedRange
	Length int
}

// FindLongestCommonRun finds the longest run of frames where
// `a[startA + k]` Hamming-matches `b[startB + k]` for some k.
// Returns the empty match if no run is at least MinSegmentFrames
// long.
//
// Algorithm: bucket hashes of `a` by (hash >> HashBucketShift),
// then for each frame in `b` look up matching seeds and extend
// forward+backward greedily. O(n * avg_bucket_size) — fast enough
// for the ~5000-frame windows we feed it (10 min of audio).
func FindLongestCommonRun(a, b []uint32) pairMatch {
	if len(a) == 0 || len(b) == 0 {
		return pairMatch{}
	}
	buckets := make(map[uint32][]int, len(a)/4)
	for i, h := range a {
		key := h >> HashBucketShift
		buckets[key] = append(buckets[key], i)
	}
	best := pairMatch{}
	// Tracks (startA, startB) seeds we've already extended so we
	// don't re-walk the same diagonal. Without this, every frame
	// inside a long run gets re-extended from its own seed and the
	// matcher degrades to O(n²).
	visited := make(map[int64]struct{}, len(b))
	for j, hb := range b {
		seeds, ok := buckets[hb>>HashBucketShift]
		if !ok {
			continue
		}
		for _, i := range seeds {
			if hammingPopcount(a[i]^hb) > HammingThreshold {
				continue
			}
			start := int64(j-i)<<32 | int64(j) // offset + start-in-B
			if _, seen := visited[start]; seen {
				continue
			}
			run := extendRun(a, b, i, j)
			// Mark every (offset, b-index) covered by the run so we
			// skip it on subsequent seeds.
			for k := 0; k < run.Length; k++ {
				key := int64((j+k)-(i+k))<<32 | int64(j+k)
				visited[key] = struct{}{}
			}
			if run.Length > best.Length {
				best = run
			}
		}
	}
	if best.Length < MinSegmentFrames {
		return pairMatch{}
	}
	return best
}

// extendRun walks forward then backward from a seed (i in A, j in B)
// while the popcount stays under HammingThreshold. Returns the full
// matched diagonal in both arrays.
func extendRun(a, b []uint32, seedI, seedJ int) pairMatch {
	// extend forward
	endI, endJ := seedI, seedJ
	for endI < len(a) && endJ < len(b) && hammingPopcount(a[endI]^b[endJ]) <= HammingThreshold {
		endI++
		endJ++
	}
	// extend backward
	startI, startJ := seedI, seedJ
	for startI > 0 && startJ > 0 && hammingPopcount(a[startI-1]^b[startJ-1]) <= HammingThreshold {
		startI--
		startJ--
	}
	return pairMatch{
		A:      MatchedRange{Start: startI, End: endI},
		B:      MatchedRange{Start: startJ, End: endJ},
		Length: endI - startI,
	}
}

func hammingPopcount(x uint32) int {
	return bits.OnesCount32(x)
}

// EpisodeMatch is the per-episode result of a series-wide match
// pass. `Range` is in the fingerprint's own frame indices; the
// orchestrator translates to seconds via FramesPerSecond and adds
// the window offset (0 for intro, durationSec - windowSec for outro).
type EpisodeMatch struct {
	EpisodeIndex int          // index into the original prints slice
	Range        MatchedRange // frame range in the episode's fingerprint
	Confidence   float64      // 0..1, share of pairs that agreed
}

// FindCommonSegments uses per-frame density voting to find the
// longest range in each episode that recurs across most other
// episodes in the same series.
//
// Why density instead of pairwise-longest: the longest single match
// between two episodes can be a recurring soundtrack motif, not
// the intro. A frame that matches across many episodes is far more
// likely to be intro content. Density-counting per frame answers
// "how many other episodes contain this frame's audio" directly.
//
// Algorithm, per episode i:
//
//  1. For every other episode j, build a hash bucket once.
//  2. For every frame in episode i, count how many other episodes
//     have a Hamming-≤-HammingThreshold frame anywhere in their
//     fingerprint window. That's the density of frame i.
//  3. Run the longest contiguous range where density ≥ threshold
//     (half of the other episodes, rounded up) and length ≥
//     MinSegmentFrames. That's the segment for episode i.
//
// Confidence = average density inside the run / (n-1). 1.0 means
// every other episode contained every frame of the run.
//
// Cost: O(n × m) bucket lookups where n = episodes and m = frames
// per episode. With 9 episodes × 4824 frames the inner loop runs
// in well under a second.
func FindCommonSegments(prints [][]uint32) []EpisodeMatch {
	n := len(prints)
	if n < 2 {
		return nil
	}
	threshold := (n - 1 + 1) / 2 // ceil(half of OTHER episodes)
	if threshold < 1 {
		threshold = 1
	}

	out := make([]EpisodeMatch, 0, n)
	for i := 0; i < n; i++ {
		if len(prints[i]) == 0 {
			continue
		}
		density := computeDensity(prints, i)
		runStart, runLen, runSum := bestRunByAverageDensity(density, threshold)
		if runLen < MinSegmentFrames {
			continue
		}
		avg := float64(runSum) / float64(runLen)
		out = append(out, EpisodeMatch{
			EpisodeIndex: i,
			Range:        MatchedRange{Start: runStart, End: runStart + runLen},
			Confidence:   avg / float64(n-1),
		})
	}
	return out
}

// computeDensity returns, for each frame in `prints[selfIdx]`, the
// number of OTHER episodes that contain a Hamming-matching frame
// anywhere in their fingerprint window.
//
// We brute-force the inner scan rather than bucketing because
// Hamming distance up to HammingThreshold can spread bit flips
// across the full 32-bit hash — most of those flips land in the
// would-be bucket key, so a hash-bucket lookup would miss the
// majority of Hamming-near matches. The early-exit on first hit
// keeps the actual cost close to O(N × M × E_first) where
// E_first is small for repeated content (intro/outro). For a
// 4824-frame window across 9 episodes the full pass is ~0.3 s.
func computeDensity(prints [][]uint32, selfIdx int) []int {
	target := prints[selfIdx]
	density := make([]int, len(target))
	for fi, h := range target {
		for j, p := range prints {
			if j == selfIdx {
				continue
			}
			for _, h2 := range p {
				if hammingPopcount(h^h2) <= HammingThreshold {
					density[fi]++
					break
				}
			}
		}
	}
	return density
}

// bestRunByAverageDensity scans `density` for maximal contiguous
// runs whose every entry is ≥ threshold AND whose length is ≥
// MinSegmentFrames. Among those, it returns the run with the
// highest average density (ties broken by length, then earliest
// start). Returns (start, len, sum); len is 0 if no qualifying
// run exists.
//
// Why max-avg over longest: a real intro audio matches in (n-1)
// other episodes — every frame's density is near the maximum.
// Coincidental long matches across the soundtrack hover at the
// threshold value. Picking the highest-average run prefers the
// real intro even when a noisier region happens to be longer.
func bestRunByAverageDensity(density []int, threshold int) (int, int, int) {
	bestStart, bestLen, bestSum := 0, 0, 0
	bestAvg := -1.0
	consider := func(s, l, sum int) {
		if l < MinSegmentFrames {
			return
		}
		avg := float64(sum) / float64(l)
		if avg > bestAvg || (avg == bestAvg && l > bestLen) {
			bestStart, bestLen, bestSum, bestAvg = s, l, sum, avg
		}
	}
	runStart, runLen, runSum := 0, 0, 0
	for i, d := range density {
		if d >= threshold {
			if runLen == 0 {
				runStart = i
			}
			runLen++
			runSum += d
		} else {
			consider(runStart, runLen, runSum)
			runLen, runSum = 0, 0
		}
	}
	consider(runStart, runLen, runSum)
	return bestStart, bestLen, bestSum
}

// FramesToSeconds converts a frame index from a chromaprint
// fingerprint into a position in seconds. Pure helper kept here so
// the orchestrator and tests share one source of truth on the
// frame-rate constant.
func FramesToSeconds(frames int) float64 {
	return float64(frames) / FramesPerSecond
}
