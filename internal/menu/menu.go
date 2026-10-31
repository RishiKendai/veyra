// Package menu is the interactive launcher shown when veyra is started with
// no arguments: pick what to scan, then pick the folder.
package menu

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/RishiKendai/veyra/internal/cli"
	"github.com/RishiKendai/veyra/internal/model"
)

// Result is what the user chose. Quit is set when they left without choosing.
type Result struct {
	Type      model.MediaType
	Recursive bool
	Root      string // absolute, validated
	Quit      bool
}

// Deps reports whether the tools needed for a media type are installed. A
// non-nil error is shown to the user and the choice is not accepted.
type Deps func(model.MediaType) error

type choice struct {
	label     string
	typ       model.MediaType
	recursive bool
}

var choices = []choice{
	{"Scan images", model.Image, false},
	{"Scan images (recursive)", model.Image, true},
	{"Scan videos", model.Video, false},
	{"Scan videos (recursive)", model.Video, true},
}

const banner = `__     _______ __   ______      _
\ \   / / ____|\ \ / /  _ \    / \
 \ \ / /|  _|   \ V /| |_) |  / _ \
  \ V / | |___   | | |  _ <  / ___ \
   \_/  |_____|  |_| |_| \_\/_/   \_\`

var (
	sTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	sSel   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	sDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	sErr   = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
)

type screen int

const (
	scMenu screen = iota
	scBrowse
	scType
)

// dirItem is one row of the folder browser.
type dirItem struct {
	label string
	path  string // folder to enter or use
	use   bool   // "use this folder"
}

// Model is the Bubble Tea model of the launcher.
type Model struct {
	deps Deps
	home string

	sc     screen
	cursor int // menu row, or browser row
	picked choice

	dir        string
	items      []dirItem
	showHidden bool
	input      textinput.Model

	err    string
	w, h   int
	result Result
	done   bool
}

// New returns a launcher whose folder browser starts in startDir.
func New(startDir string, deps Deps) *Model {
	in := textinput.New()
	in.Prompt = "Path: "
	in.Placeholder = "type or paste a folder, e.g. ~/Pictures"
	in.CharLimit = 4096
	home, _ := os.UserHomeDir()
	return &Model{deps: deps, home: home, dir: startDir, input: in, w: 80, h: 24}
}

// Result returns the user's choice once the program has finished.
func (m *Model) Result() Result { return m.result }

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) quit() (tea.Model, tea.Cmd) {
	m.result, m.done = Result{Quit: true}, true
	return m, tea.Quit
}

func (m *Model) finish(root string) (tea.Model, tea.Cmd) {
	m.result = Result{Type: m.picked.typ, Recursive: m.picked.recursive, Root: root}
	m.done = true
	return m, tea.Quit
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m.quit()
		}
		switch m.sc {
		case scMenu:
			return m.updateMenu(msg)
		case scBrowse:
			return m.updateBrowse(msg)
		default:
			return m.updateType(msg)
		}
	}
	return m, nil
}

func (m *Model) updateMenu(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch s := k.String(); s {
	case "q", "esc":
		return m.quit()
	case "up", "k":
		m.cursor = (m.cursor + len(choices)) % (len(choices) + 1)
	case "down", "j":
		m.cursor = (m.cursor + 1) % (len(choices) + 1)
	case "1", "2", "3", "4":
		m.cursor = int(s[0] - '1')
		return m.selectChoice()
	case "enter":
		return m.selectChoice()
	}
	return m, nil
}

func (m *Model) selectChoice() (tea.Model, tea.Cmd) {
	if m.cursor == len(choices) { // Quit
		return m.quit()
	}
	c := choices[m.cursor]
	if err := m.deps(c.typ); err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.picked, m.err = c, ""
	m.sc, m.cursor = scBrowse, 0
	m.load(m.dir)
	return m, nil
}

// load lists dir's sub-folders. A folder that cannot be read keeps the
// previous listing and says why.
func (m *Model) load(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		m.err = fmt.Sprintf("cannot open %s: %v", dir, err)
		return
	}
	m.err, m.dir = "", dir
	items := []dirItem{{label: "✔ Use this folder", path: dir, use: true}}
	if parent := filepath.Dir(dir); parent != dir {
		items = append(items, dirItem{label: "\u21a9 ..  (parent folder)", path: parent})
	}
	var subs []dirItem
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !m.showHidden {
			continue
		}
		p := filepath.Join(dir, name)
		if e.Type()&os.ModeSymlink != 0 { // follow links only to see whether they lead to a folder
			if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
				continue
			}
		} else if !e.IsDir() {
			continue
		}
		subs = append(subs, dirItem{label: name + string(filepath.Separator), path: p})
	}
	sort.Slice(subs, func(i, j int) bool { return strings.ToLower(subs[i].label) < strings.ToLower(subs[j].label) })
	m.items = append(items, subs...)
	m.cursor = 0
}

func (m *Model) updateBrowse(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch s := k.String(); s {
	case "esc", "q":
		m.sc, m.cursor, m.err = scMenu, 0, ""
	case "up", "k":
		m.cursor = (m.cursor + len(m.items) - 1) % len(m.items)
	case "down", "j":
		m.cursor = (m.cursor + 1) % len(m.items)
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = len(m.items) - 1
	case "left", "h", "backspace":
		if parent := filepath.Dir(m.dir); parent != m.dir {
			m.load(parent)
		}
	case "right", "l":
		if it := m.items[m.cursor]; !it.use {
			m.load(it.path)
		}
	case "s":
		return m.accept(m.dir)
	case ".":
		m.showHidden = !m.showHidden
		m.load(m.dir)
	case "tab", "/", "~":
		m.sc, m.err = scType, ""
		m.input.SetValue(m.dir)
		m.input.CursorEnd()
		return m, m.input.Focus()
	case "enter":
		if it := m.items[m.cursor]; it.use {
			return m.accept(it.path)
		} else {
			m.load(it.path)
		}
	}
	return m, nil
}

func (m *Model) updateType(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "tab":
		m.sc, m.err = scBrowse, ""
		m.input.Blur()
		return m, nil
	case "enter":
		return m.accept(m.expand(m.input.Value()))
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

// expand turns a leading ~ into the home folder and a relative path into one
// relative to the folder being browsed.
func (m *Model) expand(p string) string {
	p = strings.TrimSpace(p)
	switch {
	case p == "~" && m.home != "":
		return m.home
	case strings.HasPrefix(p, "~/") && m.home != "":
		return filepath.Join(m.home, p[2:])
	case p != "" && !filepath.IsAbs(p):
		return filepath.Join(m.dir, p)
	}
	return p
}

// accept validates root with the same rules as the command line.
func (m *Model) accept(root string) (tea.Model, tea.Cmd) {
	abs, err := cli.ValidateDir(root)
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	return m.finish(abs)
}

func (m *Model) View() string {
	var b strings.Builder
	b.WriteString(sTitle.Render(banner) + "\n")
	b.WriteString(sDim.Render("Visual Exploration & Yield-based Recognition Assistant") + "\n\n")
	switch m.sc {
	case scMenu:
		b.WriteString("What do you want to scan?\n\n")
		rows := make([]string, 0, len(choices)+1)
		for _, c := range choices {
			rows = append(rows, c.label)
		}
		rows = append(rows, "Quit")
		for i, r := range rows {
			num := "  "
			if i < len(choices) {
				num = fmt.Sprintf("%d ", i+1)
			}
			if i == m.cursor {
				b.WriteString(sSel.Render("\u25b8 "+num+r) + "\n")
			} else {
				b.WriteString("  " + num + r + "\n")
			}
		}
		b.WriteString("\n" + sDim.Render("↑↓ move · Enter select · 1-4 jump · Q quit"))
	case scBrowse:
		b.WriteString(sSel.Render(m.picked.label) + sDim.Render(" — choose the folder to scan") + "\n")
		b.WriteString(sDim.Render(m.dir) + "\n\n")
		visible := max(m.h-18, 5)
		start := max(0, min(m.cursor-visible/2, len(m.items)-visible))
		end := min(len(m.items), start+visible)
		for i := start; i < end; i++ {
			line := m.items[i].label
			if i == m.cursor {
				b.WriteString(sSel.Render("\u25b8 "+line) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
		if end < len(m.items) {
			b.WriteString(sDim.Render(fmt.Sprintf("  … %d more", len(m.items)-end)) + "\n")
		}
		b.WriteString("\n" + sDim.Render("↑↓ move · Enter open/select · ← up · S use this folder · Tab type a path · . hidden · Esc back"))
	case scType:
		b.WriteString(sSel.Render(m.picked.label) + sDim.Render(" — type the folder to scan") + "\n\n")
		b.WriteString(m.input.View() + "\n\n")
		b.WriteString(sDim.Render("Enter scan · Tab/Esc back to the folder list · ~ means your home folder"))
	}
	if m.err != "" {
		b.WriteString("\n\n" + sErr.Render(m.err))
	}
	return b.String() + "\n"
}

// Run shows the launcher and returns the choice. Cancelling ctx or pressing
// Ctrl+C returns Result{Quit: true}. The terminal is restored on every path.
func Run(ctx context.Context, startDir string, deps Deps) (Result, error) {
	m := New(startDir, deps)
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if err != nil {
		if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
			return Result{Quit: true}, nil
		}
		return Result{}, err
	}
	if !m.done {
		return Result{Quit: true}, nil
	}
	return m.result, nil
}
