package scanner

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/RishiKendai/veyra/internal/classify"
	"github.com/RishiKendai/veyra/internal/model"
	"github.com/RishiKendai/veyra/internal/volume"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range 8 {
		img.Set(i, i, color.White)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, 16, 16)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func relNames(t *testing.T, root string, res Result) []string {
	t.Helper()
	root, _ = filepath.EvalSymlinks(root)
	var out []string
	for _, f := range res.Files {
		r, err := filepath.Rel(root, f.Path)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func scanImages(t *testing.T, root string, recursive bool) Result {
	t.Helper()
	res, err := Scan(context.Background(), Options{Root: root, Recursive: recursive, Classify: classify.Image, Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestImageDiscoveryIsContentBased(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "real.png"), pngBytes(t))
	write(t, filepath.Join(root, "noext"), jpegBytes(t))                // image, no extension
	write(t, filepath.Join(root, "disguised.txt"), pngBytes(t))         // image, wrong extension
	write(t, filepath.Join(root, "fake.jpg"), []byte("not an image!!")) // extension lies
	write(t, filepath.Join(root, "empty.png"), nil)
	write(t, filepath.Join(root, "trunc.png"), pngBytes(t)[:20]) // corrupt: valid magic, truncated body

	res := scanImages(t, root, false)
	got := relNames(t, root, res)
	want := []string{"disguised.txt", "noext", "real.png"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

func TestRecursion(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.png"), pngBytes(t))
	write(t, filepath.Join(root, "sub", "b.png"), pngBytes(t))
	write(t, filepath.Join(root, "sub", "deep", "c.png"), pngBytes(t))

	if got := relNames(t, root, scanImages(t, root, false)); !reflect.DeepEqual(got, []string{"a.png"}) {
		t.Errorf("non-recursive = %v", got)
	}
	want := []string{"a.png", filepath.Join("sub", "b.png"), filepath.Join("sub", "deep", "c.png")}
	if got := relNames(t, root, scanImages(t, root, true)); !reflect.DeepEqual(got, want) {
		t.Errorf("recursive = %v, want %v", got, want)
	}
}

func TestUnicodeAndSpacesInPaths(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "my photos 日本", "été (1).png"), pngBytes(t))
	got := relNames(t, root, scanImages(t, root, true))
	if len(got) != 1 || !strings.HasSuffix(got[0], "été (1).png") {
		t.Fatalf("got %v", got)
	}
}

func TestSymlinksNotFollowed(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "outside.png"), pngBytes(t))
	write(t, filepath.Join(root, "in.png"), pngBytes(t))
	if err := os.Symlink(outside, filepath.Join(root, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "outside.png"), filepath.Join(root, "linkfile.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil { // cycle
		t.Fatal(err)
	}
	got := relNames(t, root, scanImages(t, root, true))
	if !reflect.DeepEqual(got, []string{"in.png"}) {
		t.Fatalf("got %v, want only in.png", got)
	}
}

func TestRootSymlinkIsResolved(t *testing.T) {
	real := t.TempDir()
	write(t, filepath.Join(real, "a.png"), pngBytes(t))
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if got := relNames(t, real, scanImages(t, link, false)); len(got) != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestHardLinksCollapsed(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.png"), pngBytes(t))
	if err := os.Link(filepath.Join(root, "a.png"), filepath.Join(root, "b.png")); err != nil {
		t.Fatal(err)
	}
	res := scanImages(t, root, false)
	if len(res.Files) != 1 || len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "hard link") {
		t.Fatalf("files=%v skipped=%v", res.Files, res.Skipped)
	}
}

func TestUnreadableSubdirMarksPartial(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read everything")
	}
	root := t.TempDir()
	write(t, filepath.Join(root, "a.png"), pngBytes(t))
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700) })

	res := scanImages(t, root, true)
	if !res.Partial || len(res.Skipped) != 1 || len(res.Files) != 1 {
		t.Fatalf("partial=%v skipped=%v files=%d", res.Partial, res.Skipped, len(res.Files))
	}
}

func TestCancelledScanIsPartial(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.png"), pngBytes(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Scan(ctx, Options{Root: root, Classify: classify.Image, Workers: 1})
	if err == nil || !res.Partial {
		t.Fatalf("err=%v partial=%v, want cancellation reported as partial", err, res.Partial)
	}
}

func TestEmptyDirNoError(t *testing.T) {
	res := scanImages(t, t.TempDir(), true)
	if len(res.Files) != 0 || res.Partial {
		t.Fatalf("%+v", res)
	}
}

func TestVideoDiscovery(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	root := t.TempDir()
	mp4 := filepath.Join(root, "clip.mp4")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=64x64:rate=10:duration=1",
		"-pix_fmt", "yuv420p", "-y", mp4).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(mp4)
	write(t, filepath.Join(root, "noext"), data) // video, no extension
	write(t, filepath.Join(root, "fake.mp4"), []byte("definitely not a video file"))
	write(t, filepath.Join(root, "trunc.mov"), data[:64]) // valid ftyp, truncated
	write(t, filepath.Join(root, "pic.png"), pngBytes(t)) // image must not be a video

	cl, err := classify.For("video")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Scan(context.Background(), Options{Root: root, Classify: cl, Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	got := relNames(t, root, res)
	if want := []string{"clip.mp4", "noext"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v, want %v (skipped: %v)", got, want, res.Skipped)
	}
	if len(res.Skipped) != 1 || !strings.HasSuffix(res.Skipped[0].Path, "trunc.mov") {
		t.Errorf("expected truncated video to be skipped with a reason, got %v", res.Skipped)
	}
}

func TestResultRootIsSymlinkResolved(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(real, "a.png"), pngBytes(t))
	res := scanImages(t, link, false)
	want, _ := filepath.EvalSymlinks(real)
	if res.Root != want || !strings.HasPrefix(res.Files[0].Path, want+string(filepath.Separator)) {
		t.Fatalf("root=%q file=%q want prefix %q", res.Root, res.Files[0].Path, want)
	}
}

// ---- a drive that disconnects mid-scan ------------------------------------

type drive struct{ present atomic.Bool }

func (d *drive) guard(root string) *volume.Guard {
	return volume.NewWithProbe(root, d.present.Load, 5*time.Millisecond, 5*time.Millisecond)
}

func TestScanSurvivesDriveDisconnectingDuringWalk(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a", "b", "c"} {
		write(t, filepath.Join(root, d, "img.png"), pngBytes(t))
		write(t, filepath.Join(root, d, "deeper", "img2.png"), pngBytes(t))
	}
	var dr drive
	dr.present.Store(true)

	// The drive drops the moment the walk enters "b" and returns 80 ms later.
	old := readDir
	t.Cleanup(func() { readDir = old })
	var tripped atomic.Bool
	readDir = func(dir string) ([]os.DirEntry, error) {
		if filepath.Base(dir) == "b" && tripped.CompareAndSwap(false, true) {
			dr.present.Store(false)
			go func() { time.Sleep(80 * time.Millisecond); dr.present.Store(true) }()
		}
		if !dr.present.Load() {
			return nil, &fs.PathError{Op: "open", Path: dir, Err: syscall.ENXIO}
		}
		return old(dir)
	}

	g := dr.guard(root)
	res, err := Scan(context.Background(), Options{Root: root, Recursive: true, Classify: classify.Image, Workers: 2, Guard: g})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 6 {
		t.Fatalf("found %d of 6 images; skipped: %v", len(res.Files), res.Skipped)
	}
	if len(res.Skipped) != 0 || res.Partial {
		t.Fatalf("a reconnect must leave nothing skipped: skipped=%v partial=%v", res.Skipped, res.Partial)
	}
	if g.Outages() != 1 {
		t.Errorf("outages = %d, want 1", g.Outages())
	}
}

func TestWithoutAGuardTheSameDisconnectSkipsEverythingAfterIt(t *testing.T) {
	// This is the behaviour that made scans "finish soon": documented here so
	// the guard's value is visible.
	root := t.TempDir()
	for _, d := range []string{"a", "b", "c"} {
		write(t, filepath.Join(root, d, "img.png"), pngBytes(t))
	}
	old := readDir
	t.Cleanup(func() { readDir = old })
	readDir = func(dir string) ([]os.DirEntry, error) {
		if filepath.Base(dir) != filepath.Base(root) {
			return nil, &fs.PathError{Op: "open", Path: dir, Err: syscall.ENXIO}
		}
		return old(dir)
	}
	res, err := Scan(context.Background(), Options{Root: root, Recursive: true, Classify: classify.Image, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 0 || len(res.Skipped) != 3 || !res.Partial {
		t.Fatalf("files=%d skipped=%d partial=%v", len(res.Files), len(res.Skipped), res.Partial)
	}
}

func TestClassifyRetriesWhenDriveDropsMidFile(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.png"), pngBytes(t))
	write(t, filepath.Join(root, "b.png"), pngBytes(t))
	var dr drive
	dr.present.Store(true)
	var failedOnce atomic.Bool
	classifyFn := func(ctx context.Context, path string) (model.Meta, error) {
		if filepath.Base(path) == "b.png" && failedOnce.CompareAndSwap(false, true) {
			dr.present.Store(false)
			go func() { time.Sleep(60 * time.Millisecond); dr.present.Store(true) }()
			return model.Meta{}, errors.New("ffprobe: Input/output error")
		}
		return classify.Image(ctx, path)
	}
	res, err := Scan(context.Background(), Options{Root: root, Classify: classifyFn, Workers: 1, Guard: dr.guard(root)})
	if err != nil || len(res.Files) != 2 || len(res.Skipped) != 0 {
		t.Fatalf("err=%v files=%d skipped=%v", err, len(res.Files), res.Skipped)
	}
}

func TestGenuineFileErrorsOnAHealthyDriveAreStillSkippedNotWaitedOn(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "ok.png"), pngBytes(t))
	write(t, filepath.Join(root, "bad.png"), pngBytes(t)[:30]) // truncated
	var dr drive
	dr.present.Store(true)
	start := time.Now()
	res, err := Scan(context.Background(), Options{Root: root, Classify: classify.Image, Workers: 1, Guard: dr.guard(root)})
	if err != nil || len(res.Files) != 1 || len(res.Skipped) != 1 {
		t.Fatalf("err=%v files=%d skipped=%v", err, len(res.Files), res.Skipped)
	}
	if time.Since(start) > time.Second {
		t.Error("a corrupt file on a healthy drive must not make the scan wait")
	}
}

func TestCtrlCWhileWaitingForTheDriveStopsTheScan(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "sub", "a.png"), pngBytes(t))
	var dr drive // never comes back
	old := readDir
	t.Cleanup(func() { readDir = old })
	readDir = func(dir string) ([]os.DirEntry, error) {
		if filepath.Base(dir) == "sub" {
			return nil, &fs.PathError{Op: "open", Path: dir, Err: syscall.ENXIO}
		}
		return old(dir)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(60 * time.Millisecond); cancel() }()
	done := make(chan Result, 1)
	var scanErr error
	go func() {
		var r Result
		r, scanErr = Scan(ctx, Options{Root: root, Recursive: true, Classify: classify.Image, Workers: 1, Guard: dr.guard(root)})
		done <- r
	}()
	select {
	case res := <-done:
		if scanErr == nil || !res.Partial {
			t.Fatalf("err=%v partial=%v: an interrupted scan must say so", scanErr, res.Partial)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Ctrl+C did not stop the scan while it waited for the drive")
	}
}
