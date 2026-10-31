// Package tui is the interactive Bubble Tea results browser.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RishiKendai/veyra/internal/format"
	"github.com/RishiKendai/veyra/internal/model"
	"github.com/RishiKendai/veyra/internal/platform"
	"github.com/RishiKendai/veyra/internal/result"
)

// Input is everything the TUI shows; it is independent of how it was computed.
type Input struct {
	Groups   []result.Group
	Root     string
	Type     model.MediaType
	Examined int           // regular files looked at, media or not
	Scanned  int           // media files found
	Skipped  int           // files that could not be processed
	Partial  bool          // some paths could not be read
	Elapsed  time.Duration // how long the scan took
}

// Actions are the native operations the TUI can trigger. *platform.Opener
// implements it; tests substitute a fake.
type Actions interface {
	Open(path string) error
	Reveal(path string) error
	QuickLook(path string) error
	Copy(text string) error
	Trash(path string) error
}

// Described is optionally implemented by Actions to name the local file
// manager and report which actions exist on this OS.
type Described interface {
	RevealLabel() string
	RestoreHint() string
	CanQuickLook() bool
}

// ref identifies a row by content, not position, so it survives filtering.
// f == -1 is the group header.
type ref struct{ g, f int }

type fileInfo struct {
	birth time.Time
	err   error
}

// Model is the Bubble Tea model.
type Model struct {
	in  Input
	act Actions
	l   layout

	header []string // header lines for the current width and data
	sum    result.Summary

	expanded []bool
	vis      []ref // rows currently visible, after filter and folding

	sel      ref // committed selection: what Enter/O/C/D act on
	hasSel   bool
	hover    ref // pointer position; highlights only, never acted upon
	hasHover bool
	offset   int // first visible row of the list

	input     textinput.Model
	searching bool
	filter    string

	status    string
	statusErr bool
	help      bool

	confirm    string // path awaiting delete confirmation; "" when none
	metaOff    int    // first visible metadata line
	lastTarget string // detects selection changes to reset metaOff

	info    map[string]fileInfo // lazily stat'ed, keyed by path
	pending map[string]bool
}

// New builds the model. Small result sets start fully expanded. The groups
// are copied, so deleting a file never changes the caller's data.
func New(in Input, act Actions) *Model {
	groups := make([]result.Group, len(in.Groups))
	for i, g := range in.Groups {
		g.Files = slices.Clone(g.Files)
		groups[i] = g
	}
	in.Groups = groups

	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "type to filter"
	ti.CharLimit = 200
	ti.Cursor.SetMode(cursor.CursorStatic) // no blink ticks to repaint for

	m := &Model{
		in: in, act: act, input: ti,
		expanded: make([]bool, len(in.Groups)),
		info:     map[string]fileInfo{},
		pending:  map[string]bool{},
		sum:      result.Summarize(in.Groups),
	}
	for i := range m.expanded {
		m.expanded[i] = len(in.Groups) <= 50 || i == 0
	}
	m.rebuild()
	return m
}

func (m *Model) Init() tea.Cmd { return m.wantStat() }

// relayout recomputes the header and the geometry after a resize or after
// the numbers in the header changed.
func (m *Model) relayout() {
	if m.l.w == 0 {
		return
	}
	w, h := m.l.w, m.l.h
	m.header = m.headerLines(w)
	m.l = newLayout(w, h, len(m.header))
	m.input.Width = max(m.l.listW-10, 5)
	m.rebuild()
}

// ---- tree state ------------------------------------------------------------

func groupLabel(g *result.Group) string {
	return fmt.Sprintf("group %02d %s", g.ID, g.Class)
}

// rebuild recomputes the visible rows from the groups, the fold state and the
// filter, and keeps the selection on something visible.
func (m *Model) rebuild() {
	m.vis = m.vis[:0]
	for gi := range m.in.Groups {
		g := &m.in.Groups[gi]
		expanded := m.expanded[gi]
		var files []int
		if m.filter != "" {
			_, groupHit := Match(m.filter, groupLabel(g))
			for fi := range g.Files {
				if groupHit {
					files = append(files, fi)
				} else if _, ok := Match(m.filter, g.Files[fi].Path); ok {
					files = append(files, fi)
				}
			}
			if len(files) == 0 {
				continue
			}
			expanded = true // a filter shows its matches, whatever the fold state
		} else {
			for fi := range g.Files {
				files = append(files, fi)
			}
		}
		m.vis = append(m.vis, ref{gi, -1})
		if expanded {
			for _, fi := range files {
				m.vis = append(m.vis, ref{gi, fi})
			}
		}
	}

	if !m.hasSel || m.indexOf(m.sel) < 0 {
		switch {
		case m.hasSel && m.indexOf(ref{m.sel.g, -1}) >= 0:
			m.sel = ref{m.sel.g, -1}
		case len(m.vis) > 0:
			m.sel, m.hasSel = m.vis[0], true
		default:
			m.hasSel = false
		}
	}
	if m.hasHover && m.indexOf(m.hover) < 0 {
		m.hasHover = false
	}
	m.ensureVisible()
}

func (m *Model) indexOf(r ref) int {
	for i, v := range m.vis {
		if v == r {
			return i
		}
	}
	return -1
}

func (m *Model) ensureVisible() {
	h := max(m.l.listH, 1)
	if i := m.indexOf(m.sel); m.hasSel && i >= 0 {
		if i < m.offset {
			m.offset = i
		}
		if i >= m.offset+h {
			m.offset = i - h + 1
		}
	}
	m.clampOffset()
}

func (m *Model) clampOffset() {
	m.offset = max(0, min(m.offset, len(m.vis)-max(m.l.listH, 1)))
}

func (m *Model) setSelection(r ref) {
	m.sel, m.hasSel = r, true
	m.ensureVisible()
}

func (m *Model) move(delta int) {
	if len(m.vis) == 0 {
		return
	}
	i := m.indexOf(m.sel)
	if i < 0 {
		i = 0
	} else {
		i = max(0, min(i+delta, len(m.vis)-1))
	}
	m.setSelection(m.vis[i])
}

func (m *Model) jump(i int) {
	if len(m.vis) > 0 {
		m.setSelection(m.vis[max(0, min(i, len(m.vis)-1))])
	}
}

func (m *Model) toggle(g int) {
	m.expanded[g] = !m.expanded[g]
	m.rebuild()
}

// left collapses the current group, or moves from a file to its group.
func (m *Model) left() {
	if !m.hasSel {
		return
	}
	if m.sel.f >= 0 {
		m.setSelection(ref{m.sel.g, -1})
		return
	}
	if m.expanded[m.sel.g] && m.filter == "" {
		m.toggle(m.sel.g)
	}
}

// right expands the current group, or moves into its first file.
func (m *Model) right() {
	if !m.hasSel || m.sel.f >= 0 {
		return
	}
	if !m.expanded[m.sel.g] {
		m.toggle(m.sel.g)
		return
	}
	if i := m.indexOf(m.sel); i+1 < len(m.vis) && m.vis[i+1].g == m.sel.g {
		m.setSelection(m.vis[i+1])
	}
}

// space folds or unfolds the group of the current row.
func (m *Model) space() {
	if !m.hasSel {
		return
	}
	g := m.sel.g
	m.sel = ref{g, -1}
	m.toggle(g)
}

func (m *Model) setFilter(s string) {
	if s == m.filter {
		return
	}
	m.filter = s
	m.offset = 0
	m.rebuild()
}

// target is the file that open/reveal/copy apply to: the selected file, or the
// first file of the selected group.
func (m *Model) target() (model.File, *result.Group, bool) {
	if !m.hasSel || m.sel.g >= len(m.in.Groups) {
		return model.File{}, nil, false
	}
	g := &m.in.Groups[m.sel.g]
	return g.Files[max(m.sel.f, 0)], g, true
}

// removeFile drops a file from the results after it was moved to the Trash.
// A group left with a single file is no longer a duplicate set and goes too.
func (m *Model) removeFile(path string) {
	for gi := range m.in.Groups {
		g := &m.in.Groups[gi]
		for fi := range g.Files {
			if g.Files[fi].Path != path {
				continue
			}
			g.Files = slices.Delete(g.Files, fi, fi+1)
			delete(m.info, path)
			if len(g.Files) < 2 {
				m.in.Groups = slices.Delete(m.in.Groups, gi, gi+1)
				m.expanded = slices.Delete(m.expanded, gi, gi+1)
				m.sel = ref{min(gi, len(m.in.Groups)-1), -1}
				m.hasSel = len(m.in.Groups) > 0
			} else {
				m.sel = ref{gi, min(fi, len(g.Files)-1)}
				m.hasSel = true
			}
			m.hasHover = false
			m.sum = result.Summarize(m.in.Groups)
			m.relayout()
			m.ensureVisible()
			return
		}
	}
}

// ---- messages and commands -------------------------------------------------

type actionMsg struct {
	verb string
	name string
	err  error
}

type trashMsg struct {
	path string
	name string
	size int64
	err  error
}

type statMsg struct {
	path string
	info fileInfo
}

func (m *Model) act1(verb string, fn func(string) error, path string) tea.Cmd {
	name := filepath.Base(path)
	return func() tea.Msg { return actionMsg{verb: verb, name: name, err: fn(path)} }
}

// wantStat asks for filesystem info about the selected file once. The stat
// runs as a command because a stale network or external mount can block.
func (m *Model) wantStat() tea.Cmd {
	f, _, ok := m.target()
	if !ok {
		return nil
	}
	if _, done := m.info[f.Path]; done || m.pending[f.Path] {
		return nil
	}
	m.pending[f.Path] = true
	path := f.Path
	return func() tea.Msg {
		fi, err := os.Stat(path)
		if err != nil {
			return statMsg{path, fileInfo{err: err}}
		}
		return statMsg{path, fileInfo{birth: platform.BirthTime(fi)}}
	}
}

// do runs a native action on the target file.
func (m *Model) do(action string) tea.Cmd {
	f, _, ok := m.target()
	if !ok {
		return nil
	}
	switch action {
	case "open":
		return m.act1("Opened", m.act.Open, f.Path)
	case "reveal":
		return m.act1("Revealed", m.act.Reveal, f.Path)
	case "quicklook":
		return m.act1("Quick Look closed for", m.act.QuickLook, f.Path)
	case "copy":
		path := f.Path
		return func() tea.Msg {
			return actionMsg{verb: "Copied path of", name: filepath.Base(path), err: m.act.Copy(path)}
		}
	case "delete":
		m.askDelete()
	}
	return nil
}

// askDelete starts the confirmation. It needs a file row: deleting from a group
// header would silently pick a file the user did not point at.
func (m *Model) askDelete() {
	if !m.hasSel || m.sel.f < 0 {
		m.setStatus("Select a file first: a group row cannot be deleted", true)
		return
	}
	m.confirm = m.in.Groups[m.sel.g].Files[m.sel.f].Path
}

func (m *Model) trashCmd(path string) tea.Cmd {
	_, g, _ := m.target()
	var size int64
	for _, f := range g.Files {
		if f.Path == path {
			size = f.Size
		}
	}
	return func() tea.Msg {
		return trashMsg{path: path, name: filepath.Base(path), size: size, err: m.act.Trash(path)}
	}
}

func (m *Model) setStatus(s string, isErr bool) { m.status, m.statusErr = s, isErr }

// ---- Update ----------------------------------------------------------------

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.l = layout{w: msg.Width, h: msg.Height}
		m.relayout()
	case tea.KeyMsg:
		cmd = m.key(msg)
	case tea.MouseMsg:
		cmd = m.mouse(msg)
	case actionMsg:
		if msg.err != nil {
			m.setStatus(fmt.Sprintf("Could not do that with %s: %v", msg.name, msg.err), true)
		} else {
			m.setStatus(fmt.Sprintf("%s %s", msg.verb, msg.name), false)
		}
	case trashMsg:
		if msg.err != nil {
			m.setStatus(fmt.Sprintf("Could not move %s to the Trash: %v", msg.name, msg.err), true)
		} else {
			m.removeFile(msg.path)
			m.setStatus(fmt.Sprintf("Moved %s to the Trash (%s)", msg.name, format.Size(msg.size)), false)
		}
	case statMsg:
		delete(m.pending, msg.path)
		m.info[msg.path] = msg.info
	}
	if f, _, ok := m.target(); ok && f.Path != m.lastTarget {
		m.lastTarget, m.metaOff = f.Path, 0
	}
	return m, tea.Batch(cmd, m.wantStat())
}

func (m *Model) key(msg tea.KeyMsg) tea.Cmd {
	if m.l.tooSmall() {
		// Nothing on screen to act on: only quitting is allowed, so no hidden
		// delete, open or reveal can be triggered.
		if k := msg.String(); k == "q" || k == "ctrl+c" {
			return tea.Quit
		}
		return nil
	}
	if m.help {
		m.help = false
		return nil
	}
	if m.confirm != "" {
		return m.confirmKey(msg)
	}
	m.setStatus("", false)
	if m.searching {
		return m.searchKey(msg)
	}
	switch msg.String() {
	case "q", "ctrl+c":
		return tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-max(m.l.listH-1, 1))
	case "pgdown":
		m.move(max(m.l.listH-1, 1))
	case "home", "g":
		m.jump(0)
	case "end", "G":
		m.jump(len(m.vis) - 1)
	case "left":
		m.left()
	case "right":
		m.right()
	case " ":
		m.space()
	case "enter":
		if m.hasSel && m.sel.f < 0 {
			m.toggle(m.sel.g)
			return nil
		}
		return m.do("open")
	case "o":
		return m.do("reveal")
	case "c":
		return m.do("copy")
	case "p":
		if d, ok := m.act.(Described); ok && !d.CanQuickLook() {
			return nil // no native preview on this OS
		}
		return m.do("quicklook")
	case "d":
		return m.do("delete")
	case "[":
		m.scrollMeta(-3)
	case "]":
		m.scrollMeta(3)
	case "/":
		return m.startSearch()
	case "esc":
		if m.filter != "" {
			m.input.SetValue("")
			m.setFilter("")
		}
	case "?":
		m.help = true
	}
	return nil
}

// confirmKey: Enter deletes; every other key cancels.
func (m *Model) confirmKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		path := m.confirm
		m.confirm = ""
		m.setStatus("Moving to the Trash…", false)
		return m.trashCmd(path)
	case "ctrl+c":
		return tea.Quit
	}
	m.confirm = ""
	m.setStatus("Delete cancelled", false)
	return nil
}

func (m *Model) scrollMeta(delta int) {
	p := m.pane()
	// A step larger than the window would skip lines that could then never be seen.
	step := max(1, p.metaRows-1)
	delta = max(-step, min(delta, step))
	m.metaOff = max(0, min(m.metaOff+delta, len(p.meta)-p.metaRows))
}

func (m *Model) startSearch() tea.Cmd {
	m.searching = true
	return m.input.Focus()
}

func (m *Model) stopSearch() {
	m.searching = false
	m.input.Blur()
}

func (m *Model) searchKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		m.input.SetValue("")
		m.setFilter("")
		m.stopSearch()
	case "enter":
		m.stopSearch()
	case "up":
		m.move(-1)
	case "down":
		m.move(1)
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.setFilter(m.input.Value())
		return cmd
	}
	return nil
}

// rowAt maps a screen position to a list row.
func (m *Model) rowAt(x, y int) (ref, bool) {
	l := m.l
	if x < l.listX || x >= l.listX+l.listW || y < l.listY || y >= l.listY+l.listH {
		return ref{}, false
	}
	if i := m.offset + y - l.listY; i < len(m.vis) {
		return m.vis[i], true
	}
	return ref{}, false
}

func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	if m.help || m.l.tooSmall() {
		return nil
	}
	press := msg.Action == tea.MouseActionPress
	if m.confirm != "" && press && msg.Button == tea.MouseButtonLeft {
		m.confirm = "" // any click outside the prompt cancels it
		m.setStatus("Delete cancelled", false)
		return nil
	}
	switch {
	case msg.Action == tea.MouseActionMotion:
		// Hover only highlights. It must never change what Enter opens or deletes.
		m.hover, m.hasHover = m.rowAt(msg.X, msg.Y)
	case press && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown):
		d := 3
		if msg.Button == tea.MouseButtonWheelUp {
			d = -3
		}
		if msg.X >= m.l.rightX && msg.Y >= m.pane().metaY {
			m.scrollMeta(d)
		} else {
			m.offset += d
			m.clampOffset()
		}
	case press && msg.Button == tea.MouseButtonLeft:
		m.setStatus("", false)
		if r, ok := m.rowAt(msg.X, msg.Y); ok {
			m.setSelection(r)
			if r.f < 0 {
				m.toggle(r.g)
			}
			return nil
		}
		if msg.Y == m.l.searchY && msg.X < m.l.leftW {
			return m.startSearch()
		}
		for _, b := range m.pane().btns {
			if msg.Y == b.y && msg.X >= b.x0 && msg.X < b.x1 {
				return m.do(b.action)
			}
		}
	}
	return nil
}
