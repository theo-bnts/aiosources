package aiosource

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// A bilingual release name is useful only if every complete component is a
// known alias of the same work. Never accept an arbitrary title prefix or
// discard an unrecognised subtitle, sequel, collection or edition qualifier.
var compoundTitleSeparator = regexp.MustCompile(`\s+[-–—|/]\s+|[()]`)

func compoundTitleParts(title string) []string {
	parts := compoundTitleSeparator.Split(title, -1)
	if len(parts) < 2 || len(parts) > 8 {
		return nil
	}
	result := []string{}
	for _, part := range parts {
		part = Normalize(part)
		if part == "" {
			continue
		}
		minimum := 5
		// A complete Japanese or Chinese alias can be only two characters.
		// Both full aliases still have to belong to the same metadata work.
		if strings.ContainsFunc(part, func(r rune) bool { return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) }) {
			minimum = 2
		}
		if len([]rune(strings.ReplaceAll(part, " ", ""))) < minimum {
			return nil
		}
		result = append(result, part)
	}
	if len(result) < 2 || len(result) > 4 {
		return nil
	}
	return result
}

func exactCompoundAlias(title string, aliases []string) bool {
	parts := compoundTitleParts(title)
	if len(parts) == 0 {
		return false
	}
	normalized := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		normalized = append(normalized, Normalize(alias))
	}
	for _, part := range parts {
		if !slices.Contains(normalized, part) {
			return false
		}
	}
	return true
}

func hasExactReleaseAlias(release Release, aliases []string) bool {
	for _, alias := range aliases {
		if Normalize(alias) == release.Normalized {
			return true
		}
	}
	return exactCompoundAlias(release.Title, aliases)
}
