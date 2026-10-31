package tui

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWrapTextExamples(t *testing.T) {
	long := "/Volumes/HD1/Movies/@Vijay/www.1TamilMV.fi - Good Bad Ugly (2025) Tamil Proper HQ HDRip - 1080p - HEVC - (DD+5.1 - 192Kbps & AAC) - 1.6GB - ESub.mkv"
	for _, w := range []int{12, 20, 30, 47} {
		lines := wrapText(long, w)
		for _, l := range lines {
			if ansi.StringWidth(l) > w {
				t.Fatalf("w=%d: line %q is %d wide", w, l, ansi.StringWidth(l))
			}
		}
		if got, want := strings.ReplaceAll(strings.Join(lines, ""), " ", ""), strings.ReplaceAll(long, " ", ""); got != want {
			t.Fatalf("w=%d: text changed:\n got %q\nwant %q", w, got, want)
		}
	}
	if got := wrapText("short", 30); len(got) != 1 || got[0] != "short" {
		t.Errorf("short text: %q", got)
	}
	if got := wrapText("", 30); len(got) != 1 || got[0] != "" {
		t.Errorf("empty: %q", got)
	}
	if got := wrapText("a\nb", 30); len(got) != 2 {
		t.Errorf("newline: %q", got)
	}
	if wrapText("x", 0) != nil {
		t.Error("zero width")
	}
}

// Property: for any text, every line fits and no character is lost.
func TestWrapTextNeverLosesOrOverflows(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	alphabet := []rune("abcXYZ019 /-_.,()[]名前é  ")
	for range 3000 {
		n := rng.IntN(120)
		rs := make([]rune, n)
		for i := range rs {
			rs[i] = alphabet[rng.IntN(len(alphabet))]
		}
		s := string(rs)
		w := 4 + rng.IntN(40)
		lines := wrapText(s, w)
		for _, l := range lines {
			if ansi.StringWidth(l) > w {
				t.Fatalf("w=%d line %q too wide for input %q", w, l, s)
			}
		}
		strip := func(x string) string { return strings.ReplaceAll(x, " ", "") }
		if strip(strings.Join(lines, "")) != strip(s) {
			t.Fatalf("w=%d lost characters:\n in  %q\n out %q", w, s, lines)
		}
	}
}
