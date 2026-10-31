// Package prof collects cheap per-operation timings so slow stages can be
// found with measurements instead of guesses. It is inert unless the
// VEYRA_PROFILE environment variable is set.
//
//	VEYRA_PROFILE=1            print a timing table to stderr on exit
//	VEYRA_CPUPROFILE=file      write a Go CPU profile (go tool pprof file)
package prof

import (
	"fmt"
	"io"
	"os"
	"runtime/pprof"
	"sort"
	"sync"
	"time"
)

// Enabled reports whether timings are being collected.
var Enabled = os.Getenv("VEYRA_PROFILE") != ""

type stat struct {
	calls int
	total time.Duration
	max   time.Duration
	bytes int64
}

var (
	mu    sync.Mutex
	stats = map[string]*stat{}
	t0    = time.Now()
)

// Since records one call of op that started at start and moved `bytes` bytes
// (0 if unknown). Use it as: defer prof.Since("op", time.Now(), 0).
func Since(op string, start time.Time, bytes int64) {
	if !Enabled {
		return
	}
	d := time.Since(start)
	mu.Lock()
	s := stats[op]
	if s == nil {
		s = new(stat)
		stats[op] = s
	}
	s.calls++
	s.total += d
	s.max = max(s.max, d)
	s.bytes += bytes
	mu.Unlock()
}

// Start returns the current time when profiling is on; it exists so call sites
// read as `defer prof.Since("x", prof.Start(), 0)`.
func Start() time.Time { return time.Now() }

// Report prints the table, slowest total first. "total" is summed across
// goroutines, so it can exceed wall time when work runs in parallel.
func Report(w io.Writer) {
	if !Enabled {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	names := make([]string, 0, len(stats))
	for n := range stats {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return stats[names[i]].total > stats[names[j]].total })
	fmt.Fprintf(w, "\nveyra profile (wall %v)\n", time.Since(t0).Round(time.Millisecond))
	fmt.Fprintf(w, "%-26s %7s %12s %10s %10s %10s\n", "operation", "calls", "sum", "avg", "max", "MB read")
	for _, n := range names {
		s := stats[n]
		mb := ""
		if s.bytes > 0 {
			mb = fmt.Sprintf("%.0f", float64(s.bytes)/1e6)
		}
		fmt.Fprintf(w, "%-26s %7d %12v %10v %10v %10s\n", n, s.calls,
			s.total.Round(time.Millisecond), (s.total / time.Duration(s.calls)).Round(time.Millisecond),
			s.max.Round(time.Millisecond), mb)
	}
}

// StartCPUProfile begins a pprof CPU profile if VEYRA_CPUPROFILE is set and
// returns a function that stops it.
func StartCPUProfile() func() {
	path := os.Getenv("VEYRA_CPUPROFILE")
	if path == "" {
		return func() {}
	}
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "veyra: cpu profile: %v\n", err)
		return func() {}
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return func() {}
	}
	return func() { pprof.StopCPUProfile(); f.Close() }
}
