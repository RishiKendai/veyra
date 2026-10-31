package similarity

import (
	"context"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/RishiKendai/veyra/internal/classify"
	"github.com/RishiKendai/veyra/internal/vidfp"
)

func synthFP(seed uint64, informative int, distinct int, dur float64) vidfp.FP {
	fp := vidfp.FP{Duration: dur, Distinct: distinct}
	h := seed * 0x9E3779B97F4A7C15
	for i := range vidfp.K {
		h = h*6364136223846793005 + 1442695040888963407
		fp.P[i] = h
		if i < informative {
			fp.Info |= 1 << i
		}
	}
	return fp
}

func TestVideoMatchUnit(t *testing.T) {
	a := synthFP(1, 16, 8, 60)

	t.Run("identical", func(t *testing.T) {
		if mm, ok := VideoMatch(&a, &a); !ok || mm != 0 {
			t.Fatalf("mm=%d ok=%v", mm, ok)
		}
	})
	t.Run("shifted by one sample", func(t *testing.T) {
		b := a
		for i := range vidfp.K { // b[i] == a[i+1]
			b.P[i] = a.P[min(i+1, vidfp.K-1)]
		}
		if _, ok := VideoMatch(&a, &b); !ok {
			t.Fatal("a one-sample trim offset should still match")
		}
	})
	t.Run("small per-frame noise", func(t *testing.T) {
		b := a
		for i := range vidfp.K {
			b.P[i] ^= 0b10101 // 3 bits
		}
		if _, ok := VideoMatch(&a, &b); !ok {
			t.Fatal("re-encode-level noise should match")
		}
	})
	t.Run("unrelated", func(t *testing.T) {
		b := synthFP(2, 16, 8, 60)
		if _, ok := VideoMatch(&a, &b); ok {
			t.Fatal("unrelated hashes matched")
		}
	})
	t.Run("too few comparable frames", func(t *testing.T) {
		b := a
		b.Info = 0b111 // only 3 informative frames
		if _, ok := VideoMatch(&a, &b); ok {
			t.Fatal("matched on fewer than MinCompared frames")
		}
	})
	t.Run("half the frames match is not enough", func(t *testing.T) {
		b := a
		for i := 0; i < vidfp.K; i += 2 {
			b.P[i] = ^a.P[i]
		}
		if _, ok := VideoMatch(&a, &b); ok {
			t.Fatal("50% match accepted")
		}
	})
	t.Run("static video is ineligible", func(t *testing.T) {
		s := synthFP(3, 16, 1, 60)
		if Eligible(&s) {
			t.Fatal("single-shot video must not be eligible")
		}
		edges := VideoEdges([]vidfp.FP{s, s}, nil)
		if len(edges) != 0 {
			t.Fatal("static videos must never produce perceptual edges")
		}
	})
	t.Run("duration window", func(t *testing.T) {
		b := a
		b.Duration = 120
		if e := VideoEdges([]vidfp.FP{a, b}, nil); len(e) != 0 {
			t.Fatal("durations 2x apart should not be candidates")
		}
		b.Duration = 62
		if e := VideoEdges([]vidfp.FP{a, b}, nil); len(e) != 1 {
			t.Fatalf("edges = %v, want 1", e)
		}
	})
}

// ffmpeg runs a command and fails the test with its output on error.
func ffmpeg(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command("ffmpeg", append([]string{"-v", "error", "-nostdin", "-y"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg %v: %v\n%s", args, err, out)
	}
}

func fingerprint(t *testing.T, path string) vidfp.FP {
	t.Helper()
	meta, err := classify.Video(context.Background(), path)
	if err != nil {
		t.Fatalf("classify %s: %v", filepath.Base(path), err)
	}
	fp, err := vidfp.Compute(context.Background(), path, meta.Duration)
	if err != nil {
		t.Fatalf("fingerprint %s: %v", filepath.Base(path), err)
	}
	return fp
}

func TestVideoEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	p := func(n string) string { return filepath.Join(dir, n) }
	src := func(x, y string) string {
		return "mandelbrot=s=320x240:rate=15:end_pts=90:maxiter=300:start_x=" + x + ":start_y=" + y
	}
	const x1, y1 = "-0.7436", "0.1318"
	ffmpeg(t, "-f", "lavfi", "-i", src(x1, y1), "-t", "6", "-pix_fmt", "yuv420p", "-c:v", "libx264", "-crf", "18", p("orig.mp4"))
	ffmpeg(t, "-i", p("orig.mp4"), "-c", "copy", p("remux.mkv"))
	ffmpeg(t, "-i", p("orig.mp4"), "-vf", "scale=160:120", "-c:v", "libx264", "-crf", "35", p("small_lowq.mp4"))
	ffmpeg(t, "-i", p("orig.mp4"), "-c:v", "mpeg4", "-q:v", "8", p("mpeg4.avi"))
	ffmpeg(t, "-i", p("orig.mp4"), "-r", "24", "-c:v", "libx264", "-crf", "23", p("fps24.mp4"))
	ffmpeg(t, "-i", p("orig.mp4"), "-ss", "0.15", "-c:v", "libx264", "-crf", "23", p("trim_start.mp4"))
	// Same duration and resolution, different footage.
	ffmpeg(t, "-f", "lavfi", "-i", src("-1.25", "0.02"), "-t", "6", "-pix_fmt", "yuv420p", "-c:v", "libx264", "-crf", "18", p("other.mp4"))
	ffmpeg(t, "-f", "lavfi", "-i", src("-0.16", "1.0375"), "-t", "6", "-pix_fmt", "yuv420p", "-c:v", "libx264", "-crf", "18", p("other2.mp4"))
	// Different videos of the same static scene: a still frame held for 6 s.
	ffmpeg(t, "-f", "lavfi", "-i", src(x1, y1), "-frames:v", "1", p("still.png"))
	ffmpeg(t, "-loop", "1", "-framerate", "15", "-i", p("still.png"), "-t", "6", "-pix_fmt", "yuv420p", "-c:v", "libx264", "-crf", "18", p("static1.mp4"))
	ffmpeg(t, "-loop", "1", "-framerate", "15", "-i", p("still.png"), "-t", "6", "-vf", "scale=160:120", "-pix_fmt", "yuv420p", "-c:v", "libx264", "-crf", "30", p("static2.mp4"))
	// Solid black: flat, no structure.
	ffmpeg(t, "-f", "lavfi", "-i", "color=c=black:s=320x240:r=15", "-t", "6", "-pix_fmt", "yuv420p", p("black1.mp4"))
	ffmpeg(t, "-f", "lavfi", "-i", "color=c=black:s=160x120:r=15", "-t", "6", "-pix_fmt", "yuv420p", "-crf", "30", p("black2.mp4"))
	// No audio stream is the default above; add one video with audio to prove audio does not matter.
	ffmpeg(t, "-i", p("orig.mp4"), "-f", "lavfi", "-i", "sine=f=440:d=6", "-c:v", "copy", "-c:a", "aac", "-shortest", p("with_audio.mp4"))
	// Misleading extension.
	data, _ := os.ReadFile(p("orig.mp4"))
	os.WriteFile(p("noext"), data, 0o600)

	names := []string{"orig.mp4", "remux.mkv", "small_lowq.mp4", "mpeg4.avi", "fps24.mp4", "trim_start.mp4",
		"other.mp4", "other2.mp4", "static1.mp4", "static2.mp4", "black1.mp4", "black2.mp4", "with_audio.mp4", "noext"}
	fps := make([]vidfp.FP, len(names))
	idx := map[string]int{}
	for i, n := range names {
		fps[i] = fingerprint(t, p(n))
		idx[n] = i
		t.Logf("%-16s dur=%.2f informative=%2d distinct=%2d eligible=%v", n, fps[i].Duration, popcount(fps[i].Info), fps[i].Distinct, Eligible(&fps[i]))
	}
	match := func(a, b string) bool {
		mm, ok := VideoMatch(&fps[idx[a]], &fps[idx[b]])
		eligible := Eligible(&fps[idx[a]]) && Eligible(&fps[idx[b]])
		t.Logf("  %-14s vs %-14s mismatches=%d match=%v eligible=%v", a, b, mm, ok && eligible, eligible)
		return ok && eligible
	}

	for _, n := range []string{"remux.mkv", "small_lowq.mp4", "mpeg4.avi", "fps24.mp4", "with_audio.mp4", "noext", "trim_start.mp4"} {
		if !match("orig.mp4", n) {
			t.Errorf("same footage %q should match orig.mp4", n)
		}
	}
	for _, n := range []string{"other.mp4", "other2.mp4", "static1.mp4", "black1.mp4"} {
		if match("orig.mp4", n) {
			t.Errorf("%q must not match orig.mp4", n)
		}
	}
	if match("other.mp4", "other2.mp4") {
		t.Error("unrelated same-duration same-resolution videos matched")
	}
	if match("static1.mp4", "static2.mp4") {
		t.Error("static scenes must not match perceptually")
	}
	if match("black1.mp4", "black2.mp4") {
		t.Error("black videos must not match perceptually")
	}
}

func TestVideoFingerprintErrors(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	good := filepath.Join(dir, "good.mp4")
	ffmpeg(t, "-f", "lavfi", "-i", "testsrc=s=64x64:r=10", "-t", "2", "-pix_fmt", "yuv420p", good)
	data, _ := os.ReadFile(good)

	trunc := filepath.Join(dir, "trunc.mp4")
	os.WriteFile(trunc, data[:len(data)/10], 0o600)
	if _, err := vidfp.Compute(context.Background(), trunc, 2); err == nil {
		t.Log("truncated file produced a (partial) fingerprint; frames beyond the cut are simply missing")
	}

	garbage := filepath.Join(dir, "garbage.mp4")
	os.WriteFile(garbage, []byte("not a video at all, just some text"), 0o600)
	if _, err := vidfp.Compute(context.Background(), garbage, 2); err == nil {
		t.Error("garbage must error")
	}
	if _, err := vidfp.Compute(context.Background(), good, 0); err == nil {
		t.Error("unknown duration must error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := vidfp.Compute(ctx, good, 2); err == nil {
		t.Error("cancelled context must error")
	}
}

func TestTimestamps(t *testing.T) {
	ts := vidfp.Timestamps(16)
	for i, v := range ts {
		if want := float64(i) + 0.5; v != want {
			t.Errorf("ts[%d]=%v want %v", i, v, want)
		}
	}
	for _, v := range vidfp.Timestamps(0.2) {
		if v < 0 || v > 0.2 {
			t.Errorf("timestamp %v outside clip", v)
		}
	}
}

func TestNeedsFingerprint(t *testing.T) {
	// Brute force over every pair must agree with the one-pass neighbour check.
	durs := []float64{0, 0, 300, 310, 341, 342, 500, 900, 1000, 1099, 1101, 5400}
	got := NeedsFingerprint(durs, nil)
	for i := range durs {
		want := false
		for j := range durs {
			if i == j {
				continue
			}
			lo, hi := min(durs[i], durs[j]), max(durs[i], durs[j])
			if hi <= lo*(1+DurationWindow) {
				want = true
			}
		}
		if got[i] != want {
			t.Errorf("duration %v: got %v want %v", durs[i], got[i], want)
		}
	}
	// Unique duration is not fingerprinted; two unknown durations are.
	if got[7] || got[11] || !got[0] || !got[1] {
		t.Errorf("unexpected mask %v", got)
	}
	// Skipped nodes are excluded and never marked.
	skip := make([]bool, len(durs))
	skip[2] = true
	m := NeedsFingerprint(durs, skip)
	if m[2] {
		t.Errorf("a skipped node must never be marked: %v", m)
	}
}

func TestProbeConstants(t *testing.T) {
	n := vidfp.ProbeHi - vidfp.ProbeLo + 1
	if n >= MinCompared {
		t.Errorf("probe of %d frames could qualify a video on its own (MinCompared %d)", n, MinCompared)
	}
	if n-MaxShift <= maxMismatches {
		t.Errorf("at the widest shift only %d probe pairs overlap, not more than the %d tolerated mismatches: the probe could never reject", n-MaxShift, maxMismatches)
	}
	if maxMismatches != 2 {
		t.Errorf("maxMismatches = %d, derived from K=%d and MinMatchPct=%d", maxMismatches, vidfp.K, MinMatchPct)
	}
}

// The probe must never reject a pair VideoMatch would accept. Generate many
// related pairs (shifted, perturbed, partly replaced, partly uninformative) and
// require ProbeMayMatch whenever VideoMatch matches.
func TestProbeNeverRejectsAMatch(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	matches, rejected := 0, 0
	for range 200000 {
		a := vidfp.FP{Duration: 100}
		b := vidfp.FP{Duration: 100}
		for i := range vidfp.K {
			a.P[i] = rng.Uint64()
			if rng.IntN(10) > 0 {
				a.Info |= 1 << i
			}
		}
		shift := rng.IntN(2*MaxShift+1) - MaxShift
		for j := range vidfp.K {
			src := j - shift
			if src < 0 || src >= vidfp.K {
				b.P[j] = rng.Uint64()
			} else {
				b.P[j] = a.P[src]
				for range rng.IntN(FrameDist + 1) { // small per-frame drift
					b.P[j] ^= 1 << rng.IntN(64)
				}
			}
			if rng.IntN(10) > 0 {
				b.Info |= 1 << j
			}
		}
		for range rng.IntN(5) { // replace a few frames outright
			b.P[rng.IntN(vidfp.K)] = rng.Uint64()
		}
		_, full := VideoMatch(&a, &b)
		if full {
			matches++
			if !ProbeMayMatch(&a, &b) {
				t.Fatalf("probe rejected a pair VideoMatch accepts:\n a=%+v\n b=%+v", a, b)
			}
		}
		if !ProbeMayMatch(&a, &b) {
			rejected++
		}
	}
	if matches < 10000 {
		t.Fatalf("only %d matching pairs generated; the test is not exercising the property", matches)
	}
	if rejected == 0 {
		t.Error("probe never rejected anything")
	}
	t.Logf("%d matching pairs all kept; %d pairs rejected by the probe", matches, rejected)
}

func TestProbeSurvivors(t *testing.T) {
	same := synthFP(1, 16, 8, 100)
	twin := same
	twin.Duration = 105
	other := synthFP(2, 16, 8, 102)
	far := synthFP(3, 16, 8, 900) // outside every window
	fps := []vidfp.FP{same, twin, other, far}
	got := ProbeSurvivors(fps, []bool{true, true, true, true})
	if !got[0] || !got[1] {
		t.Errorf("a pair with identical probes must survive: %v", got)
	}
	if got[2] || got[3] {
		t.Errorf("unrelated or out-of-window videos must not survive: %v", got)
	}
	if got := ProbeSurvivors(fps, []bool{true, false, true, true}); got[0] || got[1] {
		t.Errorf("a node without a probe never survives, and takes its partner with it: %v", got)
	}
}
