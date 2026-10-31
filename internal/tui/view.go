package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/RishiKendai/veyra/internal/format"
	"github.com/RishiKendai/veyra/internal/result"
)

var (
	sBold     = lipgloss.NewStyle().Bold(true)
	sTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	sDim      = lipgloss.NewStyle().Faint(true)
	sErr      = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	sOK       = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	sWarn     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	sSelected = lipgloss.NewStyle().Bold(true).Reverse(true)
	sHover    = lipgloss.NewStyle().Background(lipgloss.Color("238"))
	sButton   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("16")).Background(lipgloss.Color("39"))
	sDanger   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("160"))
	sExact    = lipgloss.NewStyle().Foreground(lipgloss.Color("78")).Bold(true)
	sVisual   = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	sSpace    = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
)

func (m *Model) View() string {
	l := m.l
	if l.w == 0 || l.h == 0 {
		return ""
	}
	if l.tooSmall() {
		return m.smallView()
	}
	if m.help {
		return m.helpView()
	}

	p := m.pane()
	left := box("GROUPS / FILES", m.leftLines(), l.leftW, l.bodyH, sTitle.Render)
	right := box("FILE", p.top, l.rightW, p.topH, sTitle.Render)
	right = append(right, box(m.metaTitle(p), m.metaWindow(p), l.rightW, p.metaH, sTitle.Render)...)

	lines := make([]string, 0, l.h)
	lines = append(lines, m.header...)
	for i := range l.bodyH {
		lines = append(lines, left[i]+right[i])
	}
	lines = append(lines, m.footerLines()...)
	return strings.Join(lines, "\n")
}

func (m *Model) smallView() string {
	msg := []string{
		"Terminal too small",
		fmt.Sprintf("need %d×%d, have %d×%d", minW, minH, m.l.w, m.l.h),
		"Resize, or press q to quit",
	}
	out := make([]string, 0, m.l.h)
	for i := range m.l.h {
		line := ""
		if j := i - (m.l.h-len(msg))/2; j >= 0 && j < len(msg) {
			line = msg[j]
		}
		out = append(out, ansi.Truncate(line, m.l.w, ""))
	}
	return strings.Join(out, "\n")
}

// metaWindow returns the metadata lines that fit, starting at the scroll offset.
func (m *Model) metaWindow(p pane) []string {
	off := max(0, min(m.metaOff, len(p.meta)-p.metaRows))
	end := min(off+p.metaRows, len(p.meta))
	if off >= end {
		return nil
	}
	return p.meta[off:end]
}

func (m *Model) metaTitle(p pane) string {
	if len(p.meta) <= p.metaRows {
		return "METADATA"
	}
	off := max(0, min(m.metaOff, len(p.meta)-p.metaRows))
	return fmt.Sprintf("METADATA  lines %d-%d of %d  ([ ] or wheel scrolls)", off+1, min(off+p.metaRows, len(p.meta)), len(p.meta))
}

type seg struct {
	text  string
	style lipgloss.Style
}

// headerLines builds the title, the scanned folder and the statistics, wrapping
// the statistics onto more lines instead of cutting them off.
func (m *Model) headerLines(w int) []string {
	state := strings.ToUpper(string(m.in.Type)) + " SCAN COMPLETE"
	stateStyle := sTitle
	if m.in.Partial {
		state = strings.ToUpper(string(m.in.Type)) + " SCAN PARTIAL"
		stateStyle = sWarn
	}
	title := " " + sTitle.Render("VEYRA")
	st := stateStyle.Render(state)
	gap := max(w-ansi.StringWidth(title)-ansi.StringWidth(st)-1, 1)
	lines := []string{fit(title+strings.Repeat(" ", gap)+st+" ", w), sDim.Render(fit(" "+midTrunc(m.in.Root, max(w-2, 4)), w))}

	var segs []seg
	add := func(style lipgloss.Style, f string, a ...any) { segs = append(segs, seg{fmt.Sprintf(f, a...), style}) }
	if m.in.Examined > 0 {
		add(sDim, "%s files examined", format.Int(m.in.Examined))
	}
	add(sDim, "%s %s files", format.Int(m.in.Scanned), m.in.Type)
	add(sDim, "%d groups", m.sum.Groups)
	add(sDim, "%s matched files", format.Int(m.sum.Files))
	add(sDim, "%s duplicates", format.Int(m.sum.Duplicates))
	// Storage and total time are one block so they always sit side by side.
	if m.in.Elapsed > 0 {
		add(sSpace, "%s duplicates · %s taken", format.Size(m.sum.DuplicateBytes), format.Elapsed(m.in.Elapsed))
	} else {
		add(sSpace, "duplicates use %s", format.Size(m.sum.DuplicateBytes))
	}
	if m.in.Skipped > 0 {
		add(sWarn, "%d skipped", m.in.Skipped)
	}

	const sep = " · "
	cur, curW := " ", 1
	flush := func() {
		lines = append(lines, fit(cur, w))
		cur, curW = " ", 1
	}
	for i, s := range segs {
		sw := ansi.StringWidth(s.text)
		if i > 0 && curW > 1 && curW+len(sep)+sw > w {
			flush()
		}
		if curW > 1 {
			cur += sDim.Render(sep)
			curW += len(sep)
		}
		cur += s.style.Render(s.text)
		curW += sw
	}
	flush()
	return lines
}

var footerVariants = []string{
	" ↑↓ Move  Enter Open  O Reveal  C Copy  D Delete  / Search  [ ] Scroll  ? Help  Q Quit",
	" ↑↓ Move  Enter Open  O Reveal  C Copy  D Delete  / Search  ? Help  Q Quit",
	" ↑↓ Move  ↵ Open  O Reveal  C Copy  D Del  / Search  ? Help  Q Quit",
	" ↑↓ ↵ O C D / ?=Help Q=Quit",
}

func (m *Model) footerLines() []string {
	w := m.l.w
	var status string
	switch {
	case m.confirm != "":
		status = sErr.Render(fit(" Delete “"+filepath.Base(m.confirm)+"”? Enter = move to the Trash, any other key cancels", w))
	case m.status != "" && m.statusErr:
		status = sErr.Render(fit(" "+m.status, w))
	case m.status != "":
		status = sOK.Render(fit(" "+m.status, w))
	case m.in.Partial:
		status = sWarn.Render(fit(" Scan was partial: some paths could not be read", w))
	default:
		status = fit("", w)
	}
	keys := footerVariants[len(footerVariants)-1]
	for _, v := range footerVariants {
		if ansi.StringWidth(v) <= w {
			keys = v
			break
		}
	}
	if m.confirm != "" {
		keys = " Enter Confirm delete  Esc Cancel"
	} else if m.searching {
		keys = " Type to filter  ↑↓ Move  Enter Keep filter  Esc Clear"
	}
	return []string{status, sDim.Render(fit(keys, w))}
}

func (m *Model) leftLines() []string {
	l := m.l
	inner := l.leftW - 2
	var search string
	switch {
	case m.searching:
		search = sBold.Render("/ ") + m.input.View()
	case m.filter != "":
		search = sBold.Render("/ ") + m.filter + sDim.Render("  (Esc clears)")
	default:
		search = sDim.Render("/ to search")
	}
	lines := []string{fit(search, inner)}

	if len(m.vis) == 0 {
		msg := "No duplicate groups left"
		if m.filter != "" {
			msg = "No matches for “" + m.filter + "”"
		}
		return append(lines, sDim.Render(fit(msg, inner)))
	}
	end := min(m.offset+l.listH, len(m.vis))
	for i := m.offset; i < end; i++ {
		lines = append(lines, m.rowLine(m.vis[i], inner))
	}
	return lines
}

func (m *Model) rowLine(r ref, w int) string {
	g := &m.in.Groups[r.g]
	var plain, styled string
	if r.f < 0 {
		arrow := "▶"
		if m.expanded[r.g] || m.filter != "" {
			arrow = "▼"
		}
		head := fmt.Sprintf("%s GROUP %02d · %%s · %d files · frees %s", arrow, g.ID, len(g.Files), format.Size(g.DuplicateBytes()))
		plain = fmt.Sprintf(head, g.Class)
		tag := sExact
		if g.Class == result.Visual {
			tag = sVisual
		}
		styled = fmt.Sprintf(head, tag.Render(string(g.Class)))
	} else {
		f := g.Files[r.f]
		mark := "  "
		if r.f == g.OriginalIndex() {
			mark = "★ "
		}
		name := "  " + mark + filepath.Base(f.Path)
		plain, styled = name, name
		if room := w - ansi.StringWidth(name) - 2; room >= 6 {
			dir := m.relDir(f.Path)
			if dir != "" {
				dir = strings.TrimRight(fit(midTrunc(dir, room), room), " ")
				plain += "  " + dir
				styled += "  " + sDim.Render(dir)
			}
		}
	}
	switch {
	case m.hasSel && r == m.sel:
		return sSelected.Render(fit(plain, w))
	case m.hasHover && r == m.hover:
		return sHover.Render(fit(plain, w))
	}
	if ansi.StringWidth(plain) > w { // too narrow for styling to stay aligned
		return fit(plain, w)
	}
	return styled + strings.Repeat(" ", w-ansi.StringWidth(plain))
}

// relDir is the file's directory relative to the scan root ("" for the root itself).
func (m *Model) relDir(path string) string {
	rel, err := filepath.Rel(m.in.Root, filepath.Dir(path))
	if err != nil || rel == "." {
		return ""
	}
	return rel
}

func (m *Model) helpView() string {
	reveal, restore, quickLook := "Finder", "Finder's Put Back works", true
	if d, ok := m.act.(Described); ok {
		reveal, restore, quickLook = d.RevealLabel(), d.RestoreHint(), d.CanQuickLook()
	}
	rows := [][2]string{
		{"↑ ↓  / j k", "Move selection"},
		{"PgUp PgDn", "Move a page"},
		{"Home End", "First / last row"},
		{"←", "Collapse group, or go to parent group"},
		{"→", "Expand group, or enter it"},
		{"Space", "Fold / unfold the group"},
		{"Enter", "Open the file in its default app (group: fold)"},
		{"O", "Reveal in " + reveal},
		{"C", "Copy the full path"},
		{"D", "Delete: move the file to the Trash (Enter confirms)"},
		{"[ ]  / wheel", "Scroll the metadata when it does not fit"},
		{"/", "Fuzzy search; Enter keeps, Esc clears"},
		{"Esc", "Clear the filter / cancel a delete"},
		{"Mouse", "Click selects; hover highlights only; wheel scrolls"},
		{"?", "This help"},
		{"Q", "Quit"},
	}
	if quickLook {
		rows = slices.Insert(rows, 10, [2]string{"P", "Peek: Quick Look preview (qlmanage)"})
	}
	lines := []string{""}
	for _, r := range rows {
		lines = append(lines, "  "+sBold.Render(fit(r[0], 14))+r[1])
	}
	lines = append(lines, "",
		"  ★ marks the original of each group: the largest file. The rest are duplicates;",
		"  their combined size is what \"duplicates use\" in the header adds up.",
		sDim.Render("  Delete moves a file to the Trash ("+restore+"); nothing is removed permanently."),
		sDim.Render("  Press any key to close"))
	w := min(m.l.w, 96)
	h := min(m.l.h, len(lines)+2)
	b := box("HELP", lines, w, h, sTitle.Render)
	return strings.Join(b, "\n")
}
