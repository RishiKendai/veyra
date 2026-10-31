package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/RishiKendai/veyra/internal/format"
	"github.com/RishiKendai/veyra/internal/model"
	"github.com/RishiKendai/veyra/internal/result"
)

// pane is everything drawn in the right column for the current state. It is
// computed from the model on demand so drawing and mouse hit-testing agree.
type pane struct {
	top      []string // inner lines of the top ("FILE") panel
	topH     int      // outer height of the top panel
	btns     []button // absolute screen coordinates
	meta     []string // every metadata line, wrapped to the panel width, never truncated
	metaY    int      // screen row where the metadata panel starts
	metaH    int      // its outer height
	metaRows int      // rows available inside it
}

func (m *Model) pane() pane {
	l := m.l
	w := l.rightInner
	f, g, ok := m.target()
	var p pane

	build := func(compact bool) (lines []string, btnRow int) {
		if !ok {
			return []string{sDim.Render("Nothing selected")}, -1
		}
		name := wrapText(filepath.Base(f.Path), w)
		if limit := map[bool]int{false: 3, true: 1}[compact]; len(name) > limit {
			name = append(name[:limit-1], name[limit-1]+"…")
		}
		for _, ln := range name {
			lines = append(lines, sBold.Render(ln))
		}
		if !compact {
			lines = append(lines, wrapText(fmt.Sprintf("Group %02d · %s · %d files · %s", g.ID, g.Class, len(g.Files), g.Type), w)...)
			for _, warn := range g.Warnings {
				for _, ln := range wrapText("! "+warn, w) {
					lines = append(lines, sWarn.Render(ln))
				}
			}
		}
		if m.confirm != "" {
			// msg := fmt.Sprintf("Move “%s” (%s) to the Trash?  Enter = confirm · Esc = cancel", filepath.Base(m.confirm), format.Size(f.Size))
			msg := "Move to the Trash?  Enter = confirm · Esc = cancel"
			for _, ln := range wrapText(msg, w) {
				lines = append(lines, sDanger.Render(ln))
			}
			return lines, -1
		}
		return lines, len(lines)
	}

	lines, btnRow := build(false)
	if len(lines)+2 > l.bodyH-6 { // leave the metadata panel room to breathe
		lines, btnRow = build(true)
	}

	if btnRow >= 0 {
		btns, rows := packButtons(l.rightX+1, w, l.bodyY+1+btnRow)
		p.btns = btns
		for r := range rows {
			var sb strings.Builder
			x := l.rightX + 1
			for _, b := range btns {
				if b.y != l.bodyY+1+btnRow+r {
					continue
				}
				sb.WriteString(strings.Repeat(" ", b.x0-x))
				style := sButton
				if b.danger {
					style = sDanger
				}
				sb.WriteString(style.Render(b.label))
				x = b.x1
			}
			lines = append(lines, sb.String())
		}
	}
	p.top = lines
	p.topH = min(len(lines)+2, max(l.bodyH-minBody/2, 4))
	p.metaY = l.bodyY + p.topH
	p.metaH = l.bodyH - p.topH
	p.metaRows = max(p.metaH-2, 0)
	p.meta = m.metaLines(w)
	return p
}

// metaLines lists every metadata field with its value wrapped under the label
// column, so long paths and titles are shown in full.
func (m *Model) metaLines(w int) []string {
	f, g, ok := m.target()
	if !ok {
		return nil
	}
	// Wide enough for the longest label ("File modified") whenever there is room;
	// the value column takes the rest and wraps.
	lw := min(15, max(8, w-12))
	vw := max(w-lw, 8)
	var out []string
	kv := func(k, v string) {
		if v == "" {
			v = "Unknown"
		}
		for i, ln := range wrapText(v, vw) {
			label := ""
			if i == 0 {
				label = k
			}
			out = append(out, sDim.Render(fit(label, lw))+ln)
		}
	}
	kv("Name", filepath.Base(f.Path))
	kv("Location", f.Path)
	kv("Size", fmt.Sprintf("%s (%s bytes)", format.Size(f.Size), format.Int(int(f.Size))))
	ext := filepath.Ext(f.Path)
	if ext == "" {
		ext = "(none)"
	}
	kv("Extension", ext+"  (as named)")
	kv("File created", m.created(f))
	kv("File modified", f.ModTime.Format("2006-01-02 15:04:05"))
	if g.Type == model.Image {
		kv("Dimensions", dims(f))
		kv("Colour", f.ColorModel)
	} else {
		kv("Resolution", dims(f))
		kv("Duration", format.Duration(f.Duration))
		kv("Codec", f.Codec)
		fps := ""
		if f.FrameRate > 0 {
			fps = fmt.Sprintf("%.2f fps", f.FrameRate)
		}
		kv("Frame rate", fps)
		kv("Bitrate", format.Bitrate(f.BitRate))
		audio := "None"
		if f.AudioCodec != "" {
			audio = f.AudioCodec
		}
		kv("Audio", audio)
	}
	match := "Exact: byte-identical (BLAKE3)"
	if g.Class == result.Visual {
		match = "Visual: same picture/footage after re-encoding or resizing"
	}
	kv("Match", match)
	if inf, ok := m.info[f.Path]; ok && inf.err != nil {
		for _, ln := range wrapText("File unavailable: "+inf.err.Error(), w) {
			out = append(out, sErr.Render(ln))
		}
	}
	return out
}

func dims(f model.File) string {
	if f.Width <= 0 || f.Height <= 0 {
		return ""
	}
	return fmt.Sprintf("%d × %d", f.Width, f.Height)
}

func (m *Model) created(f model.File) string {
	inf, ok := m.info[f.Path]
	switch {
	case !ok:
		return "…"
	case inf.err != nil || inf.birth.IsZero():
		return "Unknown"
	}
	return inf.birth.Format(time.DateTime)
}
