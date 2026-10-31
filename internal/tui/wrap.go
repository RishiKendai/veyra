package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func runeWidth(r rune) int { return ansi.StringWidth(string(r)) }

// isBreakAfter reports characters after which a path or title may wrap.
func isBreakAfter(r rune) bool {
	switch r {
	case ' ', '/', '-', '_', ',', '.', ')', ']', '(', '[':
		return true
	}
	return false
}

// wrapText breaks s into lines of at most w display cells. It prefers to break
// after spaces and path/title punctuation, and breaks inside a long word only
// when it has to. Nothing is dropped except the single space at a break, so the
// full text is always recoverable; an explicit "\n" starts a new line.
func wrapText(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		lines = append(lines, wrapParagraph(para, w)...)
	}
	return lines
}

func wrapParagraph(s string, w int) []string {
	rs := []rune(s)
	if len(rs) == 0 {
		return []string{""}
	}
	var lines []string
	emit := func(from, to int) {
		lines = append(lines, strings.TrimRight(string(rs[from:to]), " "))
	}
	start, width, lastBreak := 0, 0, -1
	for i := 0; i < len(rs); i++ {
		rw := runeWidth(rs[i])
		for width+rw > w && i > start {
			if rs[i] == ' ' { // a space that does not fit is simply the break
				emit(start, i)
				start, width, lastBreak = i+1, 0, -1
				break
			}
			cut := i
			if lastBreak > start {
				cut = lastBreak
			}
			emit(start, cut)
			start, width, lastBreak = cut, 0, -1
			for start < len(rs) && rs[start] == ' ' && start < i {
				start++
			}
			for j := start; j < i; j++ {
				width += runeWidth(rs[j])
			}
		}
		if i < start { // the space we just swallowed
			continue
		}
		width += rw
		if isBreakAfter(rs[i]) {
			lastBreak = i + 1
		}
	}
	if start < len(rs) {
		emit(start, len(rs))
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}
