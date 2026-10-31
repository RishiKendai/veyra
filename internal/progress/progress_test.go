package progress

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestLines(t *testing.T) {
	tr := New("videos")
	tr.SetScan(1094, 956)
	l := tr.Line(tr.start.Add(5 * time.Second))
	for _, want := range []string{"Scanning folder", "1,094 files examined", "956 videos found", "5.0 s"} {
		if !strings.Contains(l, want) {
			t.Errorf("scan line %q missing %q", l, want)
		}
	}
	tr.Set(Fingerprint, 120, 956)
	l = tr.Line(time.Now())
	for _, want := range []string{"Fingerprinting videos", "120 of 956", "(12%)"} {
		if !strings.Contains(l, want) {
			t.Errorf("fingerprint line %q missing %q", l, want)
		}
	}
	tr.Set(Group, 0, 0)
	if l = tr.Line(time.Now()); !strings.Contains(l, "matches") || strings.Contains(l, " of ") {
		t.Errorf("group line %q", l)
	}
}

func TestETA(t *testing.T) {
	tr := New("images")
	tr.Set(Fingerprint, 100, 1000)
	if _, ok := tr.eta(tr.stageStart.Add(time.Second)); ok {
		t.Error("no ETA before the stage has run a few seconds")
	}
	left, ok := tr.eta(tr.stageStart.Add(10 * time.Second)) // 100 in 10 s -> 900 more = 90 s
	if !ok || left != 90*time.Second {
		t.Errorf("eta = %v %v", left, ok)
	}
	tr.Set(Fingerprint, 1000, 1000)
	if _, ok := tr.eta(tr.stageStart.Add(time.Hour)); ok {
		t.Error("no ETA when finished")
	}
	tr.Set(Fingerprint, 0, 1000)
	if _, ok := tr.eta(tr.stageStart.Add(time.Hour)); ok {
		t.Error("no ETA before the first item completes")
	}
}

func TestStageChangeRestartsClock(t *testing.T) {
	tr := New("videos")
	tr.Set(Hash, 1, 10)
	first := tr.stageStart
	tr.Set(Hash, 2, 10)
	if !tr.stageStart.Equal(first) {
		t.Error("same stage must keep its clock")
	}
	time.Sleep(2 * time.Millisecond)
	tr.Set(Fingerprint, 1, 10)
	if !tr.stageStart.After(first) {
		t.Error("new stage must restart the clock")
	}
}

func TestLoaderStartsEveryLineAndSpins(t *testing.T) {
	tr := New("videos")
	stages := []func(){
		func() {},
		func() { tr.Set(Compare, 1, 4) },
		func() { tr.Set(Hash, 1, 4) },
		func() { tr.Set(Fingerprint, 1, 4) },
		func() { tr.Set(Group, 0, 0) },
	}
	for i, set := range stages {
		set()
		l := tr.Line(tr.start.Add(10 * time.Millisecond))
		if !strings.HasPrefix(l, loader[0]+" ") {
			t.Errorf("stage %d: line must begin with the loader, got %q", i, l)
		}
	}
	if len(loader) != 10 {
		t.Fatalf("the braille spinner has 10 frames, got %d", len(loader))
	}
	var order []string
	seen := map[string]bool{}
	for i := range 2 * len(loader) {
		l := tr.Line(tr.start.Add(time.Duration(i)*frameEvery + time.Millisecond))
		frame := strings.SplitN(l, " ", 2)[0]
		seen[frame] = true
		order = append(order, frame)
	}
	if len(seen) != len(loader) {
		t.Fatalf("expected %d distinct frames, got %v", len(loader), order)
	}
	for i := len(loader); i < len(order); i++ {
		if order[i] != order[i-len(loader)] {
			t.Fatalf("loader must cycle: %v", order)
		}
	}
	for i := 1; i < len(order); i++ {
		if order[i] == order[i-1] {
			t.Fatalf("consecutive frames must differ: %v", order)
		}
	}
	for _, f := range loader {
		if w := ansi.StringWidth(f); w != 1 {
			t.Errorf("frame %q is %d cells wide, want 1", f, w)
		}
	}
}

func TestNoteReplacesTheStageUntilCleared(t *testing.T) {
	tr := New("videos")
	tr.Set(Fingerprint, 5, 10)
	tr.SetNote("Drive disconnected: waiting for /Volumes/HD1 to come back (Ctrl+C to stop)")
	l := tr.Line(tr.start.Add(65 * time.Second))
	if frame := strings.SplitN(l, " ", 2)[0]; !slices.Contains(loader[:], frame) {
		t.Errorf("note line must start with a spinner frame, got %q", frame)
	}
	for _, want := range []string{"Drive disconnected", "/Volumes/HD1", "Ctrl+C", "1m 05s"} {
		if !strings.Contains(l, want) {
			t.Errorf("note line %q missing %q", l, want)
		}
	}
	if strings.Contains(l, "Fingerprinting") {
		t.Errorf("the note must replace the stage text: %q", l)
	}
	tr.SetNote("")
	if l := tr.Line(time.Now()); !strings.Contains(l, "Fingerprinting videos: 5 of 10") {
		t.Errorf("clearing the note must bring the stage back: %q", l)
	}
}
