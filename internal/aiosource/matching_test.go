package aiosource

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

func TestUnmatchedExplanationPreservesConservativeDecision(t *testing.T) {
	candidate := Candidate{EntryID: "1", WorkID: "1", Source: "tmdb", SourceID: "1", Type: "movie", Titles: []string{"Example Film"}, Years: []int{2020}, IDs: map[string]string{"imdb": "tt1234567"}}
	for _, test := range []struct {
		title  string
		ids    SourceIDs
		reason string
	}{
		{"Example.Film.2021.1080p", SourceIDs{}, "year conflict"},
		{"Example.Film.2020.1080p", SourceIDs{IMDb: "tt9999999"}, "source ID conflict"},
		{"Unrelated.Release.2020.1080p", SourceIDs{}, "no supported correspondence"},
	} {
		d := ChooseMatch(ParseRelease(test.title), []Candidate{candidate}, test.ids, false, SourceEpisode{})
		if d.Status != "unmatched" || d.Winner != nil || d.Reason != test.reason {
			t.Fatalf("unsafe or unhelpful decision: %+v for %+v", d, test)
		}
	}
}

func TestV1CorrespondenceConformance(t *testing.T) {
	b, err := os.ReadFile("testdata/v1-matching.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Matching []struct {
			Args   []json.RawMessage
			Status string
			Entry  *string
		}
		Parsing []struct {
			Title  string
			Result struct {
				Title             string
				Normalized        string
				Year              *int
				Type              *string `json:"mediaType"`
				Seasons, Episodes []int
				Pack              bool `json:"isSeasonPack"`
			}
		}
	}
	if err = json.Unmarshal(b, &golden); err != nil {
		t.Fatal(err)
	}
	for i, g := range golden.Matching {
		t.Run(fmt.Sprintf("decision_%03d", i), func(t *testing.T) {
			// Keep the original v1 export intact. Four conservative misses are
			// intentionally corrected: a verified ID resolves a yearless homonym
			// or excludes a contradictory competitor; explicit source season
			// numbering corroborates the unique, complete series title; verified
			// episode dates corroborate a season spanning two calendar years.
			expectedStatus, expectedEntry := g.Status, g.Entry
			if i == 5 || i == 70 || i == 83 || i == 111 {
				entry := "1"
				expectedStatus, expectedEntry = "matched", &entry
			}
			var p struct {
				Title             string
				Normalized        string
				Year              *int
				Type              *string `json:"mediaType"`
				Seasons, Episodes []int
				Pack              bool `json:"isSeasonPack"`
			}
			var c []Candidate
			var ids SourceIDs
			var overflow bool
			var observed SourceEpisode
			dest := []any{&p, &c, &ids, &overflow, &observed}
			for i, b := range g.Args {
				if err := json.Unmarshal(b, dest[i]); err != nil {
					t.Fatal(err)
				}
			}
			r := Release{Title: p.Title, Normalized: p.Normalized, Year: p.Year, Seasons: p.Seasons, Episodes: p.Episodes, Pack: p.Pack}
			if p.Type != nil {
				r.Type = *p.Type
			}
			d := ChooseMatch(r, c, ids, overflow, observed)
			var winner *string
			if d.Winner != nil {
				winner = &d.Winner.EntryID
			}
			if d.Status != expectedStatus || !reflect.DeepEqual(winner, expectedEntry) {
				t.Fatalf("got %s %v, want %s %v for %+v", d.Status, winner, expectedStatus, expectedEntry, r)
			}
		})
	}
	for _, g := range golden.Parsing {
		t.Run("parse_"+g.Title, func(t *testing.T) {
			p := ParseRelease(g.Title)
			r := g.Result
			if p.Title != r.Title || p.Normalized != r.Normalized || !reflect.DeepEqual(p.Year, r.Year) || !reflect.DeepEqual(p.Seasons, r.Seasons) || !reflect.DeepEqual(p.Episodes, r.Episodes) || p.Pack != r.Pack {
				t.Fatalf("got %+v, expected %+v", p, r)
			}
		})
	}
}
func TestResourceIDs(t *testing.T) {
	for _, id := range []string{"tt1234567:1:2", "tmdb:55:0:2", "imdb:1234567", "mal:21:3"} {
		if _, err := ParseID(id, "series"); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	for _, id := range []string{"tt0", "tmdb:0", "tt123:1", "mal:1:2:3", "tt1:999999999999:1", "../tt1", "kitsu:no"} {
		if _, err := ParseID(id, "series"); err == nil {
			t.Errorf("accepted %s", id)
		}
	}
}
