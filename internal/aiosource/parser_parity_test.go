package aiosource

import (
	"encoding/json"
	"os"
	"testing"
)

func TestV1ParserReference(t *testing.T) {
	b, e := os.ReadFile("testdata/v1-parser.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		Title     string
		Candidate Candidate
		Status    string
		Expected  string
		Parsed    struct{ Normalized string }
	}
	if e = json.Unmarshal(b, &cases); e != nil {
		t.Fatal(e)
	}
	// Generated with the parser and chooser from tag v1. Three old misses
	// are safely resolved here: the complete title and explicit IMDb ID agree.
	different := 0
	for _, c := range cases {
		p := ParseRelease(c.Title)
		d := ChooseMatch(p, []Candidate{c.Candidate}, SourceIDs{IMDb: "tt1234567"}, false, SourceEpisode{})
		if d.Status != c.Expected {
			different++
			if different <= 144 {
				t.Logf("%q: %s vs expected=%s; normalized=%q vs %q", c.Title, d.Status, c.Expected, p.Normalized, c.Parsed.Normalized)
			}
		}
		conflict := ChooseMatch(p, []Candidate{c.Candidate}, SourceIDs{IMDb: "tt9999999"}, false, SourceEpisode{})
		if conflict.Status == "matched" {
			t.Errorf("accepted conflicting source ID for %q", c.Title)
		}
	}
	if different > 0 {
		t.Fatalf("Reference cases=%d different outcomes=%d", len(cases), different)
	}
}
