package aiosource

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCompoundAliasesRequireEveryCompleteTitle(t *testing.T) {
	c := Candidate{EntryID: "1", WorkID: "1", Type: "tv", Titles: []string{"Ocean Chronicle", "Chronique Marine", "海語"}, Years: []int{2020}}
	for _, tc := range []struct {
		title   string
		matched bool
	}{
		{"Ocean Chronicle (Chronique Marine) S01E02 1080p", true},
		{"Chronique Marine - Ocean Chronicle S02 1080p", true},
		{"Ocean Chronicle (海語) S01 1080p", true},
		{"Ocean Chronicle (空語) S01 1080p", false},
		{"Ocean Chronicle (Unrelated Adventure) S01E02 1080p", false},
		{"Ocean Chronicle - Special Story S01 1080p", false},
		{"Ocean Chronicle (Chronique Marine) Extra Movie S01 1080p", false},
		{"Ocean Chronicle (Chronique Marine) 2024 S01", false},
	} {
		t.Run(tc.title, func(t *testing.T) {
			d := ChooseMatch(ParseRelease(tc.title), []Candidate{c}, SourceIDs{}, false, SourceEpisode{})
			if (d.Status == "matched") != tc.matched {
				t.Fatalf("unexpected decision: %+v", d)
			}
		})
	}
	other := c
	other.EntryID = "2"
	other.WorkID = "2"
	if d := ChooseMatch(ParseRelease("Ocean Chronicle (Chronique Marine) S01"), []Candidate{c, other}, SourceIDs{}, false, SourceEpisode{}); d.Status != "ambiguous" {
		t.Fatal("compound aliases hid a homonym", d)
	}
}

func TestStandaloneAndVersionedEpisodesDoNotPolluteTitle(t *testing.T) {
	for _, title := range []string{"Ocean Chronicle E05 SUBFRENCH 1080p", "Ocean Chronicle Ep05 1080p", "Ocean Chronicle - 05v2 [1080p]", "Ocean Chronicle 3rd Season - 05v2 [1080p]"} {
		p := ParseRelease(title)
		if p.Normalized != "ocean chronicle" || p.InvalidNumbering || !contains(p.Episodes, 5) {
			t.Fatalf("lost title/episode for %s: %+v", title, p)
		}
	}
	if p := ParseRelease("Village2.2020.1080p"); p.Normalized != "village2" || len(p.Episodes) != 0 {
		t.Fatal("consumed a title digit", p)
	}
}

func TestExplicitVideoKindCorroboratesYearlessAliasesWithoutOverridingConflicts(t *testing.T) {
	w := model.AIOWork{ID: 1, Type: "tv", TMDB: "101", Titles: []string{"Ocean Chronicle"}, Years: []int{2020}}
	find := func(f model.AIOCandidateFilter) ([]model.AIOWork, bool, error) { return []model.AIOWork{w}, false, nil }
	for _, tc := range []struct {
		title, kind string
		matched     bool
	}{
		{"Ocean Chronicle 1080p", "tv", true},
		{"Ocean Chronicle 1080p", "", false},
		{"Ocean Chronicle S01 1080p", "movie", true},
		{"Ocean Chronicle 1080p", "conflict", false},
	} {
		r, err := decideAIORecord(model.AIORecord{Title: tc.title, VideoType: tc.kind}, find)
		if err != nil || (r.Status == "matched") != tc.matched {
			t.Fatalf("invalid evidence handling: %+v %v", r, err)
		}
	}
}

func TestNativeExactAliasDoesNotOverflowOnUnrelatedFuzzyCandidates(t *testing.T) {
	w := model.AIOWork{ID: 1, Type: "tv", TMDB: "101", Titles: []string{"Example Chronicle"}, Years: []int{2020}}
	calls := 0
	find := func(f model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
		calls++
		return []model.AIOWork{w}, !f.ExactOnly, nil
	}
	d, err := decideAIORecord(model.AIORecord{Title: "Example Chronicle S01", TMDB: "101"}, find)
	if err != nil || d.Status != "matched" || calls != 1 {
		t.Fatal("native exact match lost to fuzzy overflow", d, calls, err)
	}
	d, err = decideAIORecord(model.AIORecord{Title: "Example Chronicle S01", TMDB: "102"}, find)
	if err != nil || d.Status == "matched" {
		t.Fatal("contradictory native ID bypassed verification", d, err)
	}
}
