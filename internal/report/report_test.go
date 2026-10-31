package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/RishiKendai/veyra/internal/model"
	"github.com/RishiKendai/veyra/internal/result"
)

func TestWrite(t *testing.T) {
	groups := []result.Group{
		{ID: 1, Type: model.Video, Class: result.Exact, Files: []model.File{
			{Path: "/m/a.mkv", Size: 1 << 30}, {Path: "/m/b.mkv", Size: 1 << 30}}},
		{ID: 2, Type: model.Video, Class: result.Visual, Files: []model.File{
			{Path: "/m/c.mkv", Size: 500 << 20}, {Path: "/m/d.mkv", Size: 2 << 30}, {Path: "/m/e.mkv", Size: 100 << 20}},
			Warnings: []string{"durations differ"}},
	}
	var b bytes.Buffer
	Write(&b, Info{Type: model.Video, Examined: 1094, Scanned: 956, Elapsed: 151 * time.Second}, groups)
	out := b.String()
	for _, want := range []string{
		"956 video files found (1,094 files examined)",
		"2 duplicate groups, 5 matched files, 3 duplicates using 1.6 GiB · 2m 31s taken",
		"GROUP 01 · EXACT · 2 files · 1.0 GiB duplicates",
		"/m/a.mkv  (1.0 GiB)  [original]", // tie: first path is the original
		"GROUP 02 · VISUAL · 3 files · 600.0 MiB duplicates",
		"/m/d.mkv  (2.0 GiB)  [original]", // the largest file
		"! durations differ",
		"\n\nGROUP 02", // blank line between groups
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "/m/b.mkv  (1.0 GiB)  [original]") || strings.Contains(out, "/m/c.mkv  (500.0 MiB)  [original]") {
		t.Error("only one original per group")
	}
	var empty bytes.Buffer
	Write(&empty, Info{Type: model.Image, Scanned: 3}, nil)
	if !strings.Contains(empty.String(), "0 duplicate groups") || strings.Contains(empty.String(), "original") {
		t.Errorf("empty result: %q", empty.String())
	}
}
