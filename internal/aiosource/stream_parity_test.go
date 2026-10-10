package aiosource

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestV1StreamPresentationAndEpisodeEligibility(t *testing.T) {
	raw, err := os.ReadFile("testdata/v1-streams.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Languages []struct {
			Title            string
			Languages, Flags []string
		}
		Episodes []struct {
			Title            string
			Season, Episode  *int
			RequestedSeason  *int   `json:"requested_season"`
			RequestedEpisode *int   `json:"requested_episode"`
			MediaType        string `json:"media_type"`
			Expected         bool
		}
		Files []struct {
			Names           []string
			Season, Episode *int
			Expected        string
		}
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	flags := regexp.MustCompile(`[\x{1F1E6}-\x{1F1FF}]{2}`)
	for _, fixture := range fixtures.Languages {
		t.Run("languages/"+fixture.Title+"/"+strings.Join(fixture.Languages, ","), func(t *testing.T) {
			stream := baseStream(model.AIORecord{Provider: "example", Title: fixture.Title, Languages: fixture.Languages}, Resource{Type: "movie", Kind: "imdb", ID: "tt1234567"}, "")
			got := flags.FindAllString(stream.Title, -1)
			if !slices.Equal(got, fixture.Flags) {
				t.Fatalf("language flags %v, want v1 %v", got, fixture.Flags)
			}
		})
	}
	for _, fixture := range fixtures.Episodes {
		t.Run("episodes/"+fixture.Title, func(t *testing.T) {
			record := model.AIORecord{Title: fixture.Title, Season: fixture.Season, Episode: fixture.Episode}
			resource := Resource{Type: fixture.MediaType, Season: fixture.RequestedSeason, Episode: fixture.RequestedEpisode}
			if got := MatchesEpisode(record, ParseRelease(record.Title), resource); got != fixture.Expected {
				t.Fatalf("episode eligibility %t, want %t", got, fixture.Expected)
			}
		})
	}
	for _, fixture := range fixtures.Files {
		t.Run("files/"+strings.Join(fixture.Names, ","), func(t *testing.T) {
			files := make([]model.AIOFile, len(fixture.Names))
			for i, name := range fixture.Names {
				files[i] = model.AIOFile{Path: name, Size: int64((i + 1) * 1000)}
			}
			file, ok := SelectFile(files, Resource{Type: "series", Season: fixture.Season, Episode: fixture.Episode})
			if fixture.Expected == "" {
				if ok {
					t.Fatalf("v1 rejected ambiguous or wrong file, got %q", file.Path)
				}
			} else if !ok || file.Path != fixture.Expected {
				t.Fatalf("file %q, selected %t, want %q", file.Path, ok, fixture.Expected)
			}
		})
	}
}

func TestBingeGroupStableAcrossEpisodesAndIdentifierAliases(t *testing.T) {
	record := model.AIORecord{Provider: "example", Title: "Example.Series.S01E02.1080p.WEB-DL", IMDb: "tt1234567", TMDB: "101", TVDB: "202", Languages: []string{"fr"}}
	var first string
	for _, id := range []string{"tt1234567:1:2", "tt1234567:1:3", "tmdb:101:1:3", "tvdb:202:2:1"} {
		resource, err := ParseID(id, "series")
		if err != nil {
			t.Fatal(err)
		}
		stream := baseStream(record, resource, "")
		if first == "" {
			first = stream.Hints.BingeGroup
		}
		if stream.Hints.BingeGroup != first {
			t.Fatalf("%s changes the binge group from %q to %q", id, first, stream.Hints.BingeGroup)
		}
	}
	for _, other := range []model.AIORecord{
		{Provider: "another", Title: record.Title, IMDb: record.IMDb, Languages: record.Languages},
		{Provider: record.Provider, Title: "Example.Series.S01E03.720p.WEB-DL", IMDb: record.IMDb, Languages: record.Languages},
		{Provider: record.Provider, Title: record.Title, IMDb: record.IMDb, Languages: []string{"en"}},
	} {
		if got := baseStream(other, Resource{Type: "series", Kind: "imdb", ID: record.IMDb}, "").Hints.BingeGroup; got == first {
			t.Fatalf("different source, quality or language reuses the binge group: %+v", other)
		}
	}
}
