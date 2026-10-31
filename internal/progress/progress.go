// Package progress turns pipeline events into one status line that says
// exactly what is being done and how far along it is.
package progress

import (
	"fmt"
	"sync"
	"time"

	"github.com/RishiKendai/veyra/internal/format"
)

// Stage names reported by the pipeline.
const (
	Scan        = "scan"        // walking the folder and identifying media
	Compare     = "compare"     // reading file heads and tails of same-size files
	Hash        = "hash"        // full BLAKE3 of what is still tied
	Fingerprint = "fingerprint" // decoding frames/pixels of each file
	Group       = "group"       // matching and clustering
)

// loader is a braille dot spinner, so a quiet stage (one long ffmpeg call, a
// slow disk, waiting for a drive) still visibly shows the tool is busy.
var loader = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// frameEvery is how long each loader frame is shown.
const frameEvery = 80 * time.Millisecond

// Tracker is safe for concurrent use.
type Tracker struct {
	mu          sync.Mutex
	media       string // plural noun: "videos" or "images"
	start       time.Time
	stage       string
	stageStart  time.Time
	done, total int
	seen, found int64
	note        string // if set, replaces the stage text (e.g. waiting for a drive)
}

// New returns a Tracker; media is the plural noun for the scanned kind.
func New(media string) *Tracker {
	now := time.Now()
	return &Tracker{media: media, start: now, stage: Scan, stageStart: now}
}

// SetScan records the directory walk counters (files examined, media found).
func (t *Tracker) SetScan(seen, found int64) {
	t.mu.Lock()
	t.seen, t.found = seen, found
	t.mu.Unlock()
}

// Set records progress inside a stage; total 0 means unknown. Entering a new
// stage restarts its clock.
func (t *Tracker) Set(stage string, done, total int) {
	t.mu.Lock()
	if stage != t.stage {
		t.stage, t.stageStart = stage, time.Now()
	}
	t.done, t.total = done, total
	t.mu.Unlock()
}

// SetNote shows text instead of the stage line until it is cleared with "".
func (t *Tracker) SetNote(s string) {
	t.mu.Lock()
	t.note = s
	t.mu.Unlock()
}

// Line renders the current status.
func (t *Tracker) Line(now time.Time) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var s string
	if t.note != "" {
		return loader[int(now.Sub(t.start)/frameEvery)%len(loader)] + " " + t.note +
			"  ·  " + format.Elapsed(now.Sub(t.start).Round(time.Second))
	}
	switch t.stage {
	case Scan:
		s = fmt.Sprintf("Scanning folder: %s files examined, %s %s found", format.Int(int(t.seen)), format.Int(int(t.found)), t.media)
	case Compare:
		s = "Prehashing the files" + t.count()
	case Hash:
		s = "Hashing files" + t.count()
	case Fingerprint:
		s = "Fingerprinting " + t.media + t.count()
	case Group:
		s = "Visual and Exact matches"
	default:
		s = t.stage
	}
	s = loader[int(now.Sub(t.start)/frameEvery)%len(loader)] + " " + s
	s += "  ·  " + format.Elapsed(now.Sub(t.start).Round(time.Second))
	if left, ok := t.eta(now); ok {
		s += "  ·  ~" + format.Elapsed(left.Round(time.Second)) + " left"
	}
	return s
}

func (t *Tracker) count() string {
	if t.total <= 0 {
		return ""
	}
	return fmt.Sprintf(": %s of %s (%d%%)", format.Int(t.done), format.Int(t.total), t.done*100/t.total)
}

// eta extrapolates from this stage's own rate once it has run long enough
// to be meaningful.
func (t *Tracker) eta(now time.Time) (time.Duration, bool) {
	el := now.Sub(t.stageStart)
	if t.total <= 0 || t.done <= 0 || t.done >= t.total || el < 3*time.Second {
		return 0, false
	}
	return time.Duration(float64(el) / float64(t.done) * float64(t.total-t.done)), true
}
