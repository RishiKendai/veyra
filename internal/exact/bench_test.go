package exact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/RishiKendai/veyra/internal/model"
)

// corpus builds n same-size files (the worst case for a size pre-filter),
// of which `dups` pairs are identical. Every file differs somewhere in the
// middle, so head+tail hashes collide for all of them.
func corpus(b *testing.B, n, size, dups int) []model.File {
	b.Helper()
	dir := b.TempDir()
	files := make([]model.File, 0, n)
	for i := range n {
		data := bytes.Repeat([]byte{0xAB}, size)
		id := i
		if i < dups*2 {
			id = i / 2 // pairs share content
		} else {
			id = 1_000_000 + i
		}
		copy(data[size/2:], fmt.Sprintf("%016d", id))
		p := filepath.Join(dir, fmt.Sprintf("f%05d", i))
		if err := os.WriteFile(p, data, 0o600); err != nil {
			b.Fatal(err)
		}
		files = append(files, model.File{Path: p, Size: int64(size)})
	}
	return files
}

func naive(files []model.File) int {
	groups := map[[32]byte]int{}
	buf := make([]byte, 1<<20)
	for _, f := range files {
		fd, _ := os.Open(f.Path)
		h := sha256.New()
		io.CopyBuffer(h, struct{ io.Reader }{fd}, buf)
		fd.Close()
		var s [32]byte
		copy(s[:], h.Sum(nil))
		groups[s]++
	}
	return len(groups)
}

// 400 x 512 KiB files, all the same size, 10 duplicate pairs.
func BenchmarkExactCascade(b *testing.B) {
	files := corpus(b, 400, 512<<10, 10)
	b.SetBytes(int64(len(files)) * (512 << 10)) // bytes "covered"; real reads are far fewer
	b.ResetTimer()
	for range b.N {
		res, err := Find(context.Background(), files, Options{Workers: 4})
		if err != nil || len(res.Groups) != 10 {
			b.Fatalf("groups=%d err=%v", len(res.Groups), err)
		}
	}
}

func BenchmarkExactNaiveFullHashEverything(b *testing.B) {
	files := corpus(b, 400, 512<<10, 10)
	b.SetBytes(int64(len(files)) * (512 << 10))
	b.ResetTimer()
	for range b.N {
		naive(files)
	}
}
