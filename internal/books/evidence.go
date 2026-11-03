package books

import (
	"regexp"
	"slices"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

var titleNumbers = regexp.MustCompile(`\p{N}+`)

func AuthorSignature(name string) string {
	words := strings.Fields(Normalize(name))
	slices.Sort(words)
	return strings.Join(words, " ")
}

// Different volume numbers or word counts cannot corroborate a fuzzy book
// title. Use this same condition before limiting candidates and when deciding.
func TitleScore(a, b string) float64 {
	if len(strings.Fields(a)) != len(strings.Fields(b)) || strings.Join(titleNumbers.FindAllString(a, -1), ",") != strings.Join(titleNumbers.FindAllString(b, -1), ",") {
		return 0
	}
	return model.TitleSimilarity(a, b)
}
