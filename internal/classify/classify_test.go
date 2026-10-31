package classify

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseRate(t *testing.T) {
	for in, want := range map[string]float64{"30/1": 30, "30000/1001": 29.97002997, "0/0": 0, "": 0, "25": 25, "x/y": 0} {
		if got := parseRate(in); math.Abs(got-want) > 1e-6 {
			t.Errorf("parseRate(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestImageMeta(t *testing.T) {
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 30, 20)))
	p := filepath.Join(t.TempDir(), "x")
	os.WriteFile(p, b.Bytes(), 0o600)
	m, err := Image(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != "png" || m.Width != 30 || m.Height != 20 || m.ColorModel == "" {
		t.Fatalf("%+v", m)
	}
	if _, err := Image(context.Background(), filepath.Join(t.TempDir(), "missing")); err == nil || errors.Is(err, ErrNotMedia) {
		t.Fatalf("missing file should be an I/O error, got %v", err)
	}
}

func TestVideoMeta(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		if out, err := exec.Command("ffmpeg", append([]string{"-v", "error", "-nostdin", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	silent, withAudio := filepath.Join(dir, "silent.mp4"), filepath.Join(dir, "audio.mp4")
	run("-f", "lavfi", "-i", "testsrc=s=96x64:r=25", "-t", "2", "-pix_fmt", "yuv420p", silent)
	run("-f", "lavfi", "-i", "testsrc=s=96x64:r=25", "-f", "lavfi", "-i", "sine=d=2", "-t", "2", "-pix_fmt", "yuv420p", "-c:a", "aac", withAudio)

	m, err := Video(context.Background(), silent)
	if err != nil {
		t.Fatal(err)
	}
	if m.Width != 96 || m.Height != 64 || m.Codec != "h264" || m.FrameRate != 25 || m.Duration < 1.9 || m.BitRate <= 0 || m.AudioCodec != "" {
		t.Fatalf("silent: %+v", m)
	}
	m, err = Video(context.Background(), withAudio)
	if err != nil || m.AudioCodec != "aac" {
		t.Fatalf("with audio: %+v %v", m, err)
	}
}
