package similarity

import (
	"math/rand"
	"testing"

	"github.com/RishiKendai/veyra/internal/vidfp"
)

// film makes n keyframes every 5 s with random hashes.
func film(seed int64, n int) vidfp.Seq {
	r := rand.New(rand.NewSource(seed))
	s := vidfp.Seq{Duration: float64(n) * 5}
	for i := range n {
		s.Frames = append(s.Frames, vidfp.SeqFrame{T: float32(i) * 5, H: r.Uint64()})
	}
	s.Distinct = n
	return s
}

// cut returns a copy with an extra intro, small hash noise, jittered times and
// a removed middle section, like a different release of the same film.
func cut(s vidfp.Seq, intro float32, drop [2]int, seed int64) vidfp.Seq {
	r := rand.New(rand.NewSource(seed))
	out := vidfp.Seq{Duration: s.Duration + float64(intro), Distinct: s.Distinct}
	for i, f := range s.Frames {
		if i >= drop[0] && i < drop[1] {
			continue
		}
		h := f.H
		for range 3 {
			h ^= 1 << uint(r.Intn(64))
		}
		out.Frames = append(out.Frames, vidfp.SeqFrame{T: f.T + intro + float32(r.Intn(3)) - 1, H: h})
	}
	return out
}

func TestSeqMatchFindsDifferentCut(t *testing.T) {
	a := film(1, 600)
	b := cut(a, 7, [2]int{200, 260}, 2)
	if _, ok := SeqMatch(&a, &b); !ok {
		t.Fatal("a re-cut of the same film must match")
	}
}

func TestSeqMatchRejectsUnrelated(t *testing.T) {
	a, b := film(1, 600), film(2, 600)
	if n, ok := SeqMatch(&a, &b); ok {
		t.Fatalf("unrelated films matched (%d frames)", n)
	}
}

func TestSeqMatchRejectsScatteredCoincidences(t *testing.T) {
	// Matching frames exist but sit at unrelated offsets: no consistent alignment.
	a := film(1, 600)
	b := film(3, 600)
	r := rand.New(rand.NewSource(9))
	for k := range 60 {
		b.Frames[r.Intn(600)].H = a.Frames[k*10].H
	}
	if _, ok := SeqMatch(&a, &b); ok {
		t.Fatal("scattered coincidences must not match")
	}
}

func TestSeqMatchRepeatedFrameDoesNotCountTwice(t *testing.T) {
	// One picture repeated everywhere (a logo): many hits, one distinct frame.
	mk := func() vidfp.Seq {
		s := vidfp.Seq{Distinct: 1}
		for i := range 600 {
			s.Frames = append(s.Frames, vidfp.SeqFrame{T: float32(i) * 5, H: 0xDEADBEEF})
		}
		return s
	}
	a, b := mk(), mk()
	if _, ok := SeqMatch(&a, &b); ok {
		t.Fatal("a repeated static frame must not match")
	}
}

func TestSeqPairsWindowAndDone(t *testing.T) {
	d := []float64{100, 105, 200, 0}
	got := SeqPairs(d, nil, nil)
	if len(got) != 1 || got[0] != [2]int{0, 1} {
		t.Fatalf("pairs = %v", got)
	}
	if got := SeqPairs(d, nil, func(a, b int) bool { return true }); len(got) != 0 {
		t.Fatalf("done pairs must be skipped: %v", got)
	}
}

func TestSeqMatchNeedsDistinctShots(t *testing.T) {
	a := film(1, 600)
	b := cut(a, 7, [2]int{200, 260}, 2)
	b.Distinct = SeqDistinctMin - 1
	if _, ok := SeqMatch(&a, &b); ok {
		t.Fatal("a video with too few different shots must not match")
	}
}
