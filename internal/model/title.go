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
