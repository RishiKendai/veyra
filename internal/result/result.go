// Package result is the UI-independent description of what a scan found.
package result

import (
	"fmt"
	"math"

	"github.com/RishiKendai/veyra/internal/model"
)

// Class says how strong the evidence for a group is.
type Class string

const (
	Exact  Class = "EXACT"  // byte-identical (full BLAKE3)
	Visual Class = "VISUAL" // same picture/footage after re-encoding or resizing
)

// Group is a set of two or more files believed to be duplicates.
type Group struct {
	ID    int // stable within one scan, 1-based, ordered by first path
	Type  model.MediaType
	Class Class
	Files []model.File // sorted by path

	// WorstDist is the largest raw matcher distance between any two matched
	// members (0 for EXACT). Its unit depends on the media type (images: summed
	// Hamming bits over three hashes; video: mismatching sampled frames), so it
	// is for ordering and diagnostics only and must not be shown as a percentage.
	WorstDist int

	// Warnings list reasons a VISUAL group deserves a closer look.
	Warnings []string
}

const (
	durationTolerance = 0.02 // relative spread before a video group is flagged
	aspectTolerance   = 0.02
)

// Analyze fills Warnings from the group's members. EXACT groups never warn:
// their bytes are identical.
func (g *Group) Analyze() {
	g.Warnings = nil
	if g.Class == Exact {
		return
	}
	if g.Type == model.Video {
		lo, hi := math.MaxFloat64, 0.0
		for _, f := range g.Files {
			if f.Duration > 0 {
				lo, hi = math.Min(lo, f.Duration), math.Max(hi, f.Duration)
			}
		}
		if hi > 0 && (hi-lo)/hi > durationTolerance {
			g.Warnings = append(g.Warnings,
				fmt.Sprintf("durations differ (%.1fs to %.1fs): trimmed or edited copy?", lo, hi))
		}
	}
	lo, hi := math.MaxFloat64, 0.0
	for _, f := range g.Files {
		if f.Width > 0 && f.Height > 0 {
			a := float64(f.Width) / float64(f.Height)
			lo, hi = math.Min(lo, a), math.Max(hi, a)
		}
	}
	if hi > 0 && (hi-lo)/hi > aspectTolerance {
		g.Warnings = append(g.Warnings, "aspect ratios differ: cropped, stretched or letterboxed copy?")
	}
}

// OriginalIndex is the member kept as the "original": the largest file, and
// the first by path when sizes tie (Files are sorted by path). "Larger" is a
// simple, predictable rule; for VISUAL groups it is not a statement about quality.
func (g *Group) OriginalIndex() int {
	best := 0
	for i, f := range g.Files {
		if f.Size > g.Files[best].Size {
			best = i
		}
	}
	return best
}

// DuplicateBytes is the storage taken by every member except the original.
func (g *Group) DuplicateBytes() int64 {
	if len(g.Files) == 0 {
		return 0
	}
	orig := g.OriginalIndex()
	var n int64
	for i, f := range g.Files {
		if i != orig {
			n += f.Size
		}
	}
	return n
}

// Summary totals a set of groups.
type Summary struct {
	Groups         int
	Files          int   // files in all groups, originals included
	Duplicates     int   // files that are not an original
	DuplicateBytes int64 // storage those duplicates take
}

// Summarize totals groups.
func Summarize(groups []Group) Summary {
	var s Summary
	for i := range groups {
		s.Groups++
		s.Files += len(groups[i].Files)
		s.Duplicates += max(len(groups[i].Files)-1, 0)
		s.DuplicateBytes += groups[i].DuplicateBytes()
	}
	return s
}
