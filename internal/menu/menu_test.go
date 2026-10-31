package menu

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RishiKendai/veyra/internal/model"
)

func noDeps(model.MediaType) error { return nil }

// press sends keys to the model: single runes as typed, and named keys such as "enter".
func press(m *Model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		case "ctrl+c":
			msg = tea.KeyMsg{Type: tea.KeyCtrlC}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m.Update(msg)
	}
}

func tree(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	for _, d := range []string{"alpha", "Beta", ".hidden", "with space"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestNumberKeysPickTheScan(t *testing.T) {
	for key, want := range map[string]choice{
		"1": {typ: model.Image}, "2": {typ: model.Image, recursive: true},
		"3": {typ: model.Video}, "4": {typ: model.Video, recursive: true},
	} {
		m := New(tree(t), noDeps)
		press(m, key)
		if m.sc != scBrowse || m.picked.typ != want.typ || m.picked.recursive != want.recursive {
			t.Errorf("key %s: screen=%v picked=%+v", key, m.sc, m.picked)
		}
	}
}

func TestSelectCurrentFolderFinishes(t *testing.T) {
	root := tree(t)
	m := New(root, noDeps)
	press(m, "2", "enter") // first row of the browser is "Use this folder"
	got := m.Result()
	abs, _ := filepath.Abs(root)
	if !m.done || got.Type != model.Image || !got.Recursive || got.Root != abs || got.Quit {
		t.Fatalf("done=%v result=%+v", m.done, got)
	}
}

func TestBrowseIntoFolderAndSelect(t *testing.T) {
	root := tree(t)
	m := New(root, noDeps)
	press(m, "3")
	var names []string
	for _, it := range m.items {
		names = append(names, it.label)
	}
	joined := strings.Join(names, "|")
	if strings.Contains(joined, "file.txt") || strings.Contains(joined, ".hidden") {
		t.Errorf("files and hidden folders must not be listed: %v", names)
	}
	// Order: use, parent, alpha, Beta, with space (case-insensitive).
	press(m, "down", "down", "enter") // parent row, then alpha
	if filepath.Base(m.dir) != "alpha" {
		t.Fatalf("dir = %s (items %v)", m.dir, names)
	}
	press(m, "s")
	if got := m.Result(); !m.done || filepath.Base(got.Root) != "alpha" || got.Type != model.Video {
		t.Fatalf("result = %+v", got)
	}
}

func TestTypedPathSupportsSpacesAndRelative(t *testing.T) {
	root := tree(t)
	m := New(root, noDeps)
	press(m, "1", "tab")
	if m.sc != scType {
		t.Fatal("tab should open the path field")
	}
	m.input.SetValue("with space")
	press(m, "enter")
	if got := m.Result(); !m.done || filepath.Base(got.Root) != "with space" {
		t.Fatalf("result = %+v err=%q", got, m.err)
	}
}

func TestBadPathsAreRejectedAndStayOnScreen(t *testing.T) {
	root := tree(t)
	m := New(root, noDeps)
	press(m, "1", "tab")
	for _, bad := range []string{filepath.Join(root, "nope"), filepath.Join(root, "file.txt")} {
		m.input.SetValue(bad)
		press(m, "enter")
		if m.done || m.err == "" {
			t.Errorf("%s: done=%v err=%q", bad, m.done, m.err)
		}
	}
}

func TestMissingToolsBlockTheChoice(t *testing.T) {
	m := New(tree(t), func(tp model.MediaType) error {
		if tp == model.Video {
			return errors.New("ffmpeg not found")
		}
		return nil
	})
	press(m, "3")
	if m.sc != scMenu || !strings.Contains(m.err, "ffmpeg") {
		t.Fatalf("screen=%v err=%q", m.sc, m.err)
	}
	press(m, "1") // images still work
	if m.sc != scBrowse || m.err != "" {
		t.Fatalf("screen=%v err=%q", m.sc, m.err)
	}
}

func TestEscGoesBackAndQuitReturnsQuit(t *testing.T) {
	m := New(tree(t), noDeps)
	press(m, "1", "esc")
	if m.sc != scMenu || m.done {
		t.Fatalf("esc in the browser should return to the menu, screen=%v done=%v", m.sc, m.done)
	}
	press(m, "q")
	if !m.done || !m.Result().Quit {
		t.Fatalf("result = %+v", m.Result())
	}
	m = New(tree(t), noDeps)
	press(m, "ctrl+c")
	if !m.Result().Quit {
		t.Error("ctrl+c must quit")
	}
	m = New(tree(t), noDeps)
	press(m, "up", "enter") // wraps to the Quit row
	if !m.Result().Quit {
		t.Error("Quit row must quit")
	}
}

func TestViewListsAllFourChoices(t *testing.T) {
	v := New(tree(t), noDeps).View()
	for _, want := range []string{"Scan images", "Scan images (recursive)", "Scan videos", "Scan videos (recursive)", "Quit", "Visual Exploration"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q", want)
		}
	}
}
