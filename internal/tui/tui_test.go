package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/RishiKendai/veyra/internal/model"
	"github.com/RishiKendai/veyra/internal/result"
)

type fakeActions struct {
	calls    []string
	err      error
	trashErr error
}

func (f *fakeActions) rec(verb, p string) error { f.calls = append(f.calls, verb+" "+p); return f.err }
func (f *fakeActions) Open(p string) error      { return f.rec("open", p) }
func (f *fakeActions) Reveal(p string) error    { return f.rec("reveal", p) }
func (f *fakeActions) QuickLook(p string) error { return f.rec("ql", p) }
func (f *fakeActions) Copy(t string) error      { return f.rec("copy", t) }
func (f *fakeActions) Trash(p string) error     { f.calls = append(f.calls, "trash "+p); return f.trashErr }

func file(path string, size int64) model.File {
	return model.File{Path: path, Size: size, ModTime: time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC),
		Meta: model.Meta{Format: "mov,mp4", Width: 1920, Height: 1080, Duration: 135, Codec: "h264", FrameRate: 29.97, BitRate: 2_400_000, AudioCodec: "aac"}}
}

func fixture() Input {
	return Input{
		Root: "/root", Type: model.Video, Examined: 3000, Scanned: 2450, Elapsed: 151 * time.Second,
		Groups: []result.Group{
			{ID: 1, Type: model.Video, Class: result.Exact, Files: []model.File{file("/root/video.mp4", 100), file("/root/video-copy.mkv", 100)}},
			{ID: 2, Type: model.Video, Class: result.Visual, Files: []model.File{
				file("/root/holiday.mov", 1_200_000_000), file("/root/sub/holiday-copy.mp4", 300_000_000), file("/root/holiday.webm", 90_000_000)},
				Warnings: []string{"durations differ (60.0s to 50.0s): trimmed or edited copy?"}},
			{ID: 3, Type: model.Video, Class: result.Exact, Files: []model.File{file("/root/a/clip.mp4", 5), file("/root/b/clip.mp4", 5)}},
		},
	}
}

type harness struct {
	t    *testing.T
	m    *Model
	act  *fakeActions
	quit bool
}

func newHarness(t *testing.T, in Input, w, h int) *harness {
	t.Helper()
	act := &fakeActions{}
	hn := &harness{t: t, m: New(in, act), act: act}
	hn.feed(tea.WindowSizeMsg{Width: w, Height: h})
	hn.run(hn.m.Init())
	return hn
}

// feed delivers a message and synchronously executes the commands it returns.
func (h *harness) feed(msg tea.Msg) {
	_, cmd := h.m.Update(msg)
	h.run(cmd)
}

func (h *harness) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			h.run(c)
		}
	case tea.QuitMsg:
		h.quit = true
	default:
		h.feed(msg)
	}
}

func (h *harness) key(s string) {
	var k tea.KeyMsg
	switch s {
	case "up":
		k = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		k = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		k = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		k = tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		k = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		k = tea.KeyMsg{Type: tea.KeyEsc}
	case "space":
		k = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "pgdown":
		k = tea.KeyMsg{Type: tea.KeyPgDown}
	case "backspace":
		k = tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	h.feed(k)
}

func (h *harness) typ(s string) {
	for _, r := range s {
		h.key(string(r))
	}
}

func (h *harness) mouse(x, y int, a tea.MouseAction, b tea.MouseButton) {
	h.feed(tea.MouseMsg{X: x, Y: y, Action: a, Button: b})
}

func (h *harness) rowY(i int) int { return h.m.l.listY + i }

func (h *harness) view() string { return ansi.Strip(h.m.View()) }

func (h *harness) selected() string {
	h.t.Helper()
	f, _, ok := h.m.target()
	if !ok {
		return ""
	}
	if h.m.sel.f < 0 {
		return fmt.Sprintf("group %d", h.m.in.Groups[h.m.sel.g].ID)
	}
	return filepath.Base(f.Path)
}

func (h *harness) rows() []string {
	var out []string
	for _, r := range h.m.vis {
		if r.f < 0 {
			out = append(out, fmt.Sprintf("G%d", h.m.in.Groups[r.g].ID))
		} else {
			out = append(out, filepath.Base(h.m.in.Groups[r.g].Files[r.f].Path))
		}
	}
	return out
}

func (h *harness) button(action string) button {
	h.t.Helper()
	for _, b := range h.m.pane().btns {
		if b.action == action {
			return b
		}
	}
	h.t.Fatalf("no %q button on screen", action)
	return button{}
}

func eq(t *testing.T, got, want any) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}

func TestInitialStateAndNavigation(t *testing.T) {
	h := newHarness(t, fixture(), 100, 30)
	eq(t, h.rows(), []string{"G1", "video.mp4", "video-copy.mkv", "G2", "holiday.mov", "holiday-copy.mp4", "holiday.webm", "G3", "clip.mp4", "clip.mp4"})
	eq(t, h.selected(), "group 1")
	h.key("down")
	eq(t, h.selected(), "video.mp4")
	h.key("j")
	eq(t, h.selected(), "video-copy.mkv")
	h.key("up")
	h.key("k")
	h.key("k")
	eq(t, h.selected(), "group 1") // stays at the top
	for range 20 {
		h.key("down")
	}
	eq(t, h.m.indexOf(h.m.sel), len(h.m.vis)-1) // stops at the bottom
	h.key("g")
	eq(t, h.selected(), "group 1")
	h.key("G")
	eq(t, h.m.indexOf(h.m.sel), len(h.m.vis)-1)
}

func TestExpandCollapse(t *testing.T) {
	h := newHarness(t, fixture(), 100, 30)
	h.key("down") // video.mp4
	h.key("left") // -> parent group
	eq(t, h.selected(), "group 1")
	h.key("left") // collapse
	eq(t, h.rows(), []string{"G1", "G2", "holiday.mov", "holiday-copy.mp4", "holiday.webm", "G3", "clip.mp4", "clip.mp4"})
	h.key("right") // expand again
	eq(t, len(h.m.vis), 10)
	h.key("right") // enter the group
	eq(t, h.selected(), "video.mp4")
	h.key("space") // folds the group of the current row and selects the group
	eq(t, h.selected(), "group 1")
	eq(t, len(h.m.vis), 8)
	h.key("space")
	eq(t, len(h.m.vis), 10)
	h.key("enter") // Enter on a group folds it and never opens anything
	eq(t, len(h.m.vis), 8)
	eq(t, len(h.act.calls), 0)
}

func TestManyGroupsStartCollapsedAndListIsVirtualised(t *testing.T) {
	in := fixture()
	in.Groups = nil
	for i := 1; i <= 500; i++ {
		in.Groups = append(in.Groups, result.Group{ID: i, Type: model.Video, Class: result.Exact,
			Files: []model.File{file(fmt.Sprintf("/root/%d/a.mp4", i), 1), file(fmt.Sprintf("/root/%d/b.mp4", i), 1)}})
	}
	h := newHarness(t, in, 100, 30)
	eq(t, len(h.m.vis), 502) // 500 headers + the first group's two files
	view := h.m.View()
	eq(t, strings.Count(view, "\n")+1, 30)
	if strings.Contains(view, "GROUP 400") {
		t.Error("rows outside the viewport must not be rendered")
	}
	h.key("G")
	if !strings.Contains(h.m.View(), "GROUP 500") {
		t.Error("jumping to the end must scroll it into view")
	}
}

func TestFuzzyMatch(t *testing.T) {
	yes := [][2]string{{"hol", "/root/holiday.mov"}, {"HOLMOV", "/root/holiday.mov"}, {"h m", "holiday.mov"}, {"", "anything"}, {"日本", "/x/日本語.mp4"}}
	no := [][2]string{{"xyz", "holiday.mov"}, {"vom", "mov"}, {"holidayy", "holiday"}}
	for _, c := range yes {
		if _, ok := Match(c[0], c[1]); !ok {
			t.Errorf("%q should match %q", c[0], c[1])
		}
	}
	for _, c := range no {
		if _, ok := Match(c[0], c[1]); ok {
			t.Errorf("%q should not match %q", c[0], c[1])
		}
	}
	tight, _ := Match("hol", "holiday.mov")
	loose, _ := Match("hol", "h_o_l.mov")
	if tight <= loose {
		t.Errorf("consecutive matches should score higher: %d vs %d", tight, loose)
	}
}

func TestSearchFiltersWithoutDestroyingData(t *testing.T) {
	h := newHarness(t, fixture(), 100, 30)
	before := fmt.Sprint(h.m.in.Groups)
	h.key("/")
	if !h.m.searching {
		t.Fatal("/ should start search")
	}
	h.typ("webm")
	eq(t, h.rows(), []string{"G2", "holiday.webm"})
	for range 4 {
		h.key("backspace")
	}
	eq(t, len(h.m.vis), 10) // empty filter shows everything again
	h.typ("clip")
	eq(t, h.rows(), []string{"G3", "clip.mp4", "clip.mp4"})
	h.key("enter") // leave search, keep the filter
	if h.m.searching || h.m.filter != "clip" {
		t.Fatalf("searching=%v filter=%q", h.m.searching, h.m.filter)
	}
	h.key("q") // not in search mode any more: quits
	if !h.quit {
		t.Error("q should quit outside search")
	}
	h.quit = false
	h.key("esc") // clears the filter
	eq(t, len(h.m.vis), 10)
	eq(t, fmt.Sprint(h.m.in.Groups), before)

	h.key("/")
	h.typ("q") // typing q while searching must not quit
	if h.quit {
		t.Error("q typed in search quit the app")
	}
	h.key("esc")
	if h.m.searching || h.m.filter != "" {
		t.Error("esc should leave search and clear the filter")
	}
	h.key("/")
	h.typ("zzzzz")
	eq(t, len(h.m.vis), 0)
	if !strings.Contains(h.view(), "No matches") {
		t.Error("empty result should say so")
	}
	h.key("enter")
	h.key("enter") // nothing selected: must not panic or open
	eq(t, len(h.act.calls), 0)
}

func TestSearchTypingDDoesNotDelete(t *testing.T) {
	h := newHarness(t, fixture(), 100, 30)
	h.key("/")
	h.typ("d")
	if h.m.confirm != "" || h.m.filter != "d" {
		t.Fatalf("d typed in search must go to the box: confirm=%q filter=%q", h.m.confirm, h.m.filter)
	}
}

func TestSelectionStaysStableAcrossFilter(t *testing.T) {
	h := newHarness(t, fixture(), 100, 30)
	h.key("G") // last clip.mp4 in group 3
	h.key("/")
	h.typ("clip")
	eq(t, h.selected(), "clip.mp4")
	eq(t, h.m.sel, ref{2, 1})
	h.key("esc")
	h.key("/")
	h.typ("holiday")
	if h.m.sel.g != 1 {
		t.Fatalf("selection should land in the visible group, got %+v", h.m.sel)
	}
}

func TestHoverDoesNotChangeSelection(t *testing.T) {
	h := newHarness(t, fixture(), 100, 30)
	h.key("down") // video.mp4 selected
	h.mouse(5, h.rowY(4), tea.MouseActionMotion, tea.MouseButtonNone)
	eq(t, h.selected(), "video.mp4")
	if !h.m.hasHover || h.m.hover != (ref{1, 0}) {
		t.Fatalf("hover = %+v %v", h.m.hover, h.m.hasHover)
	}
	h.key("enter")
	eq(t, h.act.calls, []string{"open /root/video.mp4"})
	h.mouse(90, 1, tea.MouseActionMotion, tea.MouseButtonNone)
	if h.m.hasHover {
		t.Error("hover should clear outside the list")
	}
}

func TestHoverNeverChangesWhatDeleteTargets(t *testing.T) {
	h := newHarness(t, fixture(), 100, 30)
	h.key("down") // video.mp4
	h.mouse(5, h.rowY(5), tea.MouseActionMotion, tea.MouseButtonNone)
	h.key("d")
	eq(t, h.m.confirm, "/root/video.mp4")
}

func TestClickSelects(t *testing.T) {
	h := newHarness(t, fixture(), 100, 30)
	h.mouse(5, h.rowY(5), tea.MouseActionPress, tea.MouseButtonLeft)
	eq(t, h.selected(), "holiday-copy.mp4")
	eq(t, len(h.act.calls), 0)
	h.mouse(5, h.rowY(3), tea.MouseActionPress, tea.MouseButtonLeft) // group header: selects and folds
	eq(t, h.selected(), "group 2")
	eq(t, len(h.m.vis), 7)
	h.mouse(5, h.rowY(15), tea.MouseActionPress, tea.MouseButtonLeft)
	eq(t, h.selected(), "group 2")
}

func TestWheelScrollsListWithoutMovingSelection(t *testing.T) {
	in := fixture()
	in.Groups = nil
	for i := 1; i <= 100; i++ {
		in.Groups = append(in.Groups, result.Group{ID: i, Class: result.Exact, Type: model.Video,
			Files: []model.File{file("/root/a", 1), file("/root/b", 1)}})
	}
	h := newHarness(t, in, 100, 30)
	sel := h.m.sel
	h.mouse(5, 10, tea.MouseActionPress, tea.MouseButtonWheelDown)
	if h.m.offset != 3 {
		t.Fatalf("offset = %d", h.m.offset)
	}
	h.mouse(5, 10, tea.MouseActionPress, tea.MouseButtonWheelUp)
	h.mouse(5, 10, tea.MouseActionPress, tea.MouseButtonWheelUp)
	eq(t, h.m.offset, 0)
	eq(t, h.m.sel, sel)
}

func TestButtonsActOnSelection(t *testing.T) {
	h := newHarness(t, fixture(), 120, 30)
	h.key("down")
	h.key("down") // video-copy.mkv
	h.mouse(5, h.rowY(4), tea.MouseActionMotion, tea.MouseButtonNone)
	for _, a := range []string{"open", "reveal", "copy"} {
		b := h.button(a)
		h.mouse(b.x0+1, b.y, tea.MouseActionPress, tea.MouseButtonLeft)
	}
	eq(t, h.act.calls, []string{"open /root/video-copy.mkv", "reveal /root/video-copy.mkv", "copy /root/video-copy.mkv"})
	h.act.calls = nil
	last := h.button("delete")
	h.mouse(last.x1, last.y, tea.MouseActionPress, tea.MouseButtonLeft) // one cell past the last button
	eq(t, len(h.act.calls), 0)
	eq(t, h.m.confirm, "")
}

func TestDeleteButtonIsOnTheSameRowAsCopyAndRed(t *testing.T) {
	h := newHarness(t, fixture(), 120, 30)
	h.key("down")
	cp, del := h.button("copy"), h.button("delete")
	if cp.y != del.y || del.x0 <= cp.x1 {
		t.Fatalf("Delete must follow Copy path on one line: copy=%+v delete=%+v", cp, del)
	}
	if !del.danger {
		t.Error("delete must be styled as dangerous")
	}
	if !strings.Contains(h.view(), "[ Delete D ]") {
		t.Error("button missing from the screen")
	}
}

func TestButtonsWrapOnNarrowTerminalsAndStayClickable(t *testing.T) {
	h := newHarness(t, fixture(), 70, 30)
	h.key("down")
	ys := map[int]bool{}
	for _, b := range h.m.pane().btns {
		ys[b.y] = true
		if b.x1 > h.m.l.w-1 {
			t.Fatalf("button %+v runs off the right edge", b)
		}
	}
	if len(ys) < 2 {
		t.Fatalf("expected the buttons to wrap onto a second row at 70 columns, rows=%v", ys)
	}
	b := h.button("delete")
	h.mouse(b.x0+1, b.y, tea.MouseActionPress, tea.MouseButtonLeft)
	eq(t, h.m.confirm, "/root/video.mp4")
}

// ---- delete -----------------------------------------------------------------

func TestDeleteRequiresConfirmationAndThenTrashesAndUpdatesUI(t *testing.T) {
	h := newHarness(t, fixture(), 110, 30)
	h.key("j") // group 1 -> video.mp4
	h.key("j") // video-copy.mkv
	h.key("d")
	eq(t, h.m.confirm, "/root/video-copy.mkv")
	eq(t, len(h.act.calls), 0) // nothing deleted yet
	v := h.view()
	for _, want := range []string{"Delete “video-copy.mkv”?", "Move to the Trash?", "Enter = confirm", "Enter Confirm delete"} {
		if !strings.Contains(v, want) {
			t.Errorf("confirmation prompt missing %q:\n%s", want, v)
		}
	}
	h.key("enter")
	eq(t, h.act.calls, []string{"trash /root/video-copy.mkv"})
	eq(t, h.m.confirm, "")
	if !strings.Contains(h.view(), "Moved video-copy.mkv to the Trash") {
		t.Error("success message missing")
	}
	// Group 1 had two files: only one is left, so it is no longer a duplicate set.
	eq(t, h.rows(), []string{"G2", "holiday.mov", "holiday-copy.mp4", "holiday.webm", "G3", "clip.mp4", "clip.mp4"})
	eq(t, h.m.sum.Groups, 2)
	eq(t, h.selected(), "group 2")
}

func TestDeleteFromBiggerGroupKeepsGroupAndSelectsNeighbour(t *testing.T) {
	h := newHarness(t, fixture(), 110, 40)
	for range 5 { // G1, video, video-copy, G2, holiday.mov, holiday-copy.mp4
		h.key("j")
	}
	eq(t, h.selected(), "holiday-copy.mp4")
	h.key("d")
	h.key("enter")
	eq(t, h.rows(), []string{"G1", "video.mp4", "video-copy.mkv", "G2", "holiday.mov", "holiday.webm", "G3", "clip.mp4", "clip.mp4"})
	eq(t, h.selected(), "holiday.webm") // the next file takes its place
	eq(t, h.m.in.Groups[1].ID, 2)       // group numbers are stable
}

func TestDeleteLastTwoFilesRemovesGroupButAllowsDeletingAnyFile(t *testing.T) {
	// The user can delete even the original or the last "unique" file: just confirm.
	h := newHarness(t, fixture(), 110, 40)
	h.key("j")
	h.key("d")
	h.key("enter")
	h.key("j") // after group 1 vanished the selection is on group 2's header... move to a file
	if h.act.calls[0] != "trash /root/video.mp4" {
		t.Fatalf("calls = %v", h.act.calls)
	}
	// Delete everything that is left, one confirmation each.
	for len(h.m.in.Groups) > 0 {
		for h.m.sel.f < 0 {
			h.key("j")
		}
		h.key("d")
		h.key("enter")
	}
	if !strings.Contains(h.view(), "No duplicate groups left") {
		t.Errorf("empty state missing:\n%s", h.view())
	}
	h.key("d") // nothing selected: must not panic
	h.key("enter")
}

func TestDeleteCancelledByAnyOtherKey(t *testing.T) {
	for _, k := range []string{"esc", "n", "q", "d", "x", "down"} {
		h := newHarness(t, fixture(), 110, 30)
		h.key("j")
		h.key("d")
		h.key(k)
		if h.m.confirm != "" || len(h.act.calls) != 0 {
			t.Errorf("key %q: confirm=%q calls=%v", k, h.m.confirm, h.act.calls)
		}
		if h.quit {
			t.Errorf("key %q must cancel, not quit", k)
		}
		if !strings.Contains(h.view(), "Delete cancelled") {
			t.Errorf("key %q: no cancel message", k)
		}
		if len(h.m.in.Groups) != 3 {
			t.Errorf("key %q changed the data", k)
		}
	}
}

func TestDeleteConfirmCancelledByClickElsewhere(t *testing.T) {
	h := newHarness(t, fixture(), 110, 30)
	h.key("j")
	h.key("d")
	h.mouse(5, h.rowY(5), tea.MouseActionPress, tea.MouseButtonLeft)
	if h.m.confirm != "" || len(h.act.calls) != 0 {
		t.Fatal("a click must cancel the pending delete")
	}
}

func TestDeleteOnGroupRowIsRefused(t *testing.T) {
	h := newHarness(t, fixture(), 110, 30)
	eq(t, h.selected(), "group 1")
	h.key("d")
	if h.m.confirm != "" {
		t.Fatal("a group row must not start a delete")
	}
	if !strings.Contains(h.view(), "Select a file first") {
		t.Error("no explanation shown")
	}
}

func TestDeleteFailureKeepsFileAndShowsError(t *testing.T) {
	h := newHarness(t, fixture(), 110, 30)
	h.act.trashErr = errors.New("Finder: the volume is read only")
	h.key("j")
	h.key("d")
	h.key("enter")
	if len(h.m.in.Groups) != 3 || len(h.m.in.Groups[0].Files) != 2 {
		t.Fatal("a failed delete must not change the list")
	}
	if v := h.view(); !strings.Contains(v, "Could not move video.mp4 to the Trash") || !strings.Contains(v, "read only") {
		t.Fatalf("failure not shown:\n%s", v)
	}
}

func TestDeleteDoesNotMutateCallersData(t *testing.T) {
	in := fixture()
	orig := fmt.Sprint(in.Groups)
	h := newHarness(t, in, 110, 30)
	h.key("j")
	h.key("d")
	h.key("enter")
	eq(t, fmt.Sprint(in.Groups), orig)
}

func TestDeleteIsInTheFooterAndHelp(t *testing.T) {
	h := newHarness(t, fixture(), 100, 30)
	if !strings.Contains(h.view(), "D Delete") {
		t.Error("footer should list D")
	}
	h.key("?")
	if v := h.view(); !strings.Contains(v, "Trash") {
		t.Errorf("help should explain delete:\n%s", v)
	}
}

// ---- statistics ---------------------------------------------------------------

func TestHeaderShowsCountsTimeAndDuplicateStorage(t *testing.T) {
	h := newHarness(t, fixture(), 140, 30)
	v := h.view()
	// Group 1: 100 B original + 100 B duplicate; group 2: 1.2 GB original, 390 MB duplicates; group 3: 5 + 5.
	for _, want := range []string{
		"VEYRA", "VIDEO SCAN COMPLETE", "/root",
		"3,000 files examined", "2,450 video files", "3 groups", "7 matched files", "4 duplicates",
		"371.9 MiB duplicates · 2m 31s taken",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("header missing %q:\n%s", want, v)
		}
	}
}

func TestDuplicateStorageIgnoresOriginalAndUpdatesAfterDelete(t *testing.T) {
	h := newHarness(t, fixture(), 140, 30)
	const g2dups = 300_000_000 + 90_000_000
	eq(t, h.m.sum.DuplicateBytes, int64(100+g2dups+5))
	// Delete the original of group 2 (the 1.2 GB file): the next-largest becomes the original.
	for range 4 {
		h.key("j")
	}
	eq(t, h.selected(), "holiday.mov")
	h.key("d")
	h.key("enter")
	eq(t, h.m.in.Groups[1].Files[h.m.in.Groups[1].OriginalIndex()].Path, "/root/sub/holiday-copy.mp4")
	eq(t, h.m.sum.DuplicateBytes, int64(100+90_000_000+5))
	eq(t, h.m.sum.Duplicates, 3)
	if !strings.Contains(h.view(), "85.8 MiB duplicates") {
		t.Errorf("header not refreshed:\n%s", h.view())
	}
}

func TestOriginalIsMarkedInTheListOnly(t *testing.T) {
	h := newHarness(t, fixture(), 120, 40)
	v := h.view()
	if strings.Count(v, "★ ") < 3 { // one original per group, marked in the list
		t.Errorf("each group should mark its original in the list:\n%s", v)
	}
	if !strings.Contains(v, "frees") {
		t.Error("group rows should still show what they free")
	}
	// The FILE panel and the metadata must not repeat it (user request).
	for _, k := range []string{"j", "j", "j", "j", "j", "k"} { // a group header, the original, a duplicate
		h.key(k)
		v := h.view()
		for _, banned := range []string{
			"ORIGINAL", "The largest file in this group", "the largest file in this group", "largest file is the original",
			"DUPLICATE:", "Duplicates in this group use", "the original is",
		} {
			if strings.Contains(v, banned) {
				t.Fatalf("after %q the screen still contains %q:\n%s", k, banned, v)
			}
		}
	}
}

func TestMetadataHasNoRoleOrFormatRows(t *testing.T) {
	h := newHarness(t, fixture(), 120, 44)
	for _, k := range []string{"j", "j", "j", "j", "j"} {
		h.key(k)
		for _, ln := range h.rightColumn() {
			if strings.HasPrefix(ln, "Role") || strings.HasPrefix(ln, "Format") {
				t.Fatalf("metadata still has a row it should not: %q", ln)
			}
		}
	}
	v := h.view()
	for _, keep := range []string{"Name", "Location", "Size", "Extension", "File created", "File modified", "Resolution", "Duration", "Codec", "Match"} {
		if !strings.Contains(v, keep) {
			t.Errorf("metadata lost %q", keep)
		}
	}
}

func TestHeaderWrapsInsteadOfCuttingStatistics(t *testing.T) {
	h := newHarness(t, fixture(), 64, 30)
	v := h.view()
	for _, want := range []string{"371.9 MiB duplicates · 2m 31s taken", "files examined"} {
		if !strings.Contains(v, want) {
			t.Errorf("narrow header lost %q:\n%s", want, v)
		}
	}
	if len(h.m.header) < 4 {
		t.Errorf("expected the statistics to wrap onto extra lines, header has %d", len(h.m.header))
	}
}

func TestSkippedAndPartialShown(t *testing.T) {
	in := fixture()
	in.Partial, in.Skipped = true, 3
	h := newHarness(t, in, 140, 30)
	v := h.view()
	for _, want := range []string{"VIDEO SCAN PARTIAL", "3 skipped", "Scan was partial"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// ---- metadata ---------------------------------------------------------------------

func TestMetadataFollowsSelection(t *testing.T) {
	h := newHarness(t, fixture(), 120, 44)
	for range 4 {
		h.key("j")
	}
	v := h.view()
	for _, want := range []string{"holiday.mov", "/root/holiday.mov", "1.1 GiB", "1920 × 1080", "02:15", "h264", "29.97 fps", "2.4 Mb/s", "aac", "Group 02 · VISUAL · 3 files", "durations differ", "2026-10-20 09:30:00", "(as named)"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q", want)
		}
	}
	h.key("j")
	v = h.view()
	if !strings.Contains(v, "286.1 MiB") || strings.Contains(v, "1.1 GiB (") {
		t.Errorf("metadata did not update for the new selection:\n%s", v)
	}
}

// rightColumn returns each screen line with the left panel and the right
// panel's borders removed.
func (h *harness) rightColumn() []string {
	var out []string
	for _, ln := range strings.Split(h.view(), "\n") {
		r := ansi.TruncateLeft(ln, h.m.l.leftW+1, "")
		r = strings.TrimRight(r, " ")
		r = strings.TrimSuffix(r, "│")
		out = append(out, strings.TrimRight(r, " "))
	}
	return out
}

func TestMetadataShowsLongValuesInFullByWrapping(t *testing.T) {
	long := "/Volumes/HD1/Movies/@Vijay/www.1TamilMV.fi - Good Bad Ugly (2025) Tamil Proper HQ HDRip - 1080p - HEVC - (DD+5.1 - 192Kbps & AAC) - 1.6GB - ESub.mkv"
	in := fixture()
	in.Groups[0].Files[0] = file(long, 5000)
	for _, size := range [][2]int{{80, 60}, {100, 60}, {140, 60}} {
		h := newHarness(t, in, size[0], size[1])
		h.key("j")
		right := h.rightColumn()
		for _, ln := range right {
			if strings.Contains(ln, "…") {
				t.Errorf("%dx%d: the right column was cut off with an ellipsis: %q", size[0], size[1], ln)
			}
		}
		// Reassemble the Location value from its wrapped lines.
		var parts []string
		collecting := false
		for _, ln := range right {
			switch {
			case strings.HasPrefix(ln, "Location"):
				collecting = true
				parts = append(parts, strings.TrimPrefix(ln, "Location"))
			case collecting && strings.HasPrefix(ln, " "):
				parts = append(parts, ln)
			case collecting:
				collecting = false
			}
		}
		got := strings.ReplaceAll(strings.Join(parts, ""), " ", "")
		if want := strings.ReplaceAll(long, " ", ""); got != want {
			t.Errorf("%dx%d: path not shown in full\n got  %s\n want %s", size[0], size[1], got, want)
		}
		if len(parts) < 2 && size[0] == 80 {
			t.Errorf("a path this long should wrap onto several lines at 80 columns, got %d", len(parts))
		}
	}
}

func TestMetadataScrollsWhenItCannotFit(t *testing.T) {
	long := "/Volumes/HD1/Movies/" + strings.Repeat("very long folder name ", 12) + "/film.mkv"
	in := fixture()
	in.Groups[0].Files[0] = file(long, 5000)
	h := newHarness(t, in, 80, 24)
	h.key("j")
	p := h.m.pane()
	if len(p.meta) <= p.metaRows {
		t.Skip("everything fits at this size")
	}
	if !strings.Contains(h.view(), "lines 1-") {
		t.Error("title should say the metadata is clipped and how to scroll")
	}
	first := h.view()
	h.key("]")
	if h.m.metaOff != 3 || h.view() == first {
		t.Fatalf("] should scroll the metadata: off=%d", h.m.metaOff)
	}
	h.mouse(h.m.l.rightX+3, p.metaY+2, tea.MouseActionPress, tea.MouseButtonWheelDown)
	eq(t, h.m.metaOff, 6)
	h.mouse(h.m.l.rightX+3, p.metaY+2, tea.MouseActionPress, tea.MouseButtonWheelUp)
	h.key("[")
	h.key("[")
	h.key("[")
	eq(t, h.m.metaOff, 0)
	for range 40 {
		h.key("]")
	}
	if h.m.metaOff != len(p.meta)-p.metaRows {
		t.Errorf("scroll must stop at the end: off=%d max=%d", h.m.metaOff, len(p.meta)-p.metaRows)
	}
	if !strings.Contains(ansi.Strip(h.m.View()), "film.mkv") {
		t.Error("the end of the path must be reachable")
	}
	h.key("down") // a new selection starts from the top again
	eq(t, h.m.metaOff, 0)
	// The list wheel still scrolls the list, not the metadata.
	h.mouse(5, h.rowY(2), tea.MouseActionPress, tea.MouseButtonWheelDown)
	eq(t, h.m.metaOff, 0)
}

func TestMetadataForImageAndUnknowns(t *testing.T) {
	in := Input{Root: "/p", Type: model.Image, Scanned: 2, Groups: []result.Group{{ID: 1, Type: model.Image, Class: result.Exact,
		Files: []model.File{{Path: "/p/noext", Size: 10, Meta: model.Meta{Format: "png", Width: 30, Height: 20, ColorModel: "RGB(A) (8-bit)"}}, {Path: "/p/b.png", Size: 10}}}}}
	h := newHarness(t, in, 100, 36)
	h.key("j")
	v := h.view()
	for _, want := range []string{"30 × 20", "RGB(A) (8-bit)", "(none)", "Dimensions"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(v, "Duration") {
		t.Error("image metadata must not show video fields")
	}
	h.key("j")
	if !strings.Contains(h.view(), "Unknown") {
		t.Error("unknown values should read Unknown")
	}
}

func TestMissingFileIsReported(t *testing.T) {
	h := newHarness(t, fixture(), 120, 44)
	h.key("j")
	if !strings.Contains(h.view(), "File unavailable") {
		t.Error("a file that cannot be stat'ed should be flagged")
	}
}

func TestLabelsNeverTouchValues(t *testing.T) {
	h := newHarness(t, fixture(), 100, 40)
	h.key("j")
	v := h.view()
	for _, bad := range []string{"File created…", "File modifi…", "Dimensions1"} {
		if strings.Contains(v, bad) {
			t.Errorf("label truncated or glued to value: %q", bad)
		}
	}
	if !strings.Contains(v, "File modified") {
		t.Error("File modified label should be shown in full")
	}
}

// ---- layout ----------------------------------------------------------------------------

func TestResizeKeepsExactDimensions(t *testing.T) {
	h := newHarness(t, fixture(), 100, 30)
	h.key("j")
	for _, sz := range [][2]int{{60, 22}, {61, 23}, {80, 24}, {100, 30}, {132, 43}, {200, 60}, {300, 80}} {
		h.feed(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		view := h.m.View()
		lines := strings.Split(view, "\n")
		if len(lines) != sz[1] {
			t.Fatalf("%dx%d: %d lines", sz[0], sz[1], len(lines))
		}
		for i, ln := range lines {
			if w := ansi.StringWidth(ln); w != sz[0] {
				t.Fatalf("%dx%d: line %d is %d cells wide: %q", sz[0], sz[1], i, w, ansi.Strip(ln))
			}
		}
		if strings.Contains(view, "too small") {
			t.Errorf("%dx%d should be usable", sz[0], sz[1])
		}
	}
}

func TestSmallTerminalFallback(t *testing.T) {
	h := newHarness(t, fixture(), 100, 30)
	for _, sz := range [][2]int{{59, 30}, {100, 15}, {20, 5}, {1, 1}, {60, 17}} {
		h.feed(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		lines := strings.Split(h.m.View(), "\n")
		if len(lines) > sz[1] {
			t.Fatalf("%v: %d lines exceed the terminal", sz, len(lines))
		}
		for _, ln := range lines {
			if ansi.StringWidth(ln) > sz[0] {
				t.Fatalf("%v: line too wide: %q", sz, ln)
			}
		}
		h.key("down")
		h.key("d") // must not start a delete the user cannot see
		h.key("enter")
		h.mouse(2, 2, tea.MouseActionPress, tea.MouseButtonLeft)
		if h.m.confirm != "" || len(h.act.calls) != 0 {
			t.Fatalf("%v: the too-small screen must not act: confirm=%q calls=%v", sz, h.m.confirm, h.act.calls)
		}
	}
	h.feed(tea.WindowSizeMsg{Width: 70, Height: 22})
	h.key("q")
	if !h.quit {
		t.Error("q must still quit from the small-terminal screen")
	}
	h.feed(tea.WindowSizeMsg{Width: 100, Height: 30})
	if strings.Contains(h.m.View(), "too small") {
		t.Error("growing the window should restore the UI")
	}
}

func TestLongNamesDoNotBreakLayout(t *testing.T) {
	long := "/root/" + strings.Repeat("very long directory 日本語 ", 6) + "/" + strings.Repeat("名前", 40) + ".mp4"
	in := fixture()
	in.Groups[0].Files[0] = file(long, 1)
	h := newHarness(t, in, 80, 30)
	h.key("j")
	for i, ln := range strings.Split(h.m.View(), "\n") {
		if w := ansi.StringWidth(ln); w != 80 {
			t.Fatalf("line %d is %d wide", i, w)
		}
	}
	h.key("c")
	eq(t, h.act.calls, []string{"copy " + long})
}

func TestKeysOnGroupTargetFirstFile(t *testing.T) {
	h := newHarness(t, fixture(), 100, 30)
	h.key("o")
	h.key("c")
	h.key("p")
	eq(t, h.act.calls, []string{"reveal /root/video.mp4", "copy /root/video.mp4", "ql /root/video.mp4"})
}

func TestNativeActionFailuresShowInTUI(t *testing.T) {
	h := newHarness(t, fixture(), 100, 30)
	h.key("down")
	h.act.err = errors.New("open: file no longer exists")
	h.key("enter")
	view := h.view()
	if !strings.Contains(view, "Could not do that with video.mp4") || !strings.Contains(view, "no longer exists") {
		t.Fatalf("open failure not shown:\n%s", view)
	}
	h.key("down")
	if strings.Contains(h.view(), "Could not do that") {
		t.Error("status should clear on the next key")
	}
	h.key("o")
	if !strings.Contains(h.view(), "Could not do that with video-copy.mkv") {
		t.Error("Finder reveal failure not shown")
	}
	h.act.err = nil
	h.key("c")
	if !strings.Contains(h.view(), "Copied path of video-copy.mkv") {
		t.Error("success message missing")
	}
}

func TestHelpScreen(t *testing.T) {
	h := newHarness(t, fixture(), 100, 36)
	h.key("?")
	if !strings.Contains(h.view(), "Reveal in Finder") {
		t.Error("help should list the keymap")
	}
	h.key("x")
	if strings.Contains(h.m.View(), "HELP") || h.quit {
		t.Error("help should close on any key")
	}
}

func TestQuit(t *testing.T) {
	h := newHarness(t, fixture(), 100, 30)
	h.key("q")
	if !h.quit {
		t.Fatal("q must quit")
	}
	h2 := newHarness(t, fixture(), 100, 30)
	h2.feed(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !h2.quit {
		t.Fatal("ctrl+c must quit")
	}
	h3 := newHarness(t, fixture(), 100, 30)
	h3.key("j")
	h3.key("d")
	h3.feed(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !h3.quit {
		t.Fatal("ctrl+c must quit even while a delete is pending")
	}
	if len(h3.act.calls) != 0 {
		t.Fatal("quitting must not delete")
	}
}

func TestFooterFitsAtCommonWidths(t *testing.T) {
	for _, w := range []int{80, 100, 120} {
		h := newHarness(t, fixture(), w, 30)
		if v := h.view(); !strings.Contains(v, "Q Quit") || !strings.Contains(v, "? Help") || !strings.Contains(v, "D Del") {
			t.Errorf("width %d: footer cuts off Help/Quit/Delete", w)
		}
	}
}

func TestEmptyInputDoesNotPanic(t *testing.T) {
	h := newHarness(t, Input{Root: "/r", Type: model.Image}, 100, 30)
	for _, k := range []string{"down", "up", "left", "right", "enter", "space", "o", "c", "p", "d", "G", "pgdown", "[", "]"} {
		h.key(k)
	}
	h.mouse(5, 5, tea.MouseActionPress, tea.MouseButtonLeft)
	_ = h.m.View()
	eq(t, len(h.act.calls), 0)
}

// ---- metadata panel robustness (regressions found while building the layout) ----

// Whatever the window height, scrolling by key reaches every metadata line: a
// step larger than the window would skip lines that could then never be seen.
func TestMetadataScrollNeverSkipsLines(t *testing.T) {
	for _, height := range []int{18, 19, 20, 22, 24} {
		h := newHarness(t, fixture(), 60, height)
		h.key("j")
		p := h.m.pane()
		if len(p.meta) <= p.metaRows {
			continue
		}
		seen := map[int]bool{}
		for range len(p.meta) + 4 {
			off := max(0, min(h.m.metaOff, len(p.meta)-p.metaRows))
			for i := off; i < off+p.metaRows && i < len(p.meta); i++ {
				seen[i] = true
			}
			h.key("]")
		}
		for i := range p.meta {
			if !seen[i] {
				t.Errorf("height %d: metadata line %d (%q) can never be seen (window %d rows)", height, i, ansi.Strip(p.meta[i]), p.metaRows)
			}
		}
	}
}

// Labels are never truncated, whatever the width.
func TestMetadataLabelsAreNeverCut(t *testing.T) {
	labels := []string{"File created", "File modified", "Resolution", "Extension", "Duration", "Frame rate"}
	for _, width := range []int{60, 64, 70, 80, 100, 140} {
		h := newHarness(t, fixture(), width, 60) // tall enough that nothing needs scrolling
		h.key("j")
		// The panels only: the header shows the folder on one line and may shorten it.
		for _, ln := range h.rightColumn()[h.m.l.bodyY : h.m.l.bodyY+h.m.l.bodyH] {
			if strings.Contains(ln, "…") {
				t.Errorf("width %d: truncated text in the right panels: %q", width, ln)
			}
		}
		v := h.view()
		for _, label := range labels {
			if !strings.Contains(v, label) {
				t.Errorf("width %d: label %q is cut or missing", width, label)
			}
		}
	}
}

// At the smallest supported size the panel is short, but nothing is lost: the
// buttons stay, and scrolling brings every metadata row into view.
func TestSmallestTerminalKeepsEverythingReachable(t *testing.T) {
	h := newHarness(t, fixture(), 60, 18)
	h.key("j")
	v := h.view()
	if strings.Contains(v, "too small") {
		t.Fatal("60x18 should be usable")
	}
	for _, want := range []string{"[ Preview", "METADATA", "lines 1-"} {
		if !strings.Contains(v, want) {
			t.Errorf("at the smallest size %q must still be visible:\n%s", want, v)
		}
	}
	labels := []string{"Name", "Location", "Size", "Extension", "File created", "File modified", "Resolution", "Duration", "Codec", "Match"}
	seen := map[string]bool{}
	for range 14 {
		for _, label := range labels {
			if strings.Contains(h.view(), label) {
				seen[label] = true
			}
		}
		h.key("]")
	}
	for _, label := range labels {
		if !seen[label] {
			t.Errorf("%q never came into view while scrolling", label)
		}
	}
}
