package vidfp

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// A probe completed later must equal a fingerprint computed in one pass.
func TestProbeThenCompleteEqualsCompute(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	path := filepath.Join(t.TempDir(), "v.mp4")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10:duration=20",
		"-pix_fmt", "yuv420p", path).CombinedOutput()
	if err != nil {
		t.Skipf("cannot make test video: %v %s", err, out)
	}
	ctx := context.Background()
	full, err := Compute(ctx, path, 20)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := ComputeProbe(ctx, path, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !probe.HasProbe() || probe.Complete() || probe.Have != ProbeMask {
		t.Fatalf("probe covers wrong frames: have=%#x", probe.Have)
	}
	for i := range K {
		if probe.Have&(1<<i) != 0 && (probe.P[i] != full.P[i] || probe.Informative(i) != full.Informative(i)) {
			t.Errorf("probe frame %d differs from full", i)
		}
	}
	done, err := Complete(ctx, path, probe)
	if err != nil {
		t.Fatal(err)
	}
	if done != full || !done.Complete() {
		t.Errorf("completed probe differs from one-pass fingerprint:\n got %+v\nwant %+v", done, full)
	}
	if got, ok := DecodeFP(done.Encode()); !ok || got != done {
		t.Error("codec round trip lost data")
	}
}
