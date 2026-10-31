package similarity

import (
	"sort"

	"github.com/RishiKendai/veyra/internal/imgfp"
)

// Image thresholds, in differing bits out of 64. All three hashes must agree:
// pHash finds candidates, dHash and aHash veto look-alike layouts. They were
// chosen from the separation measured in TestImageThresholdsSeparateClasses.
const (
	MaxPHash = 8
	MaxDHash = 14
	MaxAHash = 14
)

// ImageEdges returns verified visual matches between fingerprints.
// skip[i] excludes item i (failed to decode); flat images are always excluded.
func ImageEdges(hs []imgfp.Hashes, skip []bool) []Edge {
	ph := make([]uint64, len(hs))
	ex := make([]bool, len(hs))
	for i, h := range hs {
		ph[i] = h.P
		ex[i] = h.Flat || (skip != nil && skip[i])
	}
	var edges []Edge
	Pairs(ph, ex, MaxPHash, func(i, j int) {
		if d, ok := ImageMatch(hs[i], hs[j]); ok {
			edges = append(edges, Edge{A: i, B: j, Dist: d})
		}
	})
	sortEdges(edges)
	return edges
}

// ImageMatch applies the verification rule to one pair and returns a
// combined distance (sum of the three Hamming distances) for ordering.
func ImageMatch(a, b imgfp.Hashes) (dist int, ok bool) {
	if a.Flat || b.Flat {
		return 0, false
	}
	dp, dd, da := imgfp.Dist(a.P, b.P), imgfp.Dist(a.D, b.D), imgfp.Dist(a.A, b.A)
	return dp + dd + da, dp <= MaxPHash && dd <= MaxDHash && da <= MaxAHash
}

// MergeEdges unions two edge lists, keeping the smaller distance for a pair
// that appears in both, and returns them sorted.
func MergeEdges(a, b []Edge) []Edge {
	type pair struct{ a, b int }
	best := make(map[pair]int, len(a)+len(b))
	for _, list := range [][]Edge{a, b} {
		for _, e := range list {
			k := pair{e.A, e.B}
			if d, ok := best[k]; !ok || e.Dist < d {
				best[k] = e.Dist
			}
		}
	}
	out := make([]Edge, 0, len(best))
	for k, d := range best {
		out = append(out, Edge{A: k.a, B: k.b, Dist: d})
	}
	sortEdges(out)
	return out
}

func sortEdges(e []Edge) {
	sort.Slice(e, func(x, y int) bool {
		if e[x].Dist != e[y].Dist {
			return e[x].Dist < e[y].Dist
		}
		if e[x].A != e[y].A {
			return e[x].A < e[y].A
		}
		return e[x].B < e[y].B
	})
}
