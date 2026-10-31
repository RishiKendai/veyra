// Package exact finds byte-identical files with a circuit-breaker cascade
// that reads as little as possible. Each stage only discards files that
// cannot match; a match is declared by the full-content BLAKE3 alone.
//
//  1. size: a unique size cannot have a duplicate (no read at all)
//  2. prehash: BLAKE3 of head + middle + tail samples (cached)
//  3. full hash: streaming BLAKE3 of the whole file (cached)
package exact

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/zeebo/blake3"

	"github.com/RishiKendai/veyra/internal/hashcache"
	"github.com/RishiKendai/veyra/internal/model"
	"github.com/RishiKendai/veyra/internal/prof"
	"github.com/RishiKendai/veyra/internal/progress"
	"github.com/RishiKendai/veyra/internal/volume"
	"github.com/RishiKendai/veyra/internal/workpool"
)

const (
	// PrehashBufferSize is the size of each of the three prehash samples.
	PrehashBufferSize = 4 << 10
	// ThreadBufferSize is the streaming read buffer owned by each worker.
	// Memory used by hashing is therefore at most workers * ThreadBufferSize.
	ThreadBufferSize = 2 << 20

	// Files smaller than this are cheaper to hash whole than to prehash.
	prehashMin = 4 * PrehashBufferSize

	// CacheParams identify the hash and sampling layout; changing either
	// invalidates persisted entries.
	PrehashCacheParams = "blake3;prehash=head+mid+tail;sample=4096"
	FullCacheParams    = "blake3;full"
)

// Group is a set of two or more byte-identical files.
type Group struct {
	Hash  [32]byte
	Files []model.File // sorted by path
}

// Result holds groups sorted by first path, plus files that could not be read.
type Result struct {
	Groups  []Group
	Skipped []model.Skip
}

var bufPool = sync.Pool{New: func() any { b := make([]byte, ThreadBufferSize); return &b }}

// Options tunes how files are read.
type Options struct {
	Progress func(stage string, done, total int) // optional; progress.Compare then progress.Hash
	Guard    *volume.Guard                       // optional; waits out a disconnecting external drive
	Workers  int                                 // upper bound on concurrent readers; <=0 means 1
	NoCache  bool                                // bypass the OS page cache while hashing (external disks): a one-pass read gains nothing from caching
	Prehash  *hashcache.Cache                    // optional persistent prehash cache
	Full     *hashcache.Cache                    // optional persistent full-hash cache
}

// Find returns exact-duplicate groups. Caches are flushed before it returns,
// including on cancellation.
func Find(ctx context.Context, files []model.File, o Options) (Result, error) {
	defer o.Prehash.Flush() // best effort: a cache only saves work
	defer o.Full.Flush()

	var res Result
	report := o.Progress
	if report == nil {
		report = func(string, int, int) {}
	}

	// Path order makes output deterministic and disk access sequential.
	if !slices.IsSortedFunc(files, byPath) {
		files = slices.Clone(files)
		slices.SortFunc(files, byPath)
	}

	// Stage 1: identical content implies identical size. Empty files carry no media.
	nonEmpty := make([]model.File, 0, len(files))
	for _, f := range files {
		if f.Size > 0 {
			nonEmpty = append(nonEmpty, f)
		}
	}
	cands := pick(nonEmpty, groupBy(len(nonEmpty),
		func(i int) uint64 { return mix(uint64(nonEmpty[i].Size)) },
		func(a, b int) bool { return nonEmpty[a].Size == nonEmpty[b].Size }))

	// Stage 2: prehash the size collisions that are big enough to sample.
	var small, large []model.File
	for _, f := range cands {
		if f.Size < prehashMin {
			small = append(small, f)
		} else {
			large = append(large, f)
		}
	}
	hs, failed, err := hashStage(ctx, large, o.Prehash, o, progress.Compare, report, prehash)
	res.Skipped = append(res.Skipped, failed...)
	if err != nil {
		return res, err
	}
	survivors := pick(large, groupBy(len(large),
		func(i int) uint64 { return digestKey(hs[i]) },
		func(a, b int) bool { return large[a].Size == large[b].Size && hs[a] == hs[b] }))
	// A failed prehash leaves the zero digest, which would group failures together; drop them.
	badPre := paths(failed)
	survivors = slices.DeleteFunc(survivors, func(f model.File) bool { return badPre[f.Path] })
	toFull := append(small, survivors...)
	slices.SortFunc(toFull, byPath)

	// Stage 3: full streaming hash on what is left.
	fh, failed, err := hashStage(ctx, toFull, o.Full, o, progress.Hash, report, func(f model.File, _ bool) ([32]byte, error) {
		return fullHash(f.Path, o.NoCache)
	})
	res.Skipped = append(res.Skipped, failed...)
	if err != nil {
		return res, err
	}
	bad := paths(failed)
	keep := make([]int, 0, len(toFull)) // indexes of files with a valid full hash
	for i, f := range toFull {
		if !bad[f.Path] {
			keep = append(keep, i)
		}
	}
	for _, g := range groupBy(len(keep),
		func(i int) uint64 { return digestKey(fh[keep[i]]) },
		func(a, b int) bool { return fh[keep[a]] == fh[keep[b]] && toFull[keep[a]].Size == toFull[keep[b]].Size }) {
		grp := Group{Hash: fh[keep[g[0]]], Files: make([]model.File, len(g))}
		for k, m := range g {
			grp.Files[k] = toFull[keep[m]]
		}
		res.Groups = append(res.Groups, grp) // buckets arrive ordered by first path
	}
	slices.SortFunc(res.Skipped, func(a, b model.Skip) int { return strings.Compare(a.Path, b.Path) })
	return res, nil
}

func byPath(a, b model.File) int { return strings.Compare(a.Path, b.Path) }

// pick returns the members of every bucket, in path order.
func pick(files []model.File, buckets [][]int32) []model.File {
	var idx []int32
	for _, b := range buckets {
		idx = append(idx, b...)
	}
	slices.Sort(idx)
	out := make([]model.File, len(idx))
	for k, i := range idx {
		out[k] = files[i]
	}
	return out
}

func paths(skips []model.Skip) map[string]bool {
	m := make(map[string]bool, len(skips))
	for _, s := range skips {
		m[s.Path] = true
	}
	return m
}

// digestKey takes 8 bytes of a digest as its table hash; BLAKE3 output is
// already uniform, so no further mixing is needed.
func digestKey(d [32]byte) uint64 { return binary.LittleEndian.Uint64(d[:8]) }

// hashStage fills one digest per file. A single-threaded pass answers from the
// cache first; only misses go to a fixed pool of workers, so neither cached
// files nor idle goroutines cost anything. Digests come back in file order.
func hashStage(ctx context.Context, files []model.File, cache *hashcache.Cache, o Options, stage string,
	report func(string, int, int), compute func(model.File, bool) ([32]byte, error)) ([][32]byte, []model.Skip, error) {

	out := make([][32]byte, len(files))
	var misses []int
	for i, f := range files {
		if h, ok := cache.Get(f.Path, f.Size, f.ModTime); ok {
			out[i] = h
		} else {
			misses = append(misses, i)
		}
	}
	total := len(files)
	var mu sync.Mutex // guards failed and report
	var failed []model.Skip
	done := total - len(misses)
	if done > 0 {
		report(stage, done, total)
	}
	var hashed atomic.Int64
	err := workpool.ForEach(ctx, o.Workers, misses, func(i int) {
		f := files[i]
		var h [32]byte
		err := o.Guard.Do(ctx, func() error {
			var e error
			h, e = compute(f, o.NoCache)
			return e
		})
		if err == nil {
			out[i] = h // distinct index per worker: no race
			cache.Put(f.Path, f.Size, f.ModTime, h)
			hashed.Add(1)
		}
		mu.Lock()
		defer mu.Unlock()
		done++
		report(stage, done, total)
		if err != nil {
			failed = append(failed, model.Skip{Path: f.Path, Reason: err.Error()})
		}
	})
	return out, failed, err
}

// prehash hashes three PrehashBufferSize samples: the head, the middle and
// the tail. Callers guarantee f.Size >= prehashMin, so samples never overlap.
func prehash(f model.File, _ bool) ([32]byte, error) {
	defer prof.Since("exact.prehash(head+mid+tail)", prof.Start(), 3*PrehashBufferSize)
	var buf [3 * PrehashBufferSize]byte
	fd, err := os.Open(f.Path)
	if err != nil {
		return [32]byte{}, err
	}
	defer fd.Close()
	offs := [3]int64{0, f.Size/2 - PrehashBufferSize/2, f.Size - PrehashBufferSize}
	for k, off := range offs {
		if _, err := fd.ReadAt(buf[k*PrehashBufferSize:(k+1)*PrehashBufferSize], off); err != nil {
			return [32]byte{}, err // includes a file that shrank since the scan
		}
	}
	return blake3.Sum256(buf[:]), nil
}

func fullHash(path string, noCache bool) ([32]byte, error) {
	var n int64
	start := prof.Start()
	defer func() { prof.Since("exact.full(blake3)", start, n) }()
	var sum [32]byte
	fd, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	defer fd.Close()
	if noCache {
		setNoCache(fd) // best effort: failure only costs cache pollution
	}
	bp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bp)
	h := blake3.New()
	// Hide WriterTo so io.CopyBuffer really uses our pooled buffer.
	n, err = io.CopyBuffer(h, struct{ io.Reader }{fd}, *bp)
	if err != nil {
		return sum, err
	}
	h.Sum(sum[:0])
	return sum, nil
}
