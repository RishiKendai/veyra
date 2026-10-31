package similarity

import (
	"math/bits"
	"math/rand/v2"
	"testing"
)

func benchHashes(n int) []uint64 {
	rng := rand.New(rand.NewPCG(7, 8))
	h := make([]uint64, n)
	for i := range h {
		if i > 0 && rng.IntN(20) == 0 { // 5% near-duplicates
			h[i] = h[rng.IntN(i)] ^ 1<<rng.IntN(64) ^ 1<<rng.IntN(64)
		} else {
			h[i] = rng.Uint64()
		}
	}
	return h
}

const benchN = 30_000

func BenchmarkPairsMultiIndex(b *testing.B) {
	h := benchHashes(benchN)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		n := 0
		Pairs(h, nil, MaxPHash, func(i, j int) { n++ })
		if n == 0 {
			b.Fatal("no pairs")
		}
	}
}

func BenchmarkPairsBruteForce(b *testing.B) {
	h := benchHashes(benchN)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		n := 0
		for i := range h {
			for j := i + 1; j < len(h); j++ {
				if bits.OnesCount64(h[i]^h[j]) <= MaxPHash {
					n++
				}
			}
		}
		if n == 0 {
			b.Fatal("no pairs")
		}
	}
}
