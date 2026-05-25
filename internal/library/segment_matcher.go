package library

import (
	"math/bits"
)

// Helpers puros de matching para el detector de fingerprints chromaprint.
// Fichero aislado para testear con fingerprints sintéticos sin fpcalc.

// chromaprint produce hashes de 32 bits a ~7.85 frames/s
// (sample rate 11025 / step 1408). Un frame ≈ 0.1238 s.
const FramesPerSecond = 7.85

// HammingThreshold: dos hashes chromaprint son "el mismo frame" si
// su popcount XOR es <= este valor. 4 mantiene falsos positivos
// cerca de cero tolerando ruido de re-encoding.
const HammingThreshold = 4

// MinSegmentFrames: match mínimo para commitear como intro/outro.
// ~12 s — debajo es más probable un sting recurrente que el intro real.
const MinSegmentFrames = 94

// HashBucketShift descarta bits bajos ruidosos al bucketear para el
// matcher seed-and-extend. 12 mantiene top 20 bits como clave —
// agrupa variantes re-encoded del mismo audio.
const HashBucketShift = 12

// MatchedRange: slice [Start, End) de frames alineados entre dos episodios.
// Frames indexados a cero dentro de la ventana de fingerprint (NO timeline
// del episodio — el orquestador añade el offset de ventana).
type MatchedRange struct {
	Start int
	End   int
}

// pairMatch: diagonal más larga matcheada entre dos fingerprints.
type pairMatch struct {
	A      MatchedRange
	B      MatchedRange
	Length int
}

// FindLongestCommonRun encuentra el run más largo donde a[startA+k]
// Hamming-matchea b[startB+k]. Retorna match vacío si ningún run
// alcanza MinSegmentFrames.
//
// Algoritmo: bucketear hashes de `a` por (hash >> HashBucketShift),
// luego para cada frame de `b` buscar seeds y extender greedy.
// O(n * avg_bucket_size).
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
	// Rastrea diagonales (startA, startB) ya extendidas para evitar
	// re-walk O(n²) cuando cada frame de un run genera su propio seed.
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
			start := int64(j-i)<<32 | int64(j)
			if _, seen := visited[start]; seen {
				continue
			}
			run := extendRun(a, b, i, j)
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

// extendRun extiende forward/backward desde un seed mientras el
// popcount se mantenga bajo HammingThreshold.
func extendRun(a, b []uint32, seedI, seedJ int) pairMatch {
	endI, endJ := seedI, seedJ
	for endI < len(a) && endJ < len(b) && hammingPopcount(a[endI]^b[endJ]) <= HammingThreshold {
		endI++
		endJ++
	}
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

// EpisodeMatch: resultado per-episodio de un match a nivel serie.
// Range en frame indices del fingerprint; el orquestador traduce a
// segundos via FramesPerSecond y añade offset de ventana.
type EpisodeMatch struct {
	EpisodeIndex int
	Range        MatchedRange
	Confidence   float64 // 0..1
}

// FindCommonSegments usa density voting per-frame para encontrar el
// rango más largo en cada episodio que recurre en la mayoría de los
// otros episodios de la misma serie.
//
// Por qué density y no pairwise-longest: el match más largo entre dos
// episodios puede ser un motivo recurrente de soundtrack. Un frame que
// matchea en muchos episodios es mucho más probable que sea intro.
//
// Algoritmo por episodio i:
//  1. Para cada frame de i, contar en cuántos otros episodios hay un
//     frame Hamming-cercano. Eso es la density.
//  2. Run contiguo más largo donde density >= threshold (mitad de
//     otros episodios) y length >= MinSegmentFrames.
//  3. Confidence = density promedio del run / (n-1).
func FindCommonSegments(prints [][]uint32) []EpisodeMatch {
	n := len(prints)
	if n < 2 {
		return nil
	}
	threshold := (n - 1 + 1) / 2
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

// computeDensity: por cada frame de prints[selfIdx], cuenta cuántos
// OTROS episodios contienen un frame Hamming-cercano.
//
// Brute-force en vez de buckets porque Hamming distance dispersa bit
// flips por todo el hash — un bucket lookup perdería la mayoría de
// matches cercanos. Early-exit en primer hit mantiene costo real bajo
// para contenido repetido (~0.3s para 4824 frames × 9 episodios).
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

// bestRunByAverageDensity busca el run contiguo con mayor promedio de
// density donde cada entrada >= threshold y length >= MinSegmentFrames.
//
// Max-avg sobre longest: un intro real matchea en (n-1) episodios con
// density alta; coincidencias largas de soundtrack flotan en el threshold.
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

// FramesToSeconds convierte un índice de frame chromaprint a segundos.
func FramesToSeconds(frames int) float64 {
	return float64(frames) / FramesPerSecond
}
