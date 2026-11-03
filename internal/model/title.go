package model

import (
	"golang.org/x/text/unicode/norm"
	"strings"
	"unicode"
)

// NormalizeTitle gives local and imported metadata the same alias spelling.
func NormalizeTitle(s string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// TitleSimilarity compares already normalized titles using character trigrams.
// Candidate discovery and the final decision must use the same score.
func TitleSimilarity(a, b string) float64 {
	if a == b {
		return 1
	}
	grams := func(s string) map[string]bool {
		r := []rune(s)
		out := map[string]bool{}
		for i := 0; i+2 < len(r); i++ {
			out[string(r[i:i+3])] = true
		}
		return out
	}
	x, y := grams(a), grams(b)
	if len(x)+len(y) == 0 {
		return 0
	}
	n := 0
	for g := range x {
		if y[g] {
			n++
		}
	}
	return 2 * float64(n) / float64(len(x)+len(y))
}
