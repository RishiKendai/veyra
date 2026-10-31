package tui

import (
	"strings"
	"unicode"
)

// Match reports whether pattern fuzzy-matches text and how well. A match
// needs every pattern rune to appear in text in order, ignoring case and
// spaces in the pattern. Higher scores mean better matches: consecutive runs
// and word starts are rewarded, long texts mildly penalised. An empty pattern
// matches everything with score 0.
func Match(pattern, text string) (score int, ok bool) {
	p := []rune(strings.ToLower(strings.Join(strings.Fields(pattern), "")))
	if len(p) == 0 {
		return 0, true
	}
	t := []rune(text)
	pi, prev := 0, -2
	for i, r := range t {
		if pi == len(p) {
			break
		}
		if unicode.ToLower(r) != p[pi] {
			continue
		}
		s := 1
		if i == prev+1 {
			s += 5
		}
		if i == 0 || isSeparator(t[i-1]) {
			s += 4
		}
		score += s
		prev = i
		pi++
	}
	if pi < len(p) {
		return 0, false
	}
	return score - len(t)/16, true
}

func isSeparator(r rune) bool {
	return r == '/' || r == '.' || r == '-' || r == '_' || r == ' ' || r == '(' || r == ')'
}
