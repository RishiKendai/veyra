// Package report prints scan results as plain text. It is the non-interactive
// counterpart of the TUI, used when stdout is not a terminal.
package report

import (
	"fmt"
	"io"
	"time"

	"github.com/RishiKendai/veyra/internal/format"
	"github.com/RishiKendai/veyra/internal/model"
	"github.com/RishiKendai/veyra/internal/result"
)

// Info describes the scan the groups came from.
type Info struct {
	Type     model.MediaType
	Examined int           // regular files looked at
	Scanned  int           // media files found
	Elapsed  time.Duration // total time of the whole process
}

// Write prints a summary, then one block per group separated by blank lines.
// In every group the largest file is the original; the others are duplicates
// and their sizes add up to the storage the duplicates use.
func Write(w io.Writer, info Info, groups []result.Group) {
	sum := result.Summarize(groups)
	fmt.Fprintf(w, "%s %s files found", format.Int(info.Scanned), info.Type)
	if info.Examined > 0 {
		fmt.Fprintf(w, " (%s files examined)", format.Int(info.Examined))
	}
	fmt.Fprintf(w, "\n%d duplicate groups, %s matched files, %s duplicates using %s",
		sum.Groups, format.Int(sum.Files), format.Int(sum.Duplicates), format.Size(sum.DuplicateBytes))
	if info.Elapsed > 0 {
		fmt.Fprintf(w, " · %s taken", format.Elapsed(info.Elapsed))
	}
	fmt.Fprintln(w)
	if sum.Groups > 0 {
		fmt.Fprintln(w, "(the largest file of each group is the original and is not counted)")
	}
	for _, g := range groups {
		fmt.Fprintf(w, "\nGROUP %02d · %s · %d files · %s duplicates\n", g.ID, g.Class, len(g.Files), format.Size(g.DuplicateBytes()))
		orig := g.OriginalIndex()
		for i, f := range g.Files {
			tag := ""
			if i == orig {
				tag = "  [original]"
			}
			fmt.Fprintf(w, "  %s  (%s)%s\n", f.Path, format.Size(f.Size), tag)
		}
		for _, warn := range g.Warnings {
			fmt.Fprintf(w, "  ! %s\n", warn)
		}
	}
}
