// Package imgfp computes perceptual fingerprints (aHash, dHash, pHash) from
// decoded pixels. Everything is derived from a 32x32 luma thumbnail, so the
// cost is one decode plus one linear pass over the pixels.
package imgfp

import (
	"errors"
	"fmt"
	"image"
	_ "image/gif" // animated GIF: the first frame is fingerprinted
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"math/bits"
	"os"
	"slices"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// N is the side length of the normalised luma thumbnail.
const N = 32

const (
	maxPixels = 200_000_000 // refuse decompression-bomb dimensions
	flatStd   = 3.0         // thumbnails with a lower luma std-dev carry no usable structure
)

// Hashes is the fingerprint of one image: 3x64 bits.
type Hashes struct {
	A, D, P uint64
	// Flat marks near-uniform images (blank, black, solid colour). Their
	// hashes are unstable, so they must never be matched perceptually.
	Flat bool
}

// Dist is the Hamming distance between two 64-bit hashes.
func Dist(a, b uint64) int { return bits.OnesCount64(a ^ b) }

// Compute decodes the image at path, normalises EXIF orientation and returns
// its fingerprint. Only the first frame of animated formats is used.
func Compute(path string) (Hashes, error) {
	f, err := os.Open(path)
	if err != nil {
		return Hashes{}, err
	}
	defer f.Close()
	return ComputeReader(f)
}

// ComputeReader is Compute for an already-open source, such as an in-memory
// copy of the file. That lets a caller keep disk reads sequential while the
// CPU-heavy decode runs on several cores.
func ComputeReader(f io.ReadSeeker) (Hashes, error) {
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return Hashes{}, fmt.Errorf("decode: %w", err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return Hashes{}, fmt.Errorf("image too large (%dx%d)", cfg.Width, cfg.Height)
	}

	if _, err := f.Seek(0, 0); err != nil {
		return Hashes{}, err
	}
	orient := jpegOrientation(f)
	if _, err := f.Seek(0, 0); err != nil {
		return Hashes{}, err
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return Hashes{}, fmt.Errorf("decode: %w", err)
	}
	t := thumbnail(img)
	return fromThumb(applyOrientation(t, orient)), nil
}

// FromGray fingerprints an N*N 8-bit luma buffer (row-major).
func FromGray(b []byte) (Hashes, error) {
	if len(b) != N*N {
		return Hashes{}, errors.New("imgfp: luma buffer must be 32*32 bytes")
	}
	t := new(thumb)
	for i, v := range b {
		t[i] = float32(v)
	}
	return fromThumb(t), nil
}

type thumb [N * N]float32

// thumbnail box-averages the luma of img down to N x N.
func thumbnail(img image.Image) *thumb {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	t := new(thumb)
	if w < N || h < N { // tiny: sample instead of averaging
		row := make([]float32, w)
		for cy := range N {
			y := b.Min.Y + cy*h/N
			fillRow(img, y, row)
			for cx := range N {
				t[cy*N+cx] = row[cx*w/N]
			}
		}
		return t
	}
	var (
		sum [N * N]float64
		cnt [N * N]int32
	)
	xs := make([]int, w)
	for x := range xs {
		xs[x] = x * N / w
	}
	row := make([]float32, w)
	for yy := range h {
		fillRow(img, b.Min.Y+yy, row)
		base := (yy * N / h) * N
		for x, v := range row {
			i := base + xs[x]
			sum[i] += float64(v)
			cnt[i]++
		}
	}
	for i := range t {
		t[i] = float32(sum[i] / float64(cnt[i]))
	}
	return t
}

// fillRow writes the luma (0..255) of row y into buf; fast paths avoid
// the per-pixel interface call of At().
func fillRow(img image.Image, y int, buf []float32) {
	b := img.Bounds()
	switch m := img.(type) {
	case *image.YCbCr:
		o := m.YOffset(b.Min.X, y)
		for i := range buf {
			buf[i] = float32(m.Y[o+i])
		}
	case *image.Gray:
		o := m.PixOffset(b.Min.X, y)
		for i := range buf {
			buf[i] = float32(m.Pix[o+i])
		}
	case *image.RGBA:
		o := m.PixOffset(b.Min.X, y)
		for i := range buf {
			p := m.Pix[o+4*i : o+4*i+3 : o+4*i+3]
			buf[i] = 0.299*float32(p[0]) + 0.587*float32(p[1]) + 0.114*float32(p[2])
		}
	case *image.NRGBA:
		o := m.PixOffset(b.Min.X, y)
		for i := range buf {
			p := m.Pix[o+4*i : o+4*i+4 : o+4*i+4]
			l := 0.299*float32(p[0]) + 0.587*float32(p[1]) + 0.114*float32(p[2])
			buf[i] = l * float32(p[3]) / 255 // composite over black, like RGBA
		}
	default:
		for i := range buf {
			r, g, bl, _ := img.At(b.Min.X+i, y).RGBA()
			buf[i] = (0.299*float32(r) + 0.587*float32(g) + 0.114*float32(bl)) / 257
		}
	}
}

// applyOrientation maps a thumbnail of the stored pixels to the upright view
// given an EXIF orientation (1..8). Rotating the N x N thumbnail is
// equivalent to rotating the full image first, and far cheaper.
func applyOrientation(t *thumb, o int) *thumb {
	if o <= 1 || o > 8 {
		return t
	}
	out := new(thumb)
	const m = N - 1
	for y := range N {
		for x := range N {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = m-x, y
			case 3:
				sx, sy = m-x, m-y
			case 4:
				sx, sy = x, m-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, m-x
			case 7:
				sx, sy = m-y, m-x
			case 8:
				sx, sy = m-y, x
			}
			out[y*N+x] = t[sy*N+sx]
		}
	}
	return out
}

var cosTab = func() (c [8][N]float64) {
	for u := range 8 {
		for n := range N {
			c[u][n] = math.Cos(math.Pi * float64(2*n+1) * float64(u) / (2 * N))
		}
	}
	return
}()

func fromThumb(t *thumb) Hashes {
	var s, s2 float64
	for _, v := range t {
		s += float64(v)
		s2 += float64(v) * float64(v)
	}
	n := float64(len(t))
	std := math.Sqrt(math.Max(s2/n-(s/n)*(s/n), 0))
	return Hashes{A: aHash(t), D: dHash(t), P: pHash(t), Flat: std < flatStd}
}

// aHash: 8x8 block means, bit set when above the global mean.
func aHash(t *thumb) uint64 {
	var cell [64]float64
	var total float64
	for cy := range 8 {
		for cx := range 8 {
			var s float64
			for y := range 4 {
				for x := range 4 {
					s += float64(t[(cy*4+y)*N+cx*4+x])
				}
			}
			cell[cy*8+cx] = s / 16
			total += s / 16
		}
	}
	mean := total / 64
	var h uint64
	for i, v := range cell {
		if v > mean {
			h |= 1 << i
		}
	}
	return h
}

// dHash: 8 rows x 9 columns of block means; bit set when a cell is brighter
// than its right neighbour.
func dHash(t *thumb) uint64 {
	var cell [8][9]float64
	for r := range 8 {
		for c := range 9 {
			x0, x1 := c*N/9, (c+1)*N/9
			var s float64
			for y := r * 4; y < r*4+4; y++ {
				for x := x0; x < x1; x++ {
					s += float64(t[y*N+x])
				}
			}
			cell[r][c] = s / float64(4*(x1-x0))
		}
	}
	var h uint64
	for r := range 8 {
		for c := range 8 {
			if cell[r][c] > cell[r][c+1] {
				h |= 1 << (r*8 + c)
			}
		}
	}
	return h
}

// pHash: low-frequency 8x8 block of the 2-D DCT, bit set when a coefficient
// exceeds the median of the AC coefficients.
func pHash(t *thumb) uint64 {
	var tmp [N][8]float64
	for y := range N {
		for v := range 8 {
			var s float64
			for x := range N {
				s += float64(t[y*N+x]) * cosTab[v][x]
			}
			tmp[y][v] = s
		}
	}
	var coef [64]float64
	for u := range 8 {
		for v := range 8 {
			var s float64
			for y := range N {
				s += tmp[y][v] * cosTab[u][y]
			}
			coef[u*8+v] = s
		}
	}
	ac := slices.Clone(coef[1:])
	slices.Sort(ac)
	med := ac[len(ac)/2]
	var h uint64
	for i, v := range coef {
		if v > med {
			h |= 1 << i
		}
	}
	return h
}
