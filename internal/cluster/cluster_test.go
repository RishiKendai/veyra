package cluster

import (
	"reflect"
	"testing"

	"github.com/RishiKendai/veyra/internal/similarity"
)

func e(a, b, d int) similarity.Edge { return similarity.Edge{A: a, B: b, Dist: d} }

func TestCompleteLinkage(t *testing.T) {
	tests := []struct {
		name  string
		n     int
		edges []similarity.Edge
		want  [][]int
	}{
		{"no edges", 3, nil, nil},
		{"pair", 3, []similarity.Edge{e(0, 2, 1)}, [][]int{{0, 2}}},
		{"triangle becomes one group", 3, []similarity.Edge{e(0, 1, 1), e(1, 2, 1), e(0, 2, 2)}, [][]int{{0, 1, 2}}},
		// A~B and B~C but A!~C: connected components would say {A,B,C}.
		{"chain is not transitive", 3, []similarity.Edge{e(0, 1, 1), e(1, 2, 5)}, [][]int{{0, 1}}},
		{"chain prefers stronger edge", 3, []similarity.Edge{e(0, 1, 5), e(1, 2, 1)}, [][]int{{1, 2}}},
		{"two separate groups", 6, []similarity.Edge{e(0, 1, 1), e(3, 4, 1)}, [][]int{{0, 1}, {3, 4}}},
		{"merge requires every cross pair", 4,
			[]similarity.Edge{e(0, 1, 1), e(2, 3, 1), e(1, 2, 3), e(0, 3, 3)}, // 0-2 and 1-3 missing
			[][]int{{0, 1}, {2, 3}}},
		{"two cliques merge when fully connected", 4,
			[]similarity.Edge{e(0, 1, 1), e(2, 3, 1), e(0, 2, 2), e(0, 3, 2), e(1, 2, 2), e(1, 3, 2)},
			[][]int{{0, 1, 2, 3}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompleteLinkage(tt.n, tt.edges)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeterministicRegardlessOfEdgeOrder(t *testing.T) {
	edges := []similarity.Edge{e(0, 1, 2), e(1, 2, 2), e(0, 2, 9), e(2, 3, 2), e(1, 3, 2)}
	want := CompleteLinkage(4, edges)
	for range 20 {
		shuffled := append([]similarity.Edge(nil), edges...)
		for i := range shuffled {
			j := (i*7 + 3) % len(shuffled)
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		}
		if got := CompleteLinkage(4, shuffled); !reflect.DeepEqual(got, want) {
			t.Fatalf("order-dependent result: %v vs %v", got, want)
		}
	}
}
