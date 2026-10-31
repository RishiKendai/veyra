package result

import (
	"testing"

	"github.com/RishiKendai/veyra/internal/model"
)

func f(w, h int, dur float64) model.File {
	return model.File{Meta: model.Meta{Width: w, Height: h, Duration: dur}}
}

func TestAnalyze(t *testing.T) {
	tests := []struct {
		name string
		g    Group
		want int
	}{
		{"exact never warns", Group{Class: Exact, Type: model.Video, Files: []model.File{f(10, 10, 5), f(20, 10, 9)}}, 0},
		{"same shape and duration", Group{Class: Visual, Type: model.Video, Files: []model.File{f(1920, 1080, 60), f(1280, 720, 60.5)}}, 0},
		{"duration differs", Group{Class: Visual, Type: model.Video, Files: []model.File{f(1920, 1080, 60), f(1920, 1080, 50)}}, 1},
		{"aspect differs", Group{Class: Visual, Type: model.Image, Files: []model.File{f(1600, 900, 0), f(1200, 1200, 0)}}, 1},
		{"both differ", Group{Class: Visual, Type: model.Video, Files: []model.File{f(1920, 1080, 60), f(1000, 1000, 40)}}, 2},
		{"unknown values ignored", Group{Class: Visual, Type: model.Image, Files: []model.File{f(0, 0, 0), f(100, 100, 0)}}, 0},
		{"duration ignored for images", Group{Class: Visual, Type: model.Image, Files: []model.File{f(10, 10, 1), f(10, 10, 9)}}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.g.Analyze()
			if len(tt.g.Warnings) != tt.want {
				t.Fatalf("warnings = %v, want %d", tt.g.Warnings, tt.want)
			}
		})
	}
}

func sized(path string, size int64) model.File { return model.File{Path: path, Size: size} }

func TestOriginalAndDuplicateBytes(t *testing.T) {
	g := Group{Files: []model.File{sized("/a", 100), sized("/b", 900), sized("/c", 300)}}
	if g.OriginalIndex() != 1 {
		t.Errorf("original = %d, want the largest (1)", g.OriginalIndex())
	}
	if got := g.DuplicateBytes(); got != 400 {
		t.Errorf("duplicate bytes = %d, want 400 (original excluded)", got)
	}
	tie := Group{Files: []model.File{sized("/a", 500), sized("/b", 500), sized("/c", 500)}}
	if tie.OriginalIndex() != 0 || tie.DuplicateBytes() != 1000 {
		t.Errorf("ties keep the first path: idx=%d bytes=%d", tie.OriginalIndex(), tie.DuplicateBytes())
	}
	if (&Group{}).DuplicateBytes() != 0 {
		t.Error("empty group")
	}
}

func TestSummarize(t *testing.T) {
	groups := []Group{
		{Files: []model.File{sized("/a", 10), sized("/b", 10)}},
		{Files: []model.File{sized("/c", 1000), sized("/d", 200), sized("/e", 50)}},
	}
	s := Summarize(groups)
	want := Summary{Groups: 2, Files: 5, Duplicates: 3, DuplicateBytes: 10 + 250}
	if s != want {
		t.Fatalf("got %+v want %+v", s, want)
	}
	if Summarize(nil) != (Summary{}) {
		t.Error("nil groups")
	}
}
