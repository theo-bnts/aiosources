package aiosource

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestBookDecisionsRequireConsistentBibliographicEvidence(t *testing.T) {
	base := model.AIOWork{ID: 1, Type: "book", OpenLibrary: "OL100W", DisplayTitle: "The Amber Orchard", Titles: []string{"The Amber Orchard"}, Book: model.BookInformation{Authors: []string{"Morgan Vale"}}}
	for _, tc := range []struct {
		name   string
		work   model.AIOWork
		status string
	}{
		{"Morgan Vale - The Amber Orchard EPUB", base, "matched"},
		{"The Amber Orchard - Morgan Vale EPUB", base, "matched"},
		{"Morgan.Vale.The.Amber.Orchard.FR.EPUB", base, "matched"},
		{"Other Author - The Amber Orchard EPUB", base, "unmatched"},
		{"Morgan Vale - The Amber Orchard English EPUB", base, "unmatched"},
		{"Morgan Vale - The Amber Orchard Volume 2 EPUB", base, "unmatched"},
		{"Morgan Vale - The Amber Orchard 2024 EPUB", base, "unmatched"},
		{"Morgan Vale - The Amber Orchard Tomes 1-4 EPUB", base, "unmatched"},
		{"Morgan Vale - Learning French EPUB", model.AIOWork{ID: 2, Type: "book", Titles: []string{"Learning French"}, Book: base.Book}, "matched"},
		{"Morgan Vale - Annual Review 2024 FR EPUB", model.AIOWork{ID: 3, Type: "book", Titles: []string{"Annual Review 2024"}, Book: base.Book}, "matched"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := model.AIORecord{Title: tc.name}
			p := ParseBookRelease(record)
			result, err := decideBook(record, p, func(model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
				return []model.AIOWork{tc.work}, false, nil
			})
			if err != nil || result.Status != tc.status {
				t.Fatalf("unexpected book decision: %+v %v", result, err)
			}
		})
	}
	record := model.AIORecord{Title: "Morgan Vale - The Amber Orchard EPUB"}
	p := ParseBookRelease(record)
	result, err := decideBook(record, p, func(model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
		other := base
		other.ID = 2
		return []model.AIOWork{base, other}, false, nil
	})
	if err != nil || result.Status != "ambiguous" {
		t.Fatal("homonyms were guessed", err)
	}
	record.BookInformationPending = true
	result, err = decideBook(record, p, func(model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
		t.Fatal("partial initial catalog used")
		return nil, false, nil
	})
	if err != nil || result.Status == "matched" {
		t.Fatal("partial book catalog published a match")
	}
}

func TestBooksRespectConservativeIdentificationSettings(t *testing.T) {
	work := model.AIOWork{ID: 1, Type: "book", Titles: []string{"The Amber Orchard"}, Book: model.BookInformation{Authors: []string{"Morgan Vale"}}}
	record := model.AIORecord{Title: "Morgan Vale - The Amber Orchad EPUB", MatchingPolicy: model.MatchingPolicy{MinimumSimilarity: 100, AmbiguityMargin: 8}}
	result, err := decideBook(record, ParseBookRelease(record), func(model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
		return []model.AIOWork{work}, false, nil
	})
	if err != nil || result.Status == "matched" {
		t.Fatal("book matching ignored the configured minimum", err)
	}
	record.Title = "Morgan Vale - The Amber Orchard EPUB"
	record.MatchingPolicy = model.MatchingPolicy{MinimumSimilarity: 94, AmbiguityMargin: 25}
	other := work
	other.ID = 2
	other.Titles = []string{"The Amber Orchards"}
	result, err = decideBook(record, ParseBookRelease(record), func(model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
		return []model.AIOWork{work, other}, false, nil
	})
	if err != nil || result.Status != "ambiguous" {
		t.Fatal("book matching ignored nearby homonyms", err)
	}
}
