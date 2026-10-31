package similarity

import (
	"sort"

	"github.com/RishiKendai/veyra/internal/imgfp"
	"github.com/RishiKendai/veyra/internal/vidfp"
)

// Video matching policy. Matching is deliberately conservative: false
// positives are worse than misses.
const (
	FrameDist      = 10   // max pHash distance for two frames to count as the same picture
	MaxShift       = 2    // sample-index offsets tried, to tolerate small trims / start offsets
	MinCompared    = 8    // min frame pairs that were both informative
	MinMatchPct    = 85   // min % of compared pairs that must match
	MinDistinct    = 4    // each video must contain >= this many distinct shots
	DurationWindow = 0.10 // candidates must have durations within this fraction
)

// Eligible reports whether a fingerprint has enough varied content to be
// matched at all. Static or mostly-black videos are never matched
// perceptually: their frames look alike to every other static video.
func Eligible(f *vidfp.FP) bool {
	return f.Distinct >= MinDistinct && popcount(f.Info) >= MinCompared
}

func popcount(x uint32) (n int) {
	for ; x != 0; x &= x - 1 {
		n++
	}
	return
}

// VideoMatch compares two fingerprints over the best sample offset and
// returns the number of mismatching frame pairs on a match.
func VideoMatch(a, b *vidfp.FP) (mismatches int, ok bool) {
	best, bestN, found := 1<<30, 0, false
	for s := -MaxShift; s <= MaxShift; s++ {
		n, m := 0, 0
		for i := range vidfp.K {
			j := i + s
			if j < 0 || j >= vidfp.K || !a.Informative(i) || !b.Informative(j) {
				continue
			}
			n++
			if imgfp.Dist(a.P[i], b.P[j]) <= FrameDist {
				m++
			}
		}
		if n < MinCompared || m*100 < MinMatchPct*n {
			continue
		}
		if mm := n - m; mm < best || (mm == best && n > bestN) {
			best, bestN, found = mm, n, true
		}
	}
	if !found {
		return 0, false
	}
	return best, true
}

// VideoEdges returns verified matches. Candidates are limited to pairs with
// similar duration, found with a sliding window over the duration-sorted list.
func VideoEdges(fps []vidfp.FP, skip []bool) []Edge {
	idx := make([]int, 0, len(fps))
	for i := range fps {
		if (skip == nil || !skip[i]) && Eligible(&fps[i]) {
			idx = append(idx, i)
		}
	}
	sort.Slice(idx, func(x, y int) bool {
		dx, dy := fps[idx[x]].Duration, fps[idx[y]].Duration
		if dx != dy {
			return dx < dy
		}
		return idx[x] < idx[y]
	})
	var edges []Edge
	for p, i := range idx {
		for _, j := range idx[p+1:] {
			if fps[j].Duration > fps[i].Duration*(1+DurationWindow) {
				break
			}
			if mm, ok := VideoMatch(&fps[i], &fps[j]); ok {
				a, b := min(i, j), max(i, j)
				edges = append(edges, Edge{A: a, B: b, Dist: mm})
			}
		}
	}
	sortEdges(edges)
	return edges
}

// NeedsFingerprint reports, for each video, whether tier-1 fingerprinting can
// possibly matter: VideoEdges only compares videos whose durations are within
// DurationWindow, so a video with no such neighbour can never match and does
// not need its frames decoded. It uses the same window and ordering as
// VideoEdges, so unknown durations (0) pair only with each other, exactly as
// there. skip[i] excludes node i (nil excludes nothing).
//
// Sorted by duration, a node has a partner within the window iff one of its
// two sorted neighbours is, so one pass over adjacent pairs is enough.
func NeedsFingerprint(durations []float64, skip []bool) []bool {
	idx := make([]int, 0, len(durations))
	for i := range durations {
		if skip == nil || !skip[i] {
			idx = append(idx, i)
		}
	}
	sort.Slice(idx, func(x, y int) bool {
		if durations[idx[x]] != durations[idx[y]] {
			return durations[idx[x]] < durations[idx[y]]
		}
		return idx[x] < idx[y]
	})
	need := make([]bool, len(durations))
	for p := 0; p+1 < len(idx); p++ {
		i, j := idx[p], idx[p+1]
		if durations[j] <= durations[i]*(1+DurationWindow) {
			need[i], need[j] = true, true
		}
	}
	return need
}

// maxMismatches is the most mismatching frame pairs VideoMatch can accept:
// it needs m*100 >= MinMatchPct*n matches out of n <= K compared pairs.
const maxMismatches = vidfp.K - (MinMatchPct*vidfp.K+99)/100

// ProbeMayMatch is a cheap necessary condition for VideoMatch, evaluated on
// the probe frames alone. If VideoMatch accepts a pair at some shift, it has at
// most maxMismatches mismatching pairs over all frames, hence at most that many
// over the probe frames (a subset of the compared pairs) at the same shift. So
// a pair that fails here can never match and its remaining frames need not be
// decoded; a pair that passes still needs the full comparison. Pairs whose
// probe frames are uninformative have too few comparable pairs to fail, so
// they are kept.
func ProbeMayMatch(a, b *vidfp.FP) bool {
	for s := -MaxShift; s <= MaxShift; s++ {
		mm := 0
		for i := vidfp.ProbeLo; i <= vidfp.ProbeHi; i++ {
			j := i + s
			if j < vidfp.ProbeLo || j > vidfp.ProbeHi || !a.Informative(i) || !b.Informative(j) {
				continue
			}
			if imgfp.Dist(a.P[i], b.P[j]) > FrameDist {
				mm++
			}
		}
		if mm <= maxMismatches {
			return true
		}
	}
	return false
}

// ProbeSurvivors marks the videos that have at least one partner, within the
// duration window, whose probe does not rule the pair out. Only those need
// their full fingerprint. has[i] says whether node i has a probe at all; nodes
// without one are never marked. It walks pairs exactly as VideoEdges does.
func ProbeSurvivors(fps []vidfp.FP, has []bool) []bool {
	idx := make([]int, 0, len(fps))
	for i := range fps {
		if has[i] {
			idx = append(idx, i)
		}
	}
	sort.Slice(idx, func(x, y int) bool {
		dx, dy := fps[idx[x]].Duration, fps[idx[y]].Duration
		if dx != dy {
			return dx < dy
		}
		return idx[x] < idx[y]
	})
	keep := make([]bool, len(fps))
	for p, i := range idx {
		for _, j := range idx[p+1:] {
			if fps[j].Duration > fps[i].Duration*(1+DurationWindow) {
				break
			}
			if keep[i] && keep[j] {
				continue
			}
			if ProbeMayMatch(&fps[i], &fps[j]) {
				keep[i], keep[j] = true, true
			}
		}
	}
	return keep
}
