package similarity

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"math/bits"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/draw"

	"github.com/RishiKendai/veyra/internal/imgfp"
)

type blob struct{ x, y, r, v float64 }

// photo renders a synthetic natural-ish picture: soft blobs of varying
// brightness plus a few hard-edged rectangles, deterministic per seed.
func photo(seed uint64, shift int, rerandom int) *image.RGBA {
	const w, h = 160, 120
	rng := rand.New(rand.NewPCG(seed, seed*7+1))
	blobs := make([]blob, 28)
	for i := range blobs {
		blobs[i] = blob{rng.Float64() * w, rng.Float64() * h, 8 + rng.Float64()*32, rng.Float64()*200 - 100}
	}
	rects := make([][5]float64, 5)
	for i := range rects {
		rects[i] = [5]float64{rng.Float64() * w, rng.Float64() * h, 20 + rng.Float64()*60, 15 + rng.Float64()*50, rng.Float64()*160 - 80}
	}
	rng2 := rand.New(rand.NewPCG(seed+1000, 99))
	for i := range rerandom { // a different shot of the same scene: part of the content changed
		blobs[i] = blob{rng2.Float64() * w, rng2.Float64() * h, 8 + rng2.Float64()*32, rng2.Float64()*200 - 100}
	}
	gx, gy := rng.Float64()*0.4-0.2, rng.Float64()*0.4-0.2
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			fx, fy := float64(x+shift), float64(y)
			v := 128 + gx*fx + gy*fy
			for _, b := range blobs {
				d2 := (fx-b.x)*(fx-b.x) + (fy-b.y)*(fy-b.y)
				v += b.v * math.Exp(-d2/(2*b.r*b.r))
			}
			for _, r := range rects {
				if fx > r[0] && fx < r[0]+r[2] && fy > r[1] && fy < r[1]+r[3] {
					v += r[4]
				}
			}
			c := uint8(max(0, min(255, v)))
			img.Set(x, y, color.RGBA{c, uint8(max(0, min(255, v*0.9+10))), uint8(max(0, min(255, v*1.1-10))), 255})
		}
	}
	return img
}

func saveJPEG(t *testing.T, dir, name string, img image.Image, q int) string {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	os.WriteFile(p, b.Bytes(), 0o600)
	return p
}

func savePNG(t *testing.T, dir, name string, img image.Image) string {
	var b bytes.Buffer
	png.Encode(&b, img)
	p := filepath.Join(dir, name)
	os.WriteFile(p, b.Bytes(), 0o600)
	return p
}

func resize(src image.Image, f float64) *image.RGBA {
	w, h := int(float64(src.Bounds().Dx())*f), int(float64(src.Bounds().Dy())*f)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

func adjust(src *image.RGBA, gain, bias float64) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	for i := 0; i < len(src.Pix); i += 4 {
		for k := range 3 {
			dst.Pix[i+k] = uint8(max(0, min(255, float64(src.Pix[i+k])*gain+bias)))
		}
		dst.Pix[i+3] = 255
	}
	return dst
}

func hash(t *testing.T, path string) imgfp.Hashes {
	h, err := imgfp.Compute(path)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// TestImageThresholdsSeparateClasses is the calibration check: true copies
// under realistic edits must match, and different pictures - including
// different shots of the same scene - must not. It logs the margins so the
// thresholds can be re-derived when the corpus changes. The corpus is
// synthetic; real-photo validation is a separate manual step.
func TestImageThresholdsSeparateClasses(t *testing.T) {
	dir := t.TempDir()
	const nPhotos = 20
	type variant struct {
		name  string
		build func(base *image.RGBA, id int) string
		must  bool // must match: realistic edit; otherwise only reported
	}
	variants := []variant{
		{"png", func(b *image.RGBA, id int) string { return savePNG(t, dir, fmt.Sprintf("v%d-png.png", id), b) }, true},
		{"jpeg q90", func(b *image.RGBA, id int) string { return saveJPEG(t, dir, fmt.Sprintf("v%d-q90.jpg", id), b, 90) }, true},
		{"jpeg q60", func(b *image.RGBA, id int) string { return saveJPEG(t, dir, fmt.Sprintf("v%d-q60.jpg", id), b, 60) }, true},
		{"jpeg q30", func(b *image.RGBA, id int) string { return saveJPEG(t, dir, fmt.Sprintf("v%d-q30.jpg", id), b, 30) }, true},
		{"resize 50%", func(b *image.RGBA, id int) string {
			return savePNG(t, dir, fmt.Sprintf("v%d-r50.png", id), resize(b, 0.5))
		}, true},
		{"resize 40%", func(b *image.RGBA, id int) string {
			return savePNG(t, dir, fmt.Sprintf("v%d-r40.png", id), resize(b, 0.4))
		}, true},
		{"resize 200%", func(b *image.RGBA, id int) string {
			return savePNG(t, dir, fmt.Sprintf("v%d-r200.png", id), resize(b, 2))
		}, true},
		{"brighter", func(b *image.RGBA, id int) string {
			return savePNG(t, dir, fmt.Sprintf("v%d-br.png", id), adjust(b, 1, 18))
		}, true},
		{"lower contrast", func(b *image.RGBA, id int) string {
			return savePNG(t, dir, fmt.Sprintf("v%d-lc.png", id), adjust(b, 0.85, 12))
		}, true},
		{"resize+q70", func(b *image.RGBA, id int) string {
			return saveJPEG(t, dir, fmt.Sprintf("v%d-r60q70.jpg", id), resize(b, 0.6), 70)
		}, true},
	}

	bases := make([]imgfp.Hashes, nPhotos)
	worstPos := map[string]int{}
	var posFail []string
	for id := range nPhotos {
		img := photo(uint64(id)+1, 0, 0)
		bases[id] = hash(t, savePNG(t, dir, fmt.Sprintf("base%d.png", id), img))
		for _, v := range variants {
			h := hash(t, v.build(img, id))
			d, ok := ImageMatch(bases[id], h)
			p := imgfp.Dist(bases[id].P, h.P)
			worstPos[v.name] = max(worstPos[v.name], p)
			if !ok && v.must {
				posFail = append(posFail, fmt.Sprintf("%s photo %d (sum %d, pHash %d)", v.name, id, d, p))
			}
		}
	}
	for _, v := range variants {
		t.Logf("positives %-14s worst pHash distance: %2d", v.name, worstPos[v.name])
	}
	for _, f := range posFail {
		t.Errorf("true copy rejected: %s", f)
	}

	// Negatives: every pair of distinct pictures.
	minNeg, falsePos := 64, 0
	for i := range nPhotos {
		for j := i + 1; j < nPhotos; j++ {
			if _, ok := ImageMatch(bases[i], bases[j]); ok {
				falsePos++
				t.Errorf("different pictures %d and %d matched", i, j)
			}
			minNeg = min(minNeg, imgfp.Dist(bases[i].P, bases[j].P))
		}
	}
	t.Logf("negatives (%d pairs): smallest pHash distance %d, false positives %d", nPhotos*(nPhotos-1)/2, minNeg, falsePos)

	// Hard negatives: the same scene photographed again - camera moved and
	// part of the content changed. This is not a duplicate.
	minHard := 64
	for id := range nPhotos {
		other := hash(t, savePNG(t, dir, fmt.Sprintf("hard%d.png", id), photo(uint64(id)+1, 60, 9)))
		if _, ok := ImageMatch(bases[id], other); ok {
			t.Errorf("same-scene different shot %d matched", id)
		}
		minHard = min(minHard, imgfp.Dist(bases[id].P, other.P))
	}
	t.Logf("hard negatives: smallest pHash distance %d", minHard)
}

func TestFlatImagesNeverMatch(t *testing.T) {
	a := imgfp.Hashes{Flat: true}
	if _, ok := ImageMatch(a, a); ok {
		t.Fatal("flat images must not match perceptually")
	}
}

func TestPairsMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	const n = 1500
	h := make([]uint64, n)
	for i := range h {
		if i > 0 && rng.IntN(3) == 0 { // plant near-duplicates
			h[i] = h[rng.IntN(i)]
			for range rng.IntN(9) {
				h[i] ^= 1 << rng.IntN(64)
			}
		} else {
			h[i] = rng.Uint64()
		}
	}
	for _, maxDist := range []int{0, 3, 6, 9} {
		want := map[[2]int]bool{}
		for i := range n {
			for j := i + 1; j < n; j++ {
				if bits.OnesCount64(h[i]^h[j]) <= maxDist {
					want[[2]int{i, j}] = true
				}
			}
		}
		got := map[[2]int]int{}
		Pairs(h, nil, maxDist, func(i, j int) { got[[2]int{i, j}]++ })
		for k, c := range got {
			if c != 1 {
				t.Fatalf("maxDist %d: pair %v emitted %d times", maxDist, k, c)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("maxDist %d: got %d pairs, brute force %d", maxDist, len(got), len(want))
		}
		for k := range want {
			if got[k] == 0 {
				t.Fatalf("maxDist %d: missing pair %v", maxDist, k)
			}
		}
	}
}

func TestPairsSkip(t *testing.T) {
	h := []uint64{5, 5, 5}
	n := 0
	Pairs(h, []bool{false, true, false}, 2, func(i, j int) {
		if i == 1 || j == 1 {
			t.Error("skipped item emitted")
		}
		n++
	})
	if n != 1 {
		t.Fatalf("pairs = %d, want 1", n)
	}
}
