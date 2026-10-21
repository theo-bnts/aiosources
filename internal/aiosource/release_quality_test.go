package aiosource

import (
	"encoding/json"
	"github.com/theo-bnts/aiosources/internal/model"
	"os"
	"slices"
	"testing"
)

// These independently labelled synthetic examples cover the fields used by
// matching. They are not an estimate of accuracy on arbitrary release names.
func TestReleaseQualityExamples(t *testing.T) {
	body, err := os.ReadFile("testdata/parser-quality.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Title, Normalized string
		Year              *int
		Seasons, Episodes []int
		Pack              bool
	}
	if err := json.Unmarshal(body, &cases); err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		t.Run(test.Title, func(t *testing.T) {
			got := ParseRelease(test.Title)
			yearOK := got.Year == nil && test.Year == nil || got.Year != nil && test.Year != nil && *got.Year == *test.Year
			if got.Normalized != test.Normalized || !yearOK || !slices.Equal(got.Seasons, test.Seasons) || !slices.Equal(got.Episodes, test.Episodes) || got.Pack != test.Pack || got.InvalidNumbering {
				t.Fatalf("got %+v, expected %+v", got, test)
			}
		})
	}
}

func TestRelativeEpisodesDoNotPublishAbsoluteAnnotations(t *testing.T) {
	for _, title := range []string{"Example.Serial.S02E03.EP1158", "Example.Serial.S02E03 - Episode1158", "Example.Serial.S02 - 03"} {
		record := model.AIORecord{Title: title}
		for _, episode := range []int{3, 4, 1158} {
			resource := Resource{Type: "series", Season: matchingNumber(2), Episode: &episode}
			if got := MatchesEpisode(record, ParseRelease(title), resource); got != (episode == 3) {
				t.Fatalf("wrong relative episode %d for %s", episode, title)
			}
		}
	}
}
