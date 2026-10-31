package app

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/image/draw"

	"github.com/RishiKendai/veyra/internal/hashcache"
	"github.com/RishiKendai/veyra/internal/model"
	"github.com/RishiKendai/veyra/internal/result"
	"github.com/RishiKendai/veyra/internal/volume"
)

// pic makes a deterministic structured picture that differs per seed.
func pic(seed int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 160, 120))
	s := float64(seed)
	for y := range 120 {
		for x := range 160 {
			v := 128 + 60*math.Sin(float64(x)*(0.05+0.013*s)+s) + 50*math.Cos(float64(y)*(0.07+0.011*s)-s*2) +
				40*math.Sin(float64(x+y)*0.03*(s+1))
			c := uint8(max(0, min(255, v)))
			img.Set(x, y, color.RGBA{c, c / 2, 255 - c, 255})
		}
	}
	return img
}

func writePNG(t *testing.T, path string, img image.Image) {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, b.Bytes())
}

func writeJPEG(t *testing.T, path string, img image.Image, q int) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, b.Bytes())
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// tempRoot returns a temp dir with symlinks resolved (macOS /var -> /private/var),
// matching the paths the scanner reports.
func tempRoot(t *testing.T) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func summarize(root string, groups []result.Group) map[string][]string {
	out := map[string][]string{}
	for _, g := range groups {
		var names []string
		for _, f := range g.Files {
			r, _ := filepath.Rel(root, f.Path)
			names = append(names, r)
		}
		out[string(g.Class)+":"+names[0]] = names
	}
	return out
}

func TestImagePipeline(t *testing.T) {
	root := tempRoot(t)
	a, b, c := pic(1), pic(5), pic(9)

	writePNG(t, filepath.Join(root, "a.png"), a)
	writePNG(t, filepath.Join(root, "a-copy.png"), a)     // exact copy of a.png
	writeJPEG(t, filepath.Join(root, "a-q60.jpg"), a, 60) // visual
	small := image.NewRGBA(image.Rect(0, 0, 80, 60))
	draw.CatmullRom.Scale(small, small.Bounds(), a, a.Bounds(), draw.Over, nil)
	writePNG(t, filepath.Join(root, "sub", "a-small.png"), small) // visual, resized
	writePNG(t, filepath.Join(root, "b.png"), b)                  // exact pair, unrelated to a
	writePNG(t, filepath.Join(root, "b-copy.bin"), b)             // exact, other extension
	writePNG(t, filepath.Join(root, "c.png"), c)                  // unique
	blank := image.NewGray(image.Rect(0, 0, 64, 64))
	writePNG(t, filepath.Join(root, "blank1.png"), blank)
	blank2 := image.NewGray(image.Rect(0, 0, 90, 70))
	writePNG(t, filepath.Join(root, "blank2.png"), blank2) // flat: not perceptually matched

	out, err := Run(context.Background(), Options{Type: model.Image, Recursive: true, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	got := summarize(root, out.Groups)
	want := map[string][]string{
		"VISUAL:a-copy.png": {"a-copy.png", "a-q60.jpg", "a.png", filepath.Join("sub", "a-small.png")},
		"EXACT:b-copy.bin":  {"b-copy.bin", "b.png"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups:\n got %v\nwant %v", got, want)
	}
	for i, g := range out.Groups {
		if g.ID != i+1 {
			t.Errorf("group IDs must be 1..n in order, got %d at %d", g.ID, i)
		}
	}
	if out.Scanned != 9 {
		t.Errorf("scanned = %d, want 9", out.Scanned)
	}

	// Determinism: a second run gives identical output.
	out2, err := Run(context.Background(), Options{Type: model.Image, Recursive: true, Root: root})
	if err != nil || !reflect.DeepEqual(summarize(root, out2.Groups), got) {
		t.Fatalf("second run differs: %v %v", err, summarize(root, out2.Groups))
	}
}

func TestSourcesAreNotModified(t *testing.T) {
	root := tempRoot(t)
	p := filepath.Join(root, "a.png")
	writePNG(t, p, pic(2))
	writePNG(t, filepath.Join(root, "b.png"), pic(2))
	before, _ := os.Stat(p)
	data, _ := os.ReadFile(p)
	if _, err := Run(context.Background(), Options{Type: model.Image, Root: root}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(p)
	data2, _ := os.ReadFile(p)
	if !before.ModTime().Equal(after.ModTime()) || !bytes.Equal(data, data2) {
		t.Fatal("source file was modified")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 2 {
		t.Fatalf("scan left extra files in the source dir: %v", entries)
	}
}

func TestCancelled(t *testing.T) {
	root := tempRoot(t)
	writePNG(t, filepath.Join(root, "a.png"), pic(1))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, Options{Type: model.Image, Root: root}); err == nil {
		t.Fatal("cancelled run must return an error, not a complete result")
	}
}

func TestVideoPipeline(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	root := tempRoot(t)
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("ffmpeg", append([]string{"-v", "error", "-nostdin", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	src := "mandelbrot=s=320x240:rate=15:end_pts=90:maxiter=300:start_x=-0.7436:start_y=0.1318"
	run("-f", "lavfi", "-i", src, "-t", "6", "-pix_fmt", "yuv420p", filepath.Join(root, "orig.mp4"))
	run("-i", filepath.Join(root, "orig.mp4"), "-vf", "scale=160:120", "-crf", "34", filepath.Join(root, "re-encoded.mp4"))
	run("-i", filepath.Join(root, "orig.mp4"), "-c", "copy", filepath.Join(root, "orig-copy.mkv"))
	data, _ := os.ReadFile(filepath.Join(root, "orig.mp4"))
	mustWrite(t, filepath.Join(root, "no-extension"), data) // byte-identical, extension stripped
	run("-f", "lavfi", "-i", "mandelbrot=s=320x240:rate=15:end_pts=90:maxiter=300:start_x=-1.25:start_y=0.02",
		"-t", "6", "-pix_fmt", "yuv420p", filepath.Join(root, "different.mp4"))
	mustWrite(t, filepath.Join(root, "fake.mp4"), []byte("not a video"))
	mustWrite(t, filepath.Join(root, "pic.png"), func() []byte { var b bytes.Buffer; png.Encode(&b, pic(1)); return b.Bytes() }())

	out, err := Run(context.Background(), Options{Type: model.Video, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	got := summarize(root, out.Groups)
	want := map[string][]string{
		"VISUAL:no-extension": {"no-extension", "orig-copy.mkv", "orig.mp4", "re-encoded.mp4"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups:\n got %v\nwant %v", got, want)
	}
	if out.Scanned != 5 {
		t.Errorf("scanned = %d, want 5 real videos", out.Scanned)
	}
}

// A cancelled scan must not leave ffmpeg/ffprobe children behind.
func TestCancelKillsChildProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses ps, which Windows does not have")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	root := tempRoot(t)
	for i := range 8 {
		out, err := exec.Command("ffmpeg", "-v", "error", "-nostdin", "-y", "-f", "lavfi",
			"-i", fmt.Sprintf("mandelbrot=s=320x240:rate=15:end_pts=90:maxiter=2000:start_x=-0.74%d:start_y=0.131", i),
			"-t", "8", "-pix_fmt", "yuv420p", filepath.Join(root, fmt.Sprintf("v%d.mp4", i))).CombinedOutput()
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(60 * time.Millisecond); cancel() }()
	_, err := Run(ctx, Options{Type: model.Video, Root: root})
	if err == nil {
		t.Log("scan finished before cancellation; nothing to verify")
	}
	time.Sleep(300 * time.Millisecond)
	ps, _ := exec.Command("ps", "-axo", "command").Output()
	if bytes.Contains(ps, []byte(root)) {
		t.Fatalf("child processes still running for %s after cancel:\n%s", root, ps)
	}
}

// Fingerprinting waits for a returning drive instead of dropping the image.
func TestFingerprintingWaitsForDriveAndRetries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	root := tempRoot(t)
	var paths []string
	for i, seed := range []int{1, 1, 4} {
		p := filepath.Join(root, fmt.Sprintf("p%d.png", i))
		writePNG(t, p, pic(seed))
		paths = append(paths, p)
	}
	nodes := make([]node, len(paths))
	for i, p := range paths {
		nodes[i] = node{files: []model.File{{Path: p}}}
	}
	if err := os.Chmod(paths[1], 0o000); err != nil {
		t.Fatal(err)
	}
	var up atomic.Bool
	g := volume.NewWithProbe(root, up.Load, 5*time.Millisecond, 5*time.Millisecond)
	go func() {
		time.Sleep(80 * time.Millisecond)
		os.Chmod(paths[1], 0o600)
		up.Store(true)
	}()
	edges, failed, err := perceptualEdges(context.Background(), model.Image, nodes, 1, g, func(string, int, int) {}, &caches{open: map[string]*hashcache.Cache{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 0 {
		t.Fatalf("nothing may be dropped across a reconnect: %v", failed)
	}
	if len(edges) != 1 || edges[0].A != 0 || edges[0].B != 1 {
		t.Fatalf("the two copies of the same picture must still match: %v", edges)
	}
	if g.Outages() != 1 {
		t.Errorf("outages = %d", g.Outages())
	}
}

func TestIOWorkers(t *testing.T) {
	if n := ioWorkers("/Volumes/HD1/Movies", model.Video); n != 1 {
		t.Errorf("external video: %d readers, want 1", n)
	}
	if n := ioWorkers("/Volumes/HD1/Pictures", model.Image); n != 1 {
		t.Errorf("external images: %d readers, want 1", n)
	}
	if n := ioWorkers("/Users/me/Pictures", model.Image); n < 1 {
		t.Errorf("internal images: %d", n)
	}
	if n := ioWorkers("/Users/me/Movies", model.Video); n < 1 || n > 4 {
		t.Errorf("internal video must stay within the ffmpeg process bound, got %d", n)
	}
}

// A repeat scan of unchanged files must be answered entirely from the
// persistent caches: with every file unreadable it still finds the same groups,
// and a modified file is recomputed (and so reported unreadable) instead of trusted.
func TestRepeatScanServedFromCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}
	root := tempRoot(t)
	a := pic(2)
	writePNG(t, filepath.Join(root, "a.png"), a)
	writePNG(t, filepath.Join(root, "a-copy.png"), a)
	writeJPEG(t, filepath.Join(root, "a-q60.jpg"), a, 60)
	writePNG(t, filepath.Join(root, "other.png"), pic(8))
	run := func() Output {
		out, err := Run(context.Background(), Options{Type: model.Image, Root: root})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	want := summarize(root, run().Groups)
	if len(want) != 1 {
		t.Fatalf("first run: %v", want)
	}

	entries, _ := os.ReadDir(root)
	lock := func(mode os.FileMode) {
		for _, e := range entries {
			os.Chmod(filepath.Join(root, e.Name()), mode)
		}
	}
	lock(0)
	defer lock(0o600)
	if got := summarize(root, run().Groups); !reflect.DeepEqual(got, want) {
		t.Fatalf("cached run differs:\n got %v\nwant %v", got, want)
	}

	// Touch one file: its size/mtime no longer match, so it must be read again.
	lock(0o600)
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(root, "a-q60.jpg"), later, later); err != nil {
		t.Fatal(err)
	}
	lock(0)
	os.Chmod(filepath.Join(root, "a-q60.jpg"), 0)
	out := run()
	for _, g := range out.Groups {
		for _, f := range g.Files {
			if filepath.Base(f.Path) == "a-q60.jpg" {
				t.Errorf("modified, now unreadable file was served from cache: %v", summarize(root, out.Groups))
			}
		}
	}
}

// A copy that lives in a hidden folder is only found when hidden files are included.
func TestHiddenFilesOnlyScannedWhenAsked(t *testing.T) {
	root := tempRoot(t)
	writePNG(t, filepath.Join(root, "photo.png"), pic(1))
	writePNG(t, filepath.Join(root, ".backup", "photo-copy.png"), pic(1))
	writePNG(t, filepath.Join(root, ".hidden-copy.png"), pic(1))
	run := func(hidden bool) Output {
		out, err := Run(context.Background(), Options{Type: model.Image, Root: root, Recursive: true, Hidden: hidden})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := run(false); len(out.Groups) != 0 || out.Scanned != 1 {
		t.Errorf("hidden off: groups=%v scanned=%d, want no groups and 1 file", summarize(root, out.Groups), out.Scanned)
	}
	out := run(true)
	first := filepath.Join(".backup", "photo-copy.png")
	want := map[string][]string{"EXACT:" + first: {first, ".hidden-copy.png", "photo.png"}}
	if got := summarize(root, out.Groups); !reflect.DeepEqual(got, want) || out.Scanned != 3 {
		t.Fatalf("hidden on: groups=%v scanned=%d, want %v and 3 files", got, out.Scanned, want)
	}
}
