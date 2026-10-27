package aiosource

import (
	"slices"
	"strings"

	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func ParseBookRelease(r model.AIORecord) books.Release {
	h := r.BookHints
	if h.Title != "" && h.Kind == "" {
		h.Kind = "book"
	}
	p := books.Parse(r.Title, books.Hints{OpenLibrary: h.OpenLibrary, ISBNs: h.ISBNs, Kind: h.Kind, Author: h.Author, ISBN: h.ISBN, Format: h.Format, Categories: h.Categories, CategoryNames: h.CategoryNames})
	if p.Kind != "" && h.Title != "" {
		p.Title = h.Title
		p.Alternatives = nil
		p.Variants = nil
		p.StrippedYear = false
	}
	return p
}

func authorSignature(s string) string {
	v := strings.Fields(Normalize(s))
	slices.Sort(v)
	return strings.Join(v, " ")
}

func decideBook(r model.AIORecord, p books.Release, find store.AIOCandidateSearch) (model.AIOMatchResult, error) {
	r.Normalized = Normalize(p.Title)
	result := model.AIOMatchResult{Record: r, Status: "unmatched", Reason: "insufficient bibliographic evidence"}
	policy := r.MatchingPolicy
	if policy == (model.MatchingPolicy{}) {
		policy = model.DefaultMatchingPolicy()
	}
	if !policy.Valid() {
		result.Reason = "invalid identification policy"
		return result, nil
	}
	minimum := max(.94, float64(policy.MinimumSimilarity)/100)
	margin := float64(policy.AmbiguityMargin) / 100
	if r.BookInformationPending {
		result.Reason = "book information not ready"
		return result, nil
	}
	if len([]rune(r.Normalized)) < 2 && p.ISBN == "" && p.OpenLibrary == "" {
		return result, nil
	}
	if p.InvalidReference {
		result.Reason = "invalid Open Library identifier"
		return result, nil
	}
	if p.InvalidISBN {
		result.Reason = "conflicting or invalid ISBN evidence"
		return result, nil
	}
	if p.Collection {
		result.Reason = "book collection requires review"
		return result, nil
	}
	versions := []string{r.Normalized}
	for _, title := range p.Variants {
		if n := Normalize(title); n != "" && !slices.Contains(versions, n) {
			versions = append(versions, n)
		}
	}
	titles := append([]string{}, versions...)
	for _, title := range versions {
		words := strings.Fields(title)
		if len(words) > 30 {
			continue
		}
		for i := 1; i < len(words); i++ {
			for _, part := range []string{strings.Join(words[:i], " "), strings.Join(words[i:], " ")} {
				if len([]rune(part)) >= 3 && !slices.Contains([]string{"the", "and", "les", "des", "une", "der", "die", "das"}, part) && !slices.Contains(titles, part) {
					titles = append(titles, part)
				}
			}
		}
	}
	var year *int
	if p.StrippedYear && p.Year > 0 {
		year = &p.Year
	}
	works, overflow, err := find(model.AIOCandidateFilter{Type: "book", Title: r.Normalized, Titles: titles, ISBN: p.ISBN, OpenLibrary: p.OpenLibrary, Year: year, ExactOnly: len([]rune(r.Normalized)) < 5})
	if err != nil {
		return result, err
	}
	for _, title := range p.Alternatives {
		if p.ISBN != "" || p.OpenLibrary != "" {
			break
		}
		more, full, e := find(model.AIOCandidateFilter{Type: "book", Title: Normalize(title), Year: year})
		if e != nil {
			return result, e
		}
		overflow = overflow || full
		for _, w := range more {
			if !slices.ContainsFunc(works, func(old model.AIOWork) bool { return old.ID == w.ID }) {
				works = append(works, w)
			}
		}
	}
	var winners []model.AIOWork
	strengths, nearby := map[int64]float64{}, map[int64]float64{}
	for _, w := range works {
		if w.Type != "book" {
			continue
		}
		verifiedID := p.ISBN != "" || p.OpenLibrary != ""
		authorNames := append(append([]string{}, w.Book.Authors...), w.Book.AuthorAliases...)
		author := p.Author == ""
		for _, a := range authorNames {
			author = author || authorSignature(a) == authorSignature(p.Author)
		}
		eligible := verifiedID && author
		if eligible {
			strengths[w.ID] = 1
		}
		for vi, title := range versions {
			// A trailing year can be an annual volume rather than a release
			// annotation. Do not drop it without independent corroboration.
			if vi == 0 && p.StrippedYear && !w.Book.YearVerified && !verifiedID {
				continue
			}
			exact, score, joined := false, 0.0, false
			for _, alias := range w.Titles {
				n := Normalize(alias)
				if len(strings.Fields(title)) == len(strings.Fields(n)) && strings.Join(numberRE.FindAllString(title, -1), ",") == strings.Join(numberRE.FindAllString(n, -1), ",") {
					score = max(score, similarity(title, n))
				}
				exact = exact || title == n
				for _, a := range authorNames {
					na := Normalize(a)
					joined = joined || title == n+" "+na || title == na+" "+n
				}
			}
			if joined || exact && author {
				strengths[w.ID] = 1
			}
			if author {
				nearby[w.ID] = max(nearby[w.ID], score)
			}
			eligible = eligible || joined || exact && author && (len([]rune(title)) >= 5 || p.Author != "" && len([]rune(title)) >= 2)
			eligible = eligible || p.Author != "" && author && score >= minimum && len([]rune(title)) >= 12
		}
		// Reversed author/title filenames require exact title and author.
		if !eligible && (!p.StrippedYear || w.Book.YearVerified) {
			for _, a := range authorNames {
				if authorSignature(a) != authorSignature(p.Title) {
					continue
				}
				for _, alt := range p.Alternatives {
					for _, title := range w.Titles {
						if Normalize(title) == Normalize(alt) {
							eligible = true
							strengths[w.ID] = 1
						}
					}
				}
			}
		}
		if eligible {
			strengths[w.ID] = max(strengths[w.ID], nearby[w.ID])
			winners = append(winners, w)
		}
	}
	if len(winners) == 1 && p.ISBN == "" && p.OpenLibrary == "" {
		for id, score := range nearby {
			if id != winners[0].ID && strengths[winners[0].ID]-score < margin {
				overflow = true
				break
			}
		}
	}
	if overflow || len(winners) > 1 {
		result.Status = "ambiguous"
		result.Reason = "multiple or incomplete book candidates"
		return result, nil
	}
	if len(winners) == 1 {
		result.Status = "matched"
		result.Reason = "book title corroborated by bibliographic evidence"
		if p.ISBN != "" {
			result.Reason = "verified ISBN"
		} else if p.OpenLibrary != "" {
			result.Reason = "verified Open Library identifier"
		}
		result.Work = &winners[0]
	}
	return result, nil
}

func BookSuggestion(r model.AIORecord) (string, *int, bool) {
	p := ParseBookRelease(r)
	if p.Kind == "" {
		return "", nil, false
	}
	var year *int
	if p.Year > 0 {
		year = &p.Year
	}
	return p.Title, year, true
}
