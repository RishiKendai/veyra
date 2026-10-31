package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Smallest terminal the two-panel layout is usable in.
const (
	minW    = 60
	minH    = 18
	minBody = 8 // rows left for the panels after header and footer
	footerH = 2
)

// layout holds the screen geometry that depends only on the window size and
// the header height. Rendering and mouse hit-testing both read from it, so a
// click always lands on what was drawn.
type layout struct {
	w, h          int
	headerH       int
	bodyY, bodyH  int
	leftW, rightW int
	rightX        int
	rightInner    int // inner width of the right panels

	searchY                    int // screen row of the search line
	listX, listY, listW, listH int // list rows area, absolute coordinates
}

func newLayout(w, h, headerH int) layout {
	l := layout{w: w, h: h, headerH: headerH, bodyY: headerH}
	l.bodyH = h - headerH - footerH
	l.leftW = min(max(w*38/100, 24), 56)
	l.rightW = w - l.leftW
	l.rightX = l.leftW
	l.rightInner = l.rightW - 2
	l.searchY = l.bodyY + 1
	l.listX, l.listY = 1, l.bodyY+2
	l.listW = l.leftW - 2
	l.listH = max(0, l.bodyH-3)
	return l
}

func (l layout) tooSmall() bool { return l.w < minW || l.h < minH || l.bodyH < minBody }

// button is a clickable label at absolute screen coordinates.
type button struct {
	label  string
	action string
	danger bool
	x0, x1 int // columns, x1 exclusive
	y      int
}

var buttonDefs = []struct {
	label, action string
	danger        bool
}{
	{"Preview ↵", "open", false},
	{"Reveal O", "reveal", false},
	{"Copy path C", "copy", false},
	{"Delete D", "delete", true},
}

// packButtons places the buttons left to right starting at column x0 and row
// y0, wrapping to a new row when one would cross x0+width. It returns the
// buttons and how many rows they use.
func packButtons(x0, width, y0 int) ([]button, int) {
	var out []button
	x, row := x0, 0
	for _, d := range buttonDefs {
		text := "[ " + d.label + " ]"
		wd := ansi.StringWidth(text)
		if x > x0 && x+wd > x0+width {
			x, row = x0, row+1
		}
		out = append(out, button{label: text, action: d.action, danger: d.danger, x0: x, x1: x + wd, y: y0 + row})
		x += wd + 2
	}
	return out, row + 1
}

// fit truncates s to w display cells (with an ellipsis) and pads it to
// exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// midTrunc shortens s to at most w cells keeping both ends, for places where
// a single line is all there is room for (the header). Detail panels wrap instead.
func midTrunc(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw <= w || w <= 1 {
		if w <= 0 {
			return ""
		}
		return ansi.Truncate(s, w, "…")
	}
	keepR := (w - 1) / 2
	keepL := w - 1 - keepR
	return ansi.Truncate(s, keepL, "") + "…" + ansi.TruncateLeft(s, sw-keepR, "")
}

// box draws a titled border around lines and returns exactly h strings of
// exactly w cells each.
func box(title string, lines []string, w, h int, titleStyle func(...string) string) []string {
	if w < 4 || h < 2 {
		return nil
	}
	inner := w - 2
	t := " " + title + " "
	if ansi.StringWidth(t) > inner-2 {
		t = ansi.Truncate(t, max(inner-2, 0), "")
	}
	top := "┌─" + titleStyle(t) + strings.Repeat("─", max(inner-1-ansi.StringWidth(t), 0)) + "┐"
	out := make([]string, 0, h)
	out = append(out, top)
	for i := range h - 2 {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out = append(out, "│"+fit(line, inner)+"│")
	}
	out = append(out, "└"+strings.Repeat("─", inner)+"┘")
	return out
}
