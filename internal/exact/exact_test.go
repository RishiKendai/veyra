package exact

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RishiKendai/veyra/internal/hashcache"
	"github.com/RishiKendai/veyra/internal/model"
	"github.com/RishiKendai/veyra/internal/volume"
)

func mk(t *testing.T, root, rel string, data []byte) model.File {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return model.File{Path: p, Size: int64(len(data))}
}

func names(root string, g Group) []string {
	var out []string
	for _, f := range g.Files {
		r, _ := filepath.Rel(root, f.Path)
		out = append(out, r)
	}
	return out
}

func TestFind(t *testing.T) {
	root := t.TempDir()
	same := bytes.Repeat([]byte("abc"), 5000)
	// Same size and identical head+tail, differing only in the middle: must NOT match.
	a := bytes.Repeat([]byte{1}, 3<<20)
	b := bytes.Clone(a)
	b[len(b)/2] = 2

	files := []model.File{
		mk(t, root, "one.mp4", same),
		mk(t, root, "two.jpg", same),               // different extension
		mk(t, root, "sub/deep/three", same),        // nested, no extension
		mk(t, root, "x/name.png", []byte("first")), // same name, different bytes
		mk(t, root, "y/name.png", []byte("other")),
		mk(t, root, "same-size-1", []byte("12345")), // same size, different bytes
		mk(t, root, "same-size-2", []byte("12346")),
		mk(t, root, "big-a", a),
		mk(t, root, "big-b", b),
		mk(t, root, "big-c", a),
		mk(t, root, "unique", []byte("only one of these")),
	}
	res, err := Find(context.Background(), files, Options{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	var got [][]string
	for _, g := range res.Groups {
		got = append(got, names(root, g))
	}
	want := [][]string{
		{"big-a", "big-c"},
		{"one.mp4", filepath.Join("sub", "deep", "three"), "two.jpg"},
	}
	// sort expectations the same way Find does (by path of first file within group)
	want[1] = []string{"one.mp4", "sub/deep/three", "two.jpg"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups = %v\nwant     %v", got, want)
	}
}

func TestUnreadableFileIsSkippedNotFatal(t *testing.T) {
	root := t.TempDir()
	a := mk(t, root, "a", []byte("hello world"))
	b := mk(t, root, "b", []byte("hello world"))
	c := mk(t, root, "c", []byte("hello world"))
	if err := os.Remove(c.Path); err != nil { // vanished after discovery
		t.Fatal(err)
	}
	res, err := Find(context.Background(), []model.File{a, b, c}, Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 || len(res.Groups[0].Files) != 2 || len(res.Skipped) != 1 {
		t.Fatalf("%+v", res)
	}
}

func TestCancelled(t *testing.T) {
	root := t.TempDir()
	f := []model.File{mk(t, root, "a", []byte("x")), mk(t, root, "b", []byte("x"))}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Find(ctx, f, Options{Workers: 1}); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestNoCacheGivesSameAnswer(t *testing.T) {
	root := t.TempDir()
	data := bytes.Repeat([]byte("z"), 3<<20)
	files := []model.File{mk(t, root, "a", data), mk(t, root, "b", data), mk(t, root, "c", append(bytes.Clone(data[:len(data)-1]), 'q'))}
	for _, nc := range []bool{false, true} {
		res, err := Find(context.Background(), files, Options{Workers: 2, NoCache: nc})
		if err != nil || len(res.Groups) != 1 || len(res.Groups[0].Files) != 2 {
			t.Fatalf("noCache=%v: %+v %v", nc, res, err)
		}
	}
}

// A file that cannot be read while the "drive" is down is retried, not skipped.
func TestHashingWaitsForDriveAndRetries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	root := t.TempDir()
	a := mk(t, root, "a", bytes.Repeat([]byte("x"), 50_000))
	b := mk(t, root, "b", bytes.Repeat([]byte("x"), 50_000))
	if err := os.Chmod(b.Path, 0o000); err != nil { // unreadable: stands in for a vanished drive
		t.Fatal(err)
	}
	var up atomic.Bool
	g := volume.NewWithProbe(root, up.Load, 5*time.Millisecond, 5*time.Millisecond)
	go func() {
		time.Sleep(80 * time.Millisecond)
		os.Chmod(b.Path, 0o600)
		up.Store(true)
	}()
	res, err := Find(context.Background(), []model.File{a, b}, Options{Workers: 1, Guard: g})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 || len(res.Skipped) != 0 {
		t.Fatalf("groups=%d skipped=%v: the duplicate must be found after the drive returns", len(res.Groups), res.Skipped)
	}
	if g.Outages() != 1 {
		t.Errorf("outages = %d", g.Outages())
	}
}

func TestGroupBy(t *testing.T) {
	keys := []int{5, 1, 5, 2, 1, 5, 9}
	// Constant hash forces every item into one probe chain: equality must still decide.
	for name, h := range map[string]func(int) uint64{
		"constant": func(int) uint64 { return 0 },
		"mixed":    func(i int) uint64 { return mix(uint64(keys[i])) },
	} {
		got := groupBy(len(keys), h, func(a, b int) bool { return keys[a] == keys[b] })
		want := [][]int32{{0, 2, 5}, {1, 4}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v want %v", name, got, want)
		}
	}
	if groupBy(1, nil, nil) != nil || groupBy(0, nil, nil) != nil {
		t.Error("fewer than two items cannot group")
	}
}

func TestMiddleDifferenceCaughtByPrehash(t *testing.T) {
	root := t.TempDir()
	a := bytes.Repeat([]byte{7}, 200<<10)
	b := bytes.Clone(a)
	b[len(b)/2+10] = 1 // inside the middle sample
	files := []model.File{mk(t, root, "a", a), mk(t, root, "b", b)}
	pre := hashcache.Open(t.TempDir(), "p", "x")
	res, err := Find(context.Background(), files, Options{Workers: 2, Prehash: pre})
	if err != nil || len(res.Groups) != 0 {
		t.Fatalf("err=%v groups=%v", err, res.Groups)
	}
	if pre.Len() != 2 {
		t.Errorf("both files should be prehash-cached, got %d", pre.Len())
	}
}

func TestCacheUsedAndInvalidated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}
	root := t.TempDir()
	data := bytes.Repeat([]byte("xyz"), 100<<10)
	files := []model.File{mk(t, root, "a", data), mk(t, root, "b", data)}
	for i := range files {
		fi, _ := os.Stat(files[i].Path)
		files[i].ModTime = fi.ModTime()
	}
	dir := t.TempDir()
	run := func() (Result, *hashcache.Cache) {
		full := hashcache.Open(dir, "f", FullCacheParams)
		res, err := Find(context.Background(), files, Options{Workers: 2, Full: full,
			Prehash: hashcache.Open(dir, "p", PrehashCacheParams)})
		if err != nil {
			t.Fatal(err)
		}
		return res, full
	}
	res, _ := run()
	if len(res.Groups) != 1 {
		t.Fatalf("first run: %v", res.Groups)
	}
	// Second run must be served from the cache: make the files unreadable
	// and it still finds the group.
	for _, f := range files {
		os.Chmod(f.Path, 0)
	}
	res, _ = run()
	for _, f := range files {
		os.Chmod(f.Path, 0o600)
	}
	if len(res.Groups) != 1 || len(res.Skipped) != 0 {
		t.Fatalf("cached run read the files: %+v", res)
	}
	// A changed mtime invalidates the entry, so the (now unreadable) file is read and skipped.
	files[0].ModTime = files[0].ModTime.Add(time.Second)
	os.Chmod(files[0].Path, 0)
	res, _ = run()
	os.Chmod(files[0].Path, 0o600)
	if len(res.Groups) != 0 || len(res.Skipped) == 0 {
		t.Fatalf("stale entry was trusted: %+v", res)
	}
}

// 10k files: unique sizes must cost nothing beyond the size index (no prehash,
// no full hash, nothing cached), and only the few size collisions are read.
func TestTenThousandFilesReadAlmostNothing(t *testing.T) {
	root := t.TempDir()
	const n = 10000
	files := make([]model.File, 0, n+6)
	for i := range n {
		data := bytes.Repeat([]byte{byte(i)}, 20000+i) // every size distinct
		p := filepath.Join(root, "u", fmt.Sprintf("%05d.bin", i))
		files = append(files, mk(t, root, filepath.Join("u", filepath.Base(p)), data))
	}
	dup := bytes.Repeat([]byte("dup"), 30000) // size 90000 collides with nothing above
	files = append(files, mk(t, root, "d1", dup), mk(t, root, "d2", dup), mk(t, root, "d3", dup))
	pre, full := hashcache.Open(t.TempDir(), "p", "x"), hashcache.Open(t.TempDir(), "f", "x")

	start := time.Now()
	res, err := Find(context.Background(), files, Options{Workers: 4, Prehash: pre, Full: full})
	t.Logf("10k files: %v", time.Since(start))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 || len(res.Groups[0].Files) != 3 {
		t.Fatalf("groups = %+v", res.Groups)
	}
	if pre.Len() != 3 || full.Len() != 3 {
		t.Errorf("only the 3 colliding files may be hashed: prehash=%d full=%d", pre.Len(), full.Len())
	}
}
