// Package app runs the whole pipeline: scan, exact matching, perceptual
// fingerprinting, matching and clustering. It has no UI dependencies.
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RishiKendai/veyra/internal/classify"
	"github.com/RishiKendai/veyra/internal/cluster"
	"github.com/RishiKendai/veyra/internal/exact"
	"github.com/RishiKendai/veyra/internal/hashcache"
	"github.com/RishiKendai/veyra/internal/imgfp"
	"github.com/RishiKendai/veyra/internal/model"
	"github.com/RishiKendai/veyra/internal/prof"
	"github.com/RishiKendai/veyra/internal/progress"
	"github.com/RishiKendai/veyra/internal/result"
	"github.com/RishiKendai/veyra/internal/scanner"
	"github.com/RishiKendai/veyra/internal/similarity"
	"github.com/RishiKendai/veyra/internal/vidfp"
	"github.com/RishiKendai/veyra/internal/volume"
	"github.com/RishiKendai/veyra/internal/workpool"
)

// Options configures a run.
type Options struct {
	Type      model.MediaType
	Recursive bool
	Root      string
	Progress  *scanner.Progress                   // optional, polled during the scan stage
	Report    func(stage string, done, total int) // optional; stages are the progress.* names; called from worker goroutines
	// OnVolume, if set, is told when an external drive disconnects (down) and
	// when it is back. It is called from a separate goroutine.
	OnVolume func(down bool)
}

// Output is the complete outcome of a run.
type Output struct {
	Root     string // scanned root with symlinks resolved; file paths are under it
	Groups   []result.Group
	Examined int           // regular files looked at, media or not
	Scanned  int           // media files found
	Elapsed  time.Duration // wall time for the whole run
	Outages  int           // times the external drive disconnected and the scan waited for it
	Skipped  []model.Skip
	Partial  bool // some paths could not be read
}

// node is one unit of perceptual comparison: an exact-duplicate set is
// represented by a single member so identical bytes are decoded only once.
type node struct{ files []model.File }

// Run executes the pipeline. On cancellation it returns ctx.Err() and no groups.
func Run(ctx context.Context, o Options) (Output, error) {
	var out Output
	started := time.Now()
	defer func() { out.Elapsed = time.Since(started) }()
	prog := o.Progress
	if prog == nil {
		prog = new(scanner.Progress)
	}
	guard := volume.New(o.Root) // nil for the internal disk
	if guard != nil {
		guard.OnChange = o.OnVolume
	}
	defer func() { out.Outages = guard.Outages() }()
	report := o.Report
	if report == nil {
		report = func(string, int, int) {}
	}

	cl, err := classify.For(o.Type)
	if err != nil {
		return out, err
	}
	cc := newCaches()
	defer cc.flush() // also on cancel and error: finished work is kept
	cl = cachedClassifier(cl, cc.get("classify-"+string(o.Type)+".cache", classifyCacheParams))
	scanStart := prof.Start()
	res, err := scanner.Scan(ctx, scanner.Options{
		Root: o.Root, Recursive: o.Recursive, Classify: cl,
		Workers: ioWorkers(o.Root, o.Type), Progress: prog, Guard: guard,
	})
	prof.Since("STAGE scan+classify", scanStart, 0)
	out.Root, out.Skipped, out.Partial, out.Scanned = res.Root, res.Skipped, res.Partial, len(res.Files)
	out.Examined = int(prog.Seen.Load())
	if err != nil {
		return out, err
	}

	exactStart := prof.Start()
	eo := exact.Options{Workers: ioWorkers(o.Root, o.Type), NoCache: isExternal(o.Root), Progress: report, Guard: guard}
	eo.Prehash = cc.get("prehash.cache", exact.PrehashCacheParams)
	eo.Full = cc.get("fullhash.cache", exact.FullCacheParams)
	ex, err := exact.Find(ctx, res.Files, eo)
	prof.Since("STAGE exact", exactStart, 0)
	out.Skipped = append(out.Skipped, ex.Skipped...)
	if err != nil {
		return out, err
	}

	// Nodes: one per exact group, one per remaining file.
	inExact := make(map[string]bool)
	var nodes []node
	for _, g := range ex.Groups {
		nodes = append(nodes, node{files: g.Files})
		for _, f := range g.Files {
			inExact[f.Path] = true
		}
	}
	for _, f := range res.Files {
		if !inExact[f.Path] {
			nodes = append(nodes, node{files: []model.File{f}})
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].files[0].Path < nodes[j].files[0].Path })

	percStart := prof.Start()
	edges, failed, err := perceptualEdges(ctx, o.Type, nodes, ioWorkers(o.Root, o.Type), guard, report, cc)
	prof.Since("STAGE perceptual (all)", percStart, 0)
	out.Skipped = append(out.Skipped, failed...)
	if err != nil {
		return out, err
	}

	// Visual clusters, expanded back to every file of every member node.
	used := make([]bool, len(nodes))
	var groups []result.Group
	for _, members := range cluster.CompleteLinkage(len(nodes), edges) {
		var files []model.File
		in := make(map[int]bool, len(members))
		for _, n := range members {
			used[n] = true
			in[n] = true
			files = append(files, nodes[n].files...)
		}
		worst := 0
		for _, e := range edges {
			if in[e.A] && in[e.B] {
				worst = max(worst, e.Dist)
			}
		}
		groups = append(groups, result.Group{Type: o.Type, Class: result.Visual, Files: files, WorstDist: worst})
	}
	for n, nd := range nodes {
		if !used[n] && len(nd.files) >= 2 {
			groups = append(groups, result.Group{Type: o.Type, Class: result.Exact, Files: nd.files})
		}
	}
	for i := range groups {
		fs := groups[i].Files
		sort.Slice(fs, func(a, b int) bool { return fs[a].Path < fs[b].Path })
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Files[0].Path < groups[j].Files[0].Path })
	for i := range groups {
		groups[i].ID = i + 1
		groups[i].Analyze()
	}
	out.Groups = groups
	sort.Slice(out.Skipped, func(i, j int) bool { return out.Skipped[i].Path < out.Skipped[j].Path })
	return out, nil
}

// caches opens each persistent cache the first time a stage needs it, so a
// stage with nothing to do never reads a cache file. It is used from the Run
// goroutine only; the caches themselves are safe for concurrent use.
type caches struct {
	dir  string // empty disables persistence
	open map[string]*hashcache.Cache
}

func newCaches() *caches {
	dir, err := hashcache.DefaultDir()
	if err != nil {
		dir = "" // no user cache dir: run without caching
	}
	return &caches{dir: dir, open: make(map[string]*hashcache.Cache)}
}

// get returns the named cache, or nil (which caches nothing) when persistence is off.
func (c *caches) get(name, params string) *hashcache.Cache {
	if c.dir == "" {
		return nil
	}
	if h, ok := c.open[name]; ok {
		return h
	}
	h := hashcache.Open(c.dir, name, params)
	c.open[name] = h
	return h
}

// flush writes every opened cache. Best effort: a cache only saves work.
func (c *caches) flush() {
	for _, h := range c.open {
		_ = h.Flush()
	}
}

// maxInMemoryImage is the largest image read whole into memory for decoding;
// larger files are streamed under the read slot instead. It bounds memory to
// roughly NumCPU x this value.
const maxInMemoryImage = 32 << 20

// classifyCacheParams identifies the model.Meta layout stored by cachedClassifier.
const classifyCacheParams = "classify1;json"

// cachedClassifier serves metadata from c while the file's size and mtime are
// unchanged. Failures are never cached, so a transient ffprobe error cannot
// hide a file.
func cachedClassifier(cl classify.Classifier, c *hashcache.Cache) classify.Classifier {
	if c == nil {
		return cl
	}
	return func(ctx context.Context, path string) (model.Meta, error) {
		fi, serr := os.Stat(path)
		if serr == nil {
			if b, ok := c.GetData(path, fi.Size(), fi.ModTime()); ok {
				var m model.Meta
				if json.Unmarshal(b, &m) == nil {
					return m, nil
				}
			}
		}
		m, err := cl(ctx, path)
		if err == nil && serr == nil {
			if b, merr := json.Marshal(m); merr == nil {
				c.PutData(path, fi.Size(), fi.ModTime(), b)
			}
		}
		return m, err
	}
}

// perceptualEdges fingerprints each node's first file and returns verified
// matches between nodes. Nodes that fail to fingerprint are reported and
// excluded; they can still be part of an EXACT group. Results come from the
// persistent caches when the file is unchanged; only misses are computed.
func perceptualEdges(ctx context.Context, t model.MediaType, nodes []node, workers int, guard *volume.Guard, report func(string, int, int), cc *caches) ([]similarity.Edge, []model.Skip, error) {
	if len(nodes) < 2 {
		return nil, nil, nil // nothing to compare
	}
	var (
		mu     sync.Mutex
		failed []model.Skip
		skip   = make([]bool, len(nodes))
		done   int
		total  int
	)
	fail := func(i int, err error) {
		if ctx.Err() != nil {
			return // cancelled work is not a skipped file
		}
		mu.Lock()
		skip[i] = true
		failed = append(failed, model.Skip{Path: nodes[i].files[0].Path, Reason: err.Error()})
		mu.Unlock()
	}
	tick := func(stage string) {
		mu.Lock()
		done++
		d := done
		mu.Unlock()
		report(stage, d, total)
	}
	// start resets the counter for a stage whose first `hits` items came from the cache.
	start := func(stage string, hits, n int) {
		done, total = hits, n
		if hits > 0 {
			report(stage, hits, n)
		}
	}

	switch t {
	case model.Image:
		ic := cc.get("imgfp.cache", imgfp.CodecVersion)
		hs := make([]imgfp.Hashes, len(nodes))
		var todo []int
		for i := range nodes {
			f := nodes[i].files[0]
			if b, ok := ic.GetData(f.Path, f.Size, f.ModTime); ok {
				if h, ok := imgfp.DecodeHashes(b); ok {
					hs[i] = h
					continue
				}
			}
			todo = append(todo, i)
		}
		start(progress.Fingerprint, len(nodes)-len(todo), len(nodes))
		// Disk reads are limited to `workers` at a time (1 on an external drive,
		// where concurrent readers make a hard disk seek), but the decode that
		// follows is CPU work from memory and uses every core.
		slots := make(chan struct{}, max(workers, 1))
		err := workpool.ForEach(ctx, runtime.NumCPU(), todo, func(i int) {
			f := nodes[i].files[0]
			var h imgfp.Hashes
			var data []byte
			big := f.Size > maxInMemoryImage
			err := guard.Do(ctx, func() error {
				select {
				case slots <- struct{}{}:
					defer func() { <-slots }()
				case <-ctx.Done():
					return ctx.Err()
				}
				var e error
				if big {
					h, e = imgfp.Compute(f.Path) // too large to hold: stream it, decoding under the read slot
				} else {
					data, e = os.ReadFile(f.Path)
				}
				return e
			})
			if err == nil && !big {
				h, err = imgfp.ComputeReader(bytes.NewReader(data))
			}
			if err != nil {
				fail(i, err)
			} else {
				hs[i] = h
				ic.PutData(f.Path, f.Size, f.ModTime, h.Encode())
			}
			tick(progress.Fingerprint)
		})
		if err != nil {
			return nil, failed, err
		}
		report(progress.Group, 0, 0)
		return similarity.ImageEdges(hs, skip), failed, nil

	case model.Video:
		durations := make([]float64, len(nodes))
		for i := range nodes {
			durations[i] = nodes[i].files[0].Duration
		}

		// Step 0: a video with no other video within the matcher's duration
		// window can never match, so its frames are not decoded. Filtered nodes
		// keep a zero FP, which VideoEdges treats as ineligible.
		need := similarity.NeedsFingerprint(durations, nil)
		fps := make([]vidfp.FP, len(nodes))
		var vc *hashcache.Cache
		var todo []int
		cands := 0
		for i := range nodes {
			if !need[i] {
				continue
			}
			cands++
			if vc == nil {
				vc = cc.get("vidfp.cache", vidfp.CodecVersion)
			}
			f := nodes[i].files[0]
			if b, ok := vc.GetData(f.Path, f.Size, f.ModTime); ok {
				if fp, ok := vidfp.DecodeFP(b); ok && fp.HasProbe() {
					fps[i] = fp
					continue
				}
			}
			todo = append(todo, i)
		}
		if prof.Enabled {
			fmt.Fprintf(os.Stderr, "veyra: profile: %d videos, %d in a duration window, %d probes not cached\n", len(nodes), cands, len(todo))
		}
		tier1Start := prof.Start()

		// Step 1: probe. A few middle frames rule most unrelated pairs out
		// (similarity.ProbeMayMatch is a necessary condition for a match).
		start(progress.Fingerprint, cands-len(todo), cands)
		err := workpool.ForEach(ctx, workers, todo, func(i int) {
			f := nodes[i].files[0]
			var fp vidfp.FP
			err := guard.Do(ctx, func() error {
				var e error
				fp, e = vidfp.ComputeProbe(ctx, f.Path, f.Duration)
				return e
			})
			if err != nil {
				fail(i, err)
			} else {
				fps[i] = fp
				vc.PutData(f.Path, f.Size, f.ModTime, fp.Encode())
			}
			tick(progress.Fingerprint)
		})
		if err != nil {
			return nil, failed, err
		}

		// Step 2: only videos that still have a possible partner get every frame.
		has := make([]bool, len(nodes))
		for i := range nodes {
			has[i] = need[i] && !skip[i] && fps[i].HasProbe()
		}
		surv := similarity.ProbeSurvivors(fps, has)
		var rest []int
		nsurv := 0
		for i := range nodes {
			if !surv[i] {
				continue
			}
			nsurv++
			if !fps[i].Complete() {
				rest = append(rest, i)
			}
		}
		if prof.Enabled {
			fmt.Fprintf(os.Stderr, "veyra: profile: %d of %d probed videos have a possible partner, %d need the remaining frames\n", nsurv, cands, len(rest))
		}
		start(progress.Fingerprint, nsurv-len(rest), nsurv)
		err = workpool.ForEach(ctx, workers, rest, func(i int) {
			f := nodes[i].files[0]
			var fp vidfp.FP
			err := guard.Do(ctx, func() error {
				var e error
				fp, e = vidfp.Complete(ctx, f.Path, fps[i])
				return e
			})
			if err != nil {
				fail(i, err)
			} else {
				fps[i] = fp
				vc.PutData(f.Path, f.Size, f.ModTime, fp.Encode())
			}
			tick(progress.Fingerprint)
		})
		if err != nil {
			return nil, failed, err
		}
		prof.Since("STAGE tier1 fingerprints", tier1Start, 0)
		report(progress.Group, 0, 0)
		return similarity.VideoEdges(fps, skip), failed, nil
	}
	return nil, nil, fmt.Errorf("unsupported media type %q", t)
}

// ioWorkers is how many files are read at once. An external drive gets one
// reader: several readers at different positions make a hard disk seek
// constantly, which is slower overall and, on a bus-powered USB drive, can
// drop the connection. Internal disks run in parallel.
func ioWorkers(root string, t model.MediaType) int {
	if isExternal(root) {
		return 1
	}
	if t == model.Video {
		return max(1, min(4, runtime.NumCPU()/2)) // bounds concurrent ffprobe/ffmpeg processes
	}
	return runtime.NumCPU()
}

// isExternal reports whether root is on a mounted volume rather than the
// internal disk. Such volumes may be spinning disks.
func isExternal(root string) bool { return strings.HasPrefix(root, "/Volumes/") }
