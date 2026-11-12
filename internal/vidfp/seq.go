package vidfp

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RishiKendai/veyra/internal/imgfp"
)

// A Seq is a fingerprint of a whole video made of one perceptual hash per
// keyframe, each with its timestamp. Unlike FP (16 frames at fractions of the
// duration) it is anchored to absolute time, so two cuts of the same film that
// differ by an intro, a logo or a few removed scenes still line up: the matcher
// looks for a consistent time offset between them (similarity.SeqMatch).
//
// Only keyframes are decoded (ffmpeg -skip_frame nokey), which is a small
// fraction of the frames in a film, and the result is cached per file.
type Seq struct {
	Duration float64
	Frames   []SeqFrame // informative keyframes in time order
	// Distinct is the number of visually different shots among Frames. It is
	// derived, not stored: Build and DecodeSeq fill it.
	Distinct int
}

func (s *Seq) countDistinct() {
	var reps []uint64
	for _, f := range s.Frames {
		novel := true
		for _, r := range reps {
			if imgfp.Dist(r, f.H) <= DistinctFrameDist {
				novel = false
				break
			}
		}
		if novel {
			reps = append(reps, f.H)
		}
	}
	s.Distinct = len(reps)
}

// SeqFrame is one sampled keyframe.
type SeqFrame struct {
	T float32 // seconds
	H uint64  // pHash of the frame with black bars removed
}

const (
	// SeqCodecVersion belongs in the params string of any cache holding Seq values.
	SeqCodecVersion = "vidseq1;key3s;lb64;crop"

	seqMinGap = 3 // seconds between kept keyframes (all-intra files would otherwise yield every frame)
	seqSide   = 64
	seqFrame  = seqSide * seqSide
	// A film is decoded in one pass, so the budget is generous.
	seqTimeout = 20 * time.Minute

	// Letterbox detection: a row or column belongs to the picture when its
	// brightest pixel exceeds this (video black is 16, compression noise a bit above).
	barLevel = 40
	// A crop is only used when it leaves at least this much of the frame.
	minPicture = seqSide / 2
)

var seqFilter = fmt.Sprintf("select='key*(isnan(prev_selected_t)+gte(t-prev_selected_t,%d))',showinfo,scale=%d:%d:flags=area,format=gray", seqMinGap, seqSide, seqSide)

// ComputeSeq decodes the keyframes of path and fingerprints them. On macOS it
// first tries the hardware decoder, which is several times faster, and falls
// back to software if that yields nothing.
func ComputeSeq(ctx context.Context, path string, duration float64) (Seq, error) {
	ctx, cancel := context.WithTimeout(ctx, seqTimeout)
	defer cancel()
	var raw []byte
	var ts []float32
	var err error
	if runtime.GOOS == "darwin" {
		raw, ts, err = decodeKeyframes(ctx, path, "-hwaccel", "videotoolbox")
	}
	if len(ts) < 2 && ctx.Err() == nil {
		raw, ts, err = decodeKeyframes(ctx, path)
	}
	if ctx.Err() != nil {
		return Seq{}, ctx.Err()
	}
	if len(ts) == 0 {
		if err == nil {
			err = errors.New("no keyframes")
		}
		return Seq{}, fmt.Errorf("no decodable keyframes: %w", err)
	}
	return buildSeq(raw, ts, duration)
}

// decodeKeyframes runs one ffmpeg process and returns the 64x64 luma of every
// kept keyframe and its timestamp.
func decodeKeyframes(ctx context.Context, path string, pre ...string) ([]byte, []float32, error) {
	args := append([]string{"-v", "info", "-nostdin"}, pre...)
	args = append(args, "-skip_frame", "nokey", "-i", path, "-map", "0:v:0", "-an", "-sn",
		"-vf", seqFilter, "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.WaitDelay = 2 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("ffmpeg: %w", err)
	}

	// showinfo prints one line per frame on stderr; frames arrive in order on stdout.
	var (
		ts   []float32
		last string
		wg   sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if i := indexOf(line, "pts_time:"); i >= 0 {
				rest := line[i+len("pts_time:"):]
				j := 0
				for j < len(rest) && rest[j] != ' ' {
					j++
				}
				if v, err := strconv.ParseFloat(rest[:j], 64); err == nil {
					ts = append(ts, float32(v))
				}
			} else if line != "" {
				last = line
			}
		}
	}()
	raw, rerr := io.ReadAll(bufio.NewReaderSize(stdout, 1<<20))
	wg.Wait()
	werr := cmd.Wait()
	if rerr == nil && werr != nil && ctx.Err() == nil && len(raw) == 0 {
		rerr = fmt.Errorf("%v: %s", werr, last)
	}
	n := len(raw) / seqFrame
	raw = raw[:n*seqFrame]
	if len(ts) > n {
		ts = ts[:n]
	}
	if n > len(ts) {
		raw = raw[:len(ts)*seqFrame]
	}
	return raw, ts, rerr
}

func indexOf(s, sub string) int { return strings.Index(s, sub) }

// buildSeq crops the black bars shared by all frames and hashes each frame.
func buildSeq(raw []byte, ts []float32, duration float64) (Seq, error) {
	n := len(ts)
	box := pictureBox(raw, n)
	frames := make([]SeqFrame, 0, n)
	var small [imgfp.N * imgfp.N]byte
	for i := range n {
		resize(raw[i*seqFrame:(i+1)*seqFrame], box, small[:])
		h, err := imgfp.FromGray(small[:])
		if err != nil {
			return Seq{}, err
		}
		if h.Flat {
			continue
		}
		frames = append(frames, SeqFrame{T: ts[i], H: h.P})
	}
	if len(frames) == 0 {
		return Seq{}, errors.New("no informative keyframes")
	}
	out := Seq{Duration: duration, Frames: frames}
	out.countDistinct()
	return out, nil
}

type rect struct{ x0, y0, x1, y1 int } // half-open

// pictureBox finds the picture area of the video. Letterbox bars are constant
// for the whole video, but a dark scene makes the bars look bigger, so each
// frame gives a bound and the least aggressive one (10th/90th percentile) wins.
func pictureBox(raw []byte, n int) rect {
	full := rect{0, 0, seqSide, seqSide}
	var xs0, ys0, xs1, ys1 []int
	for i := range n {
		f := raw[i*seqFrame : (i+1)*seqFrame]
		var colMax, rowMax [seqSide]byte
		for y := range seqSide {
			for x := range seqSide {
				v := f[y*seqSide+x]
				rowMax[y] = max(rowMax[y], v)
				colMax[x] = max(colMax[x], v)
			}
		}
		y0, y1 := bounds(rowMax[:])
		x0, x1 := bounds(colMax[:])
		if y1 <= y0 || x1 <= x0 {
			continue // black frame
		}
		ys0, ys1, xs0, xs1 = append(ys0, y0), append(ys1, y1), append(xs0, x0), append(xs1, x1)
	}
	if len(ys0) < 5 {
		return full
	}
	b := rect{pct(xs0, 10), pct(ys0, 10), pct(xs1, 90), pct(ys1, 90)}
	// Ignore a one-pixel border: it is rounding, not a bar, and cropping it
	// would make this video hash differently from an uncropped copy.
	if b.x0 < 2 {
		b.x0 = 0
	}
	if b.y0 < 2 {
		b.y0 = 0
	}
	if seqSide-b.x1 < 2 {
		b.x1 = seqSide
	}
	if seqSide-b.y1 < 2 {
		b.y1 = seqSide
	}
	if b.x1-b.x0 < minPicture || b.y1-b.y0 < minPicture {
		return full
	}
	return b
}

// bounds returns the first and one-past-last index whose value exceeds barLevel.
func bounds(m []byte) (lo, hi int) {
	lo, hi = 0, 0
	for i, v := range m {
		if v > barLevel {
			lo = i
			break
		}
	}
	for i := len(m) - 1; i >= 0; i-- {
		if m[i] > barLevel {
			hi = i + 1
			break
		}
	}
	return lo, hi
}

func pct(v []int, p int) int {
	s := slices.Clone(v)
	slices.Sort(s)
	return s[min(len(s)-1, len(s)*p/100)]
}

// resize area-averages the box of a 64x64 frame down to 32x32.
func resize(src []byte, b rect, dst []byte) {
	const N = imgfp.N
	w, h := b.x1-b.x0, b.y1-b.y0
	for oy := range N {
		sy0, sy1 := float64(oy*h)/N, float64((oy+1)*h)/N
		for ox := range N {
			sx0, sx1 := float64(ox*w)/N, float64((ox+1)*w)/N
			var sum, wt float64
			for y := int(sy0); y < h && float64(y) < sy1; y++ {
				wy := math.Min(float64(y+1), sy1) - math.Max(float64(y), sy0)
				for x := int(sx0); x < w && float64(x) < sx1; x++ {
					wx := math.Min(float64(x+1), sx1) - math.Max(float64(x), sx0)
					sum += float64(src[(b.y0+y)*seqSide+b.x0+x]) * wx * wy
					wt += wx * wy
				}
			}
			if wt > 0 {
				dst[oy*N+ox] = byte(sum/wt + 0.5)
			}
		}
	}
}

// Encode serialises s for a persistent cache.
func (s Seq) Encode() []byte {
	b := make([]byte, 12+len(s.Frames)*12)
	binary.LittleEndian.PutUint64(b, math.Float64bits(s.Duration))
	binary.LittleEndian.PutUint32(b[8:], uint32(len(s.Frames)))
	o := 12
	for _, f := range s.Frames {
		binary.LittleEndian.PutUint32(b[o:], math.Float32bits(f.T))
		binary.LittleEndian.PutUint64(b[o+4:], f.H)
		o += 12
	}
	return b
}

// DecodeSeq reverses Seq.Encode. It reports false for malformed input.
func DecodeSeq(b []byte) (Seq, bool) {
	if len(b) < 12 {
		return Seq{}, false
	}
	n := int(binary.LittleEndian.Uint32(b[8:]))
	if n < 0 || len(b) != 12+n*12 {
		return Seq{}, false
	}
	s := Seq{Duration: math.Float64frombits(binary.LittleEndian.Uint64(b)), Frames: make([]SeqFrame, n)}
	o := 12
	for i := range s.Frames {
		s.Frames[i] = SeqFrame{T: math.Float32frombits(binary.LittleEndian.Uint32(b[o:])), H: binary.LittleEndian.Uint64(b[o+4:])}
		o += 12
	}
	s.countDistinct()
	return s, true
}
