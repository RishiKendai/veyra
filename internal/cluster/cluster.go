// Package cluster turns verified pairwise matches into groups.
package cluster

import (
	"sort"

	"github.com/RishiKendai/veyra/internal/similarity"
)

// CompleteLinkage groups n items so that every pair inside a group has a
// verified edge. Plain connected components would chain A~B and B~C into
// {A,B,C} even when A and C do not match; here a merge happens only when all
// cross pairs are matches. Edges are applied strongest first, so the result
// is deterministic. Groups of one are dropped; members and groups are sorted.
func CompleteLinkage(n int, edges []similarity.Edge) [][]int {
	type pair struct{ a, b int }
	adj := make(map[pair]struct{}, len(edges))
	for _, e := range edges {
		adj[pair{min(e.A, e.B), max(e.A, e.B)}] = struct{}{}
	}
	sorted := append([]similarity.Edge(nil), edges...)
	sort.Slice(sorted, func(i, j int) bool {
		x, y := sorted[i], sorted[j]
		if x.Dist != y.Dist {
			return x.Dist < y.Dist
		}
		if x.A != y.A {
			return x.A < y.A
		}
		return x.B < y.B
	})

	group := make([]int, n) // item -> group id
	members := make(map[int][]int, n)
	for i := range n {
		group[i] = i
		members[i] = []int{i}
	}
	for _, e := range sorted {
		ga, gb := group[e.A], group[e.B]
		if ga == gb {
			continue
		}
		ok := true
	check:
		for _, x := range members[ga] {
			for _, y := range members[gb] {
				if _, has := adj[pair{min(x, y), max(x, y)}]; !has {
					ok = false
					break check
				}
			}
		}
		if !ok {
			continue
		}
		if gb < ga {
			ga, gb = gb, ga
		}
		for _, y := range members[gb] {
			group[y] = ga
		}
		members[ga] = append(members[ga], members[gb]...)
		delete(members, gb)
	}

	var out [][]int
	for _, m := range members {
		if len(m) < 2 {
			continue
		}
		sort.Ints(m)
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}
