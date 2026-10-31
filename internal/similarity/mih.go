// Package similarity finds candidate pairs of similar fingerprints and
// verifies them against conservative thresholds.
package similarity

import "math/bits"

// Edge is a verified match between items A and B (A < B). Dist orders edges
// from strongest to weakest; it is not a percentage.
type Edge struct{ A, B, Dist int }

// Pairs calls emit(i, j) with i < j for every pair of 64-bit hashes whose
// Hamming distance is at most maxDist, without comparing all pairs.
//
// It uses multi-index hashing: split each hash into maxDist+1 chunks. If two
// hashes differ in at most maxDist bits, the pigeonhole principle guarantees
// at least one chunk is identical, so only items sharing a chunk value need
// to be compared. A pair is emitted once, from the first chunk it shares.
// Items with skip[i] set are ignored.
func Pairs(h []uint64, skip []bool, maxDist int, emit func(i, j int)) {
	m := min(max(maxDist+1, 1), 64)
	width := make([]uint, m)
	shift := make([]uint, m)
	var off uint
	for c := range m {
		width[c] = uint(64 / m)
		if uint(c) < uint(64%m) {
			width[c]++
		}
		shift[c] = off
		off += width[c]
	}
	chunk := func(v uint64, c int) uint64 {
		return (v >> shift[c]) & (1<<width[c] - 1)
	}
	for c := range m {
		buckets := make(map[uint64][]int32)
		for i, v := range h {
			if skip != nil && skip[i] {
				continue
			}
			k := chunk(v, c)
			buckets[k] = append(buckets[k], int32(i))
		}
		for _, idx := range buckets {
			for a := 0; a < len(idx); a++ {
				for b := a + 1; b < len(idx); b++ {
					i, j := int(idx[a]), int(idx[b])
					x := h[i] ^ h[j]
					if bits.OnesCount64(x) > maxDist {
						continue
					}
					seen := false
					for c2 := range c {
						if chunk(x, c2) == 0 {
							seen = true // already emitted from chunk c2
							break
						}
					}
					if !seen {
						emit(i, j)
					}
				}
			}
		}
	}
}
