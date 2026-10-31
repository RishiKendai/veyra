// Package scanner walks a directory and returns the content-verified media
// files in it. It is strictly read-only.
package scanner

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/RishiKendai/veyra/internal/classify"
	"github.com/RishiKendai/veyra/internal/model"
	"github.com/RishiKendai/veyra/internal/volume"
)

// Progress holds counters that a UI may poll concurrently.
type Progress struct {
	Seen    atomic.Int64 // regular files examined
	Matched atomic.Int64 // files accepted as the requested media type
}

// Options configures a scan.
type Options struct {
	Root      string
	Recursive bool
	Classify  classify.Classifier
	Workers   int // classifier concurrency; <=0 means 1
	Progress  *Progress
	// Guard, if set, makes the scan wait for an external drive that
	// disconnects and carry on when it returns, instead of skipping files.
	Guard *volume.Guard
}

// readDir is a variable so tests can make directory reads fail.
var readDir = os.ReadDir

// Result is the outcome of a scan. Partial is true when the scan was
// cancelled or some directories/files could not be read.
type Result struct {
	Root    string       // the scanned root with symlinks resolved; all file paths are under it
	Files   []model.File // sorted by path, hard links collapsed
	Skipped []model.Skip // sorted by path
	Partial bool
}

// Scan walks opts.Root. On cancellation it returns the partial result
// together with ctx.Err(); Partial is then always true.
func Scan(ctx context.Context, opts Options) (Result, error) {
	var res Result
	root, err := filepath.EvalSymlinks(opts.Root)
	if err != nil {
		return res, err
	}
	res.Root = root
	workers := max(opts.Workers, 1)
	prog := opts.Progress
	if prog == nil {
		prog = new(Progress)
	}

	var (
		mu      sync.Mutex
		files   []model.File
		skipped []model.Skip
		partial bool
	)
	// skip records a file we could not process. Only I/O failures make the
	// scan partial; unsupported or corrupt media is a normal, reported skip.
	skip := func(path, reason string, ioFailure bool) {
		mu.Lock()
		skipped = append(skipped, model.Skip{Path: path, Reason: reason})
		partial = partial || ioFailure
		mu.Unlock()
	}

	work := make(chan model.File, workers*4)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range work {
				if ctx.Err() != nil {
					continue // drain
				}
				var meta model.Meta
				err := opts.Guard.Do(ctx, func() error {
					var e error
					meta, e = opts.Classify(ctx, f.Path)
					return e
				}, classify.ErrNotMedia)
				switch {
				case err == nil:
					f.Meta = meta
					prog.Matched.Add(1)
					mu.Lock()
					files = append(files, f)
					mu.Unlock()
				case errors.Is(err, classify.ErrNotMedia):
				case ctx.Err() != nil:
				default:
					skip(f.Path, err.Error(), errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist))
				}
			}
		}()
	}

	var walk func(dir string) error
	walk = func(dir string) error {
		var entries []os.DirEntry
		err := opts.Guard.Do(ctx, func() error {
			var e error
			entries, e = readDir(dir)
			return e
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if dir == root {
				return err // an unreadable root is fatal
			}
			skip(dir, err.Error(), true)
			return nil
		}
		for _, e := range entries {
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
			path := filepath.Join(dir, e.Name())
			switch {
			case e.IsDir(): // false for symlinks: they are never followed
				if opts.Recursive {
					if err := walk(path); err != nil {
						return err
					}
				}
			case e.Type().IsRegular():
				var fi fs.FileInfo
				err := opts.Guard.Do(ctx, func() error {
					var e2 error
					fi, e2 = e.Info()
					return e2
				})
				if err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					skip(path, err.Error(), true) // vanished or unreadable
					continue
				}
				prog.Seen.Add(1)
				dev, ino, nlink := devIno(fi)
				select {
				case work <- model.File{Path: path, Size: fi.Size(), ModTime: fi.ModTime(), Dev: dev, Ino: ino, Nlink: nlink}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			// sockets, devices, symlinks: never followed or read
		}
		return nil
	}
	walkErr := walk(root)
	close(work)
	wg.Wait()

	res.Files, res.Skipped = collapseHardLinks(files, skipped)
	sort.Slice(res.Skipped, func(i, j int) bool { return res.Skipped[i].Path < res.Skipped[j].Path })
	res.Partial = partial

	if err := ctx.Err(); err != nil {
		res.Partial = true
		return res, err
	}
	if walkErr != nil {
		return res, walkErr
	}
	return res, nil
}

// collapseHardLinks sorts files by path and drops later paths that share an
// inode with an earlier one: they are the same bytes on disk, not copies.
// Only files that can have a sibling (link count above 1, or unknown) enter
// the table, so it stays small.
func collapseHardLinks(files []model.File, skipped []model.Skip) ([]model.File, []model.Skip) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	type key struct{ dev, ino uint64 }
	first := make(map[key]string)
	out := files[:0]
	for _, f := range files {
		if f.Ino != 0 && f.Nlink != 1 {
			k := key{f.Dev, f.Ino}
			if p, ok := first[k]; ok {
				skipped = append(skipped, model.Skip{Path: f.Path, Reason: "hard link to " + p})
				continue
			}
			first[k] = f.Path
		}
		out = append(out, f)
	}
	return out, skipped
}
