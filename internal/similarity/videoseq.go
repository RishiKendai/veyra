package similarity

import (
	"math"
	"math/bits"
	"slices"
	"sort"

	"github.com/RishiKendai/veyra/internal/vidfp"
)

// Keyframe-sequence matching: finds two cuts of the same footage even when one
// has a different intro, logos, removed scenes or another resolution/aspect.
//
// Every keyframe of A is compared with every keyframe of B. Each pair whose
// pHashes are close votes for the time offset tB-tA. Genuine copies produce
// many votes at one offset (or a few offsets, when scenes were cut out); two
// unrelated videos scatter their rare coincidences over random offsets.
const (
	SeqFrameDist   = FrameDist // max pHash distance for two keyframes to correspond
	SeqBin         = 6.0       // seconds; keyframes of different encodes are not at identical times
	SeqWindowMin   = 4         // distinct frames an offset window needs to count at all
	SeqBestMin     = 10        // the best window needs at least this many distinct frames
	SeqTotalMin    = 20        // distinct matched frames over all accepted windows
	SeqCoverPct    = 15        // ...and at least this % of the shorter video's keyframes
	SeqDurationWin = 0.12      // candidates must have durations within this fraction
	SeqDistinctMin = 20        // each video must contain at least this many different shots
	maxSeqHits     = 400000    // more than this means repetitive content, not a match
)

type hit struct {
	bin  int32
	a, b int32
}

// SeqMatch reports whether a and b are the same footage and returns the
// number of distinct matched keyframes (higher is stronger).
func SeqMatch(a, b *vidfp.Seq) (matched int, ok bool) {
	na, nb := len(a.Frames), len(b.Frames)
	if na < SeqTotalMin || nb < SeqTotalMin || a.Distinct < SeqDistinctMin || b.Distinct < SeqDistinctMin {
		return 0, false
	}
	hits := make([]hit, 0, 4096)
	for i, fa := range a.Frames {
		for j, fb := range b.Frames {
			if bits.OnesCount64(fa.H^fb.H) > SeqFrameDist {
				continue
			}
			bin := int32(math.Floor(float64(fb.T-fa.T) / SeqBin))
			hits = append(hits, hit{bin, int32(i), int32(j)})
			if len(hits) > maxSeqHits {
				return 0, false
			}
		}
	}
	if len(hits) < SeqBestMin {
		return 0, false
	}
	sort.Slice(hits, func(x, y int) bool { return hits[x].bin < hits[y].bin })

	// Score every window of two adjacent bins by its distinct frames on the
	// weaker side, so one static frame matching many others counts once.
	type win struct {
		lo    int32 // window covers bins lo and lo+1
		score int
	}
	var wins []win
	for lo := hits[0].bin - 1; lo <= hits[len(hits)-1].bin; lo++ {
		l := sort.Search(len(hits), func(k int) bool { return hits[k].bin >= lo })
		r := sort.Search(len(hits), func(k int) bool { return hits[k].bin > lo+1 })
		if r-l < SeqWindowMin {
			continue
		}
		if s := distinctMin(a, b, hits[l:r]); s >= SeqWindowMin {
			wins = append(wins, win{lo, s})
		}
	}
	if len(wins) == 0 {
		return 0, false
	}
	slices.SortFunc(wins, func(x, y win) int {
		if x.score != y.score {
			return y.score - x.score
		}
		return int(x.lo - y.lo)
	})
	if wins[0].score < SeqBestMin {
		return 0, false
	}
	// Greedily take the best non-overlapping windows; count distinct frames overall.
	usedA, usedB := map[uint64]struct{}{}, map[uint64]struct{}{}
	var taken []int32
	for _, w := range wins {
		overlap := false
		for _, t := range taken {
			if w.lo-t < 2 && t-w.lo < 2 {
				overlap = true
				break
			}
		}
		if overlap {
			continue
		}
		taken = append(taken, w.lo)
		l := sort.Search(len(hits), func(k int) bool { return hits[k].bin >= w.lo })
		r := sort.Search(len(hits), func(k int) bool { return hits[k].bin > w.lo+1 })
		for _, h := range hits[l:r] {
			usedA[a.Frames[h.a].H], usedB[b.Frames[h.b].H] = struct{}{}, struct{}{}
		}
	}
	matched = min(len(usedA), len(usedB))
	if matched < SeqTotalMin || matched*100 < SeqCoverPct*min(na, nb) {
		return 0, false
	}
	return matched, true
}

// distinctMin counts the different pictures among the hits on the weaker
// side. Frames are compared by hash, so one picture repeated at many times
// (a logo, a title card) counts once.
func distinctMin(a, b *vidfp.Seq, h []hit) int {
	as, bs := make(map[uint64]struct{}, len(h)), make(map[uint64]struct{}, len(h))
	for _, x := range h {
		as[a.Frames[x.a].H], bs[b.Frames[x.b].H] = struct{}{}, struct{}{}
	}
	return min(len(as), len(bs))
}

// SeqPairs returns the pairs worth comparing by keyframe sequence: durations
// within SeqDurationWin, excluding nodes with skip set and any pair for which
// done reports true (already matched by a cheaper tier). Pairs are (a<b).
func SeqPairs(durations []float64, skip []bool, done func(a, b int) bool) [][2]int {
	idx := make([]int, 0, len(durations))
	for i := range durations {
		if (skip == nil || !skip[i]) && durations[i] > 0 {
			idx = append(idx, i)
		}
	}
	sort.Slice(idx, func(x, y int) bool {
		if durations[idx[x]] != durations[idx[y]] {
			return durations[idx[x]] < durations[idx[y]]
		}
		return idx[x] < idx[y]
	})
	var out [][2]int
	for p, i := range idx {
		for _, j := range idx[p+1:] {
			if durations[j] > durations[i]*(1+SeqDurationWin) {
				break
			}
			a, b := min(i, j), max(i, j)
			if done != nil && done(a, b) {
				continue
			}
			out = append(out, [2]int{a, b})
		}
	}
	return out
}
