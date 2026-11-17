package model

import (
	"golang.org/x/text/unicode/norm"
	"strings"
	"unicode"
)

// NormalizeTitle gives local and imported metadata the same alias spelling.
func NormalizeTitle(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	var b strings.Builder
	b.Grow(len(s))
	space := false
	if ascii {
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
				if space && b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteByte(c)
				space = false
			} else {
				space = true
			}
		}
		return b.String()
	}
	for _, r := range norm.NFKD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		} else {
			space = true
		}
	}
	return b.String()
}

func trigrams(s string, buf []uint64) []uint64 {
	var r0, r1 rune
	n := 0
	for _, r2 := range s {
		if n >= 2 {
			g := uint64(r0)<<42 | uint64(r1)<<21 | uint64(r2)
			dup := false
			for _, existing := range buf {
				if existing == g {
					dup = true
					break
				}
			}
			if !dup {
				buf = append(buf, g)
			}
		}
		r0, r1 = r1, r2
		n++
	}
	return buf
}

// TitleSimilarity compares already normalized titles using character trigrams.
// Candidate discovery and the final decision must use the same score.
func TitleSimilarity(a, b string) float64 {
	if a == b {
		return 1
	}
	var bufA, bufB [64]uint64
	x := trigrams(a, bufA[:0])
	y := trigrams(b, bufB[:0])
	if len(x)+len(y) == 0 {
		return 0
	}
	n := 0
	for _, gx := range x {
		for _, gy := range y {
			if gx == gy {
				n++
				break
			}
		}
	}
	return 2 * float64(n) / float64(len(x)+len(y))
}
