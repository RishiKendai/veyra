// Package vidfp fingerprints a video as a fixed number of perceptual frame
// hashes taken at normalised timestamps. Originals are only read.
package vidfp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/RishiKendai/veyra/internal/imgfp"
	"github.com/RishiKendai/veyra/internal/prof"
)

const (
	// K is the number of sampled frames. It is a constant of the stored
	// fingerprint layout; the sampling strategy is in Timestamps.
	K = 16

	// DistinctFrameDist: two frames further apart than this count as
	// different shots when measuring how much a video actually changes.
	DistinctFrameDist = 12

	computeTimeout = 90 * time.Second
	frameBytes     = imgfp.N * imgfp.N
)

// FP is the fingerprint of one video.
type FP struct {
	P        [K]uint64 // pHash of each sampled frame
	Info     uint32    // bit i set: frame i decoded and has visual structure
	Distinct int       // number of visually distinct shots among informative frames
	Duration float64   // seconds, from ffprobe
	// Have has bit i set when frame i was attempted (decoded or not). A probe
	// has only the ProbeLo..ProbeHi bits; a complete fingerprint has all K.
	// Zero means "unrecorded" and is treated as complete by callers that build
	// fingerprints by hand.
	Have uint32
}

// The probe is a contiguous run of sample frames in the middle of the film.
// It is long enough that two probes still overlap in more frame pairs than the
// matcher tolerates as mismatches at every shift (see similarity.ProbeMayMatch),
// and shorter than similarity.MinCompared, so a probe alone can never qualify
// a video for matching.
const (
	ProbeLo = 5
	ProbeHi = 10
)

// FullMask has a bit for every sample frame.
const FullMask uint32 = 1<<K - 1

// ProbeMask has a bit for every probe frame.
const ProbeMask uint32 = (1<<(ProbeHi+1) - 1) &^ (1<<ProbeLo - 1)

// Complete reports whether every sample frame was attempted.
func (f *FP) Complete() bool { return f.Have == FullMask }

// HasProbe reports whether the probe frames were attempted.
func (f *FP) HasProbe() bool { return f.Have&ProbeMask == ProbeMask }

// Informative reports whether frame i can take part in matching.
func (f *FP) Informative(i int) bool { return f.Info&(1<<i) != 0 }

// Timestamps returns the sample times for a video of the given duration:
// the centres of K equal slices. Centres avoid black intros/outros and the
// unreliable last frames of a stream.
func Timestamps(duration float64) [K]float64 {
	var ts [K]float64
	for i := range ts {
		t := duration * (float64(i) + 0.5) / K
		ts[i] = max(0, min(t, duration-0.05))
	}
	return ts
}

// Compute extracts all K sample frames with a single ffmpeg process (one
// seeking input and one pipe output per sample) and hashes them.
func Compute(ctx context.Context, path string, duration float64) (FP, error) {
	defer prof.Since("video.tier1(16 seeks)", prof.Start(), 0)
	return compute(ctx, path, FP{Duration: duration}, FullMask)
}

// ComputeProbe extracts only the probe frames. It is a fraction of the cost of
// Compute and is enough to rule most unrelated videos out; see Complete.
func ComputeProbe(ctx context.Context, path string, duration float64) (FP, error) {
	defer prof.Since("video.tier1.probe", prof.Start(), 0)
	return compute(ctx, path, FP{Duration: duration}, ProbeMask)
}

// Complete extracts the sample frames fp is missing and returns the full
// fingerprint, identical to what Compute would have produced.
func Complete(ctx context.Context, path string, fp FP) (FP, error) {
	defer prof.Since("video.tier1.complete", prof.Start(), 0)
	return compute(ctx, path, fp, FullMask&^fp.Have)
}

// compute extracts the frames in want (a bitmask of sample indices) into fp.
func compute(ctx context.Context, path string, fp FP, want uint32) (FP, error) {
	duration := fp.Duration
	if !(duration > 0) {
		return fp, errors.New("unknown duration")
	}
	if want == 0 {
		return fp, nil
	}
	ctx, cancel := context.WithTimeout(ctx, computeTimeout)
	defer cancel()

	var idx []int
	for i := range K {
		if want&(1<<i) != 0 {
			idx = append(idx, i)
		}
	}
	ts := Timestamps(duration)
	args := []string{"-v", "error", "-nostdin"}
	for _, i := range idx {
		args = append(args, "-ss", fmt.Sprintf("%.3f", ts[i]), "-i", path)
	}
	readers := make([]*os.File, len(idx))
	writers := make([]*os.File, len(idx))
	defer func() {
		for i := range idx {
			if readers[i] != nil {
				readers[i].Close()
			}
			if writers[i] != nil {
				writers[i].Close()
			}
		}
	}()
	for n := range idx {
		r, w, err := os.Pipe()
		if err != nil {
			return fp, err
		}
		readers[n], writers[n] = r, w
		args = append(args,
			"-map", fmt.Sprintf("%d:v:0", n), "-an", "-sn", "-frames:v", "1",
			"-vf", "scale=32:32:flags=area", "-pix_fmt", "gray", "-f", "rawvideo",
			fmt.Sprintf("pipe:%d", 3+n))
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.ExtraFiles = writers
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return fp, fmt.Errorf("ffmpeg: %w", err)
	}
	for n := range writers { // parent must drop its copies so reads see EOF
		writers[n].Close()
		writers[n] = nil
	}
	runErr := cmd.Wait()
	if ctx.Err() != nil {
		return fp, fmt.Errorf("ffmpeg: %w", ctx.Err())
	}

	// Each output is one 1 KiB frame, far below the pipe buffer, so ffmpeg
	// never blocks on us and we can read after it exits.
	var buf [frameBytes]byte
	decoded := 0
	for n, i := range idx {
		fp.Have |= 1 << i
		if _, err := io.ReadFull(readers[n], buf[:]); err != nil {
			continue // frame missing (seek past EOF, decode error)
		}
		decoded++
		h, err := imgfp.FromGray(buf[:])
		if err != nil {
			return fp, err
		}
		fp.P[i] = h.P
		if !h.Flat {
			fp.Info |= 1 << i
		}
	}
	fp.Distinct = distinct(&fp)
	if decoded == 0 && fp.Info == 0 {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		return fp, fmt.Errorf("no decodable frames: %s", msg)
	}
	return fp, nil
}

// distinct counts visually distinct shots among the informative frames, in
// index order, so a completed probe gives the same value as one pass over all frames.
func distinct(fp *FP) int {
	var reps []uint64
	for i := range K {
		if !fp.Informative(i) {
			continue
		}
		novel := true
		for _, r := range reps {
			if imgfp.Dist(r, fp.P[i]) <= DistinctFrameDist {
				novel = false
				break
			}
		}
		if novel {
			reps = append(reps, fp.P[i])
		}
	}
	return len(reps)
}
