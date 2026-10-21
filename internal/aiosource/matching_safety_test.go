package aiosource

import (
	"fmt"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func matchingNumber(n int) *int { return &n }

func TestSourceNumberingCorroboratesSeasonAndEpisodeYears(t *testing.T) {
	candidate := Candidate{EntryID: "1", WorkID: "1", Source: "tmdb", SourceID: "101", Type: "tv", Titles: []string{"Example Serial"}, Years: []int{2018}, SeasonYears: map[int]int{2: 2020}, EpisodeYears: map[string]int{"2:3": 2021}, IDs: map[string]string{"imdb": "tt1234567"}}
	for _, tc := range []struct {
		name, title     string
		season, episode *int
		matched         bool
	}{
		{"episode metadata", "Example.Serial.2021.1080p", matchingNumber(2), matchingNumber(3), true},
		{"season metadata", "Example.Serial.2020.1080p", matchingNumber(2), nil, true},
		{"season spans calendar years", "Example.Serial.S02.2021.1080p", nil, nil, true},
		{"source pack spans calendar years", "Example.Serial.2021.1080p", matchingNumber(2), nil, true},
		{"parsed season", "Example.Serial.S02.2020.1080p", nil, nil, true},
		{"unsupported year", "Example.Serial.2024.1080p", matchingNumber(2), matchingNumber(3), false},
		{"unknown season", "Example.Serial.2020.1080p", matchingNumber(3), nil, false},
		{"contradictory season", "Example.Serial.S01E03.2018.1080p", matchingNumber(2), matchingNumber(3), false},
		{"contradictory episode", "Example.Serial.S02E02.2020.1080p", matchingNumber(2), matchingNumber(3), false},
		{"invalid numbering", "Example.Serial.2018.1080p", matchingNumber(-1), nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := ParseRelease(tc.title)
			d := ChooseMatch(p, []Candidate{candidate}, SourceIDs{}, false, SourceEpisode{tc.season, tc.episode})
			if (d.Status == "matched") != tc.matched {
				t.Fatalf("unsafe or lost correspondence: %+v, parsed %+v", d, p)
			}
		})
	}
}

func TestVerifiedIdentifierDisambiguatesYearlessSeasonPacks(t *testing.T) {
	first := Candidate{EntryID: "1", WorkID: "1", Source: "tmdb", SourceID: "101", Type: "tv", Titles: []string{"Example Serial"}, Years: []int{2018}, IDs: map[string]string{"imdb": "tt1234567", "tvdb": "201"}}
	second := Candidate{EntryID: "2", WorkID: "2", Source: "tmdb", SourceID: "102", Type: "tv", Titles: []string{"Example Serial"}, Years: []int{2024}, IDs: map[string]string{"imdb": "tt2345678", "tvdb": "202"}}
	for _, tc := range []struct {
		name    string
		ids     SourceIDs
		matched bool
	}{
		{"IMDb", SourceIDs{IMDb: "tt1234567"}, true},
		{"TMDB", SourceIDs{TMDB: "101"}, true},
		{"TVDB", SourceIDs{TVDB: "201"}, true},
		{"agreeing identifiers", SourceIDs{TMDB: "101", IMDb: "tt1234567", TVDB: "201"}, true},
		{"no identifier", SourceIDs{}, false},
		{"conflicting identifiers", SourceIDs{TMDB: "101", IMDb: "tt2345678"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := ChooseMatch(ParseRelease("Example.Serial.S01-S03.COMPLETE.1080p"), []Candidate{first, second}, tc.ids, false, SourceEpisode{})
			if (d.Status == "matched") != tc.matched || (d.Winner != nil && d.Winner.WorkID != "1") {
				t.Fatalf("wrong homonym decision: %+v", d)
			}
		})
	}
	if d := ChooseMatch(ParseRelease("Example.Serial.S01.2024"), []Candidate{first, second}, SourceIDs{IMDb: "tt1234567"}, false, SourceEpisode{}); d.Status == "matched" {
		t.Fatal("an identifier overrode an explicit conflicting year")
	}
}

func TestEpisodeMarkersDoNotConsumeDigitsInsideTitles(t *testing.T) {
	for _, title := range []string{"Zone51", "Village2", "TheEdge17"} {
		t.Run(title, func(t *testing.T) {
			p := ParseRelease(title + ".2021.1080p")
			if p.Type != "" || len(p.Episodes) != 0 || p.Normalized != Normalize(title) {
				t.Fatalf("movie title became episode numbering: %+v", p)
			}
			c := Candidate{EntryID: "1", WorkID: "1", Type: "movie", Titles: []string{title}, Years: []int{2021}}
			if d := ChooseMatch(p, []Candidate{c}, SourceIDs{}, false, SourceEpisode{}); d.Status != "matched" {
				t.Fatalf("lost complete title and year: %+v", d)
			}
		})
	}
	for _, title := range []string{"Example.S01E02", "Example.S01E02E03", "Example.S01E02-E03", "Example.S01.E02"} {
		p := ParseRelease(title)
		if !contains(p.Seasons, 1) || !contains(p.Episodes, 2) {
			t.Fatalf("lost structured numbering for %s: %+v", title, p)
		}
		if title == "Example.S01E02E03" && !contains(p.Episodes, 3) {
			t.Fatal("lost concatenated episodes")
		}
	}
	for _, title := range []string{"Example.S01.Episode03", "Example.Season01.Episode03", "Example.S01.Episode 03", "Example.S01.Ep03", "Example.S01Episode03", "Example.S01Ep03", "Example.S01.Episode02-Episode03", "Example.S01E02-Episode03", "Example.S01E02-Ep03"} {
		p := ParseRelease(title)
		if !contains(p.Seasons, 1) || !contains(p.Episodes, 3) || p.Pack {
			t.Fatalf("lost explicit word-based numbering for %s: %+v", title, p)
		}
		if _, ok := SelectFile([]model.AIOFile{{Path: title + ".mkv", Size: 10}}, Resource{Type: "series", Season: matchingNumber(1), Episode: matchingNumber(3)}); !ok {
			t.Fatal("lost explicitly numbered episode file", title)
		}
	}
}

func TestMultiSeasonPacksRetainCoveredSeasonsAndVetoContradictions(t *testing.T) {
	for _, season := range []int{1, 2, 3, 4} {
		r := Resource{Type: "series", Season: matchingNumber(season), Episode: matchingNumber(4)}
		record := model.AIORecord{Title: "Example.Serial.S01-S03.COMPLETE", Season: matchingNumber(1)}
		if got := MatchesEpisode(record, ParseRelease(record.Title), r); got != (season <= 3) {
			t.Fatalf("season %d in explicit pack: %t", season, got)
		}
	}
	for _, record := range []model.AIORecord{
		{Title: "Example.Serial.S01E02", Season: matchingNumber(2), Episode: matchingNumber(2)},
		{Title: "Example.Serial.S01E03", Season: matchingNumber(1), Episode: matchingNumber(2)},
		{Title: "Example.Serial.S01-S03.COMPLETE", Season: matchingNumber(4)},
	} {
		r := Resource{Type: "series", Season: record.Season, Episode: matchingNumber(2)}
		if MatchesEpisode(record, ParseRelease(record.Title), r) {
			t.Fatalf("contradictory listing evidence accepted: %+v", record)
		}
	}
}

func TestSpacedSeasonPacksKeepTheirExplicitPackMarker(t *testing.T) {
	for _, title := range []string{"Example Serial Season 1 - 3 COMPLETE", "Example Serial S01 - 03 Pack", "Example Serial Saison 1 - 3 Intégrale"} {
		p := ParseRelease(title)
		if !p.Pack || p.InvalidNumbering || len(p.Seasons) != 3 || len(p.Episodes) != 0 {
			t.Fatalf("spaced season range became an episode: %s: %+v", title, p)
		}
		for _, season := range []int{1, 2, 3, 4} {
			r := Resource{Type: "series", Season: matchingNumber(season), Episode: matchingNumber(4)}
			if got := MatchesEpisode(model.AIORecord{Title: title}, p, r); got != (season <= 3) {
				t.Fatalf("wrong season %d in explicit pack %s", season, title)
			}
		}
	}
}

func TestSeasonFolderQualifiesNumericEpisodeFiles(t *testing.T) {
	r := Resource{Type: "series", Season: matchingNumber(2), Episode: matchingNumber(3)}
	for _, name := range []string{"Example Serial/Season 02/03.mkv", "Example.Serial.S02/03 - Example title.mp4", `Example Serial\Season 02\03.mkv`} {
		t.Run(name, func(t *testing.T) {
			files := []model.AIOFile{{Path: name, Size: 10}, {Path: "Example Serial/Season 01/03.mkv", Size: 999}}
			if f, ok := SelectFile(files, r); !ok || f.Path != name {
				t.Fatalf("lost proven episode: %+v %t", f, ok)
			}
			if _, ok := SelectFile(append(files, model.AIOFile{Path: "Example.Serial.S02E03.mp4", Size: 20}), r); ok {
				t.Fatal("ambiguous episode accepted")
			}
		})
	}
	for _, name := range []string{"03.mkv", "Example Serial/03.mkv", "Example.Serial.S02/2021.mkv", "Example.Serial.S02/Extras/03.mkv", "Example.Serial.S02/04.mkv", "Example.Serial.S02-S03/03.mkv", "Example.Serial.S02-S03/E03.mkv", "Example.Serial.S02.S03-S01/03.mkv", "Example.Serial.S02/Season03-01/E03.mkv"} {
		if _, ok := SelectFile([]model.AIOFile{{Path: name, Size: 10}}, r); ok {
			t.Fatal(fmt.Sprintf("guessed missing, ambiguous or contradictory numbering in %s", name))
		}
	}
}

func TestMalformedRangesNeverBecomeUnscopedPacks(t *testing.T) {
	r := Resource{Type: "series", Season: matchingNumber(1), Episode: matchingNumber(3)}
	c := Candidate{WorkID: "1", EntryID: "1", Type: "tv", Titles: []string{"Example Serial"}, Years: []int{2021}}
	for _, title := range []string{"Example.Serial.S03-S01.2021", "Example.Serial.S01-S200.2021", "Example.Serial.S01E05-E03.2021", "Example.Serial.S01E001-E500.2021", "Example.Serial.S01E05-Episode03.2021", "Example.Serial.S01E001-Ep500.2021", "Example Serial - 05-03", "Example Serial - 001-500"} {
		p := ParseRelease(title)
		if d := ChooseMatch(p, []Candidate{c}, SourceIDs{}, false, SourceEpisode{}); d.Status == "matched" {
			t.Fatal("invalid range was identified", title)
		}
		if MatchesEpisode(model.AIORecord{Title: title}, p, r) {
			t.Fatal("invalid range became an unscoped stream", title)
		}
		if _, ok := SelectFile([]model.AIOFile{{Path: title + ".mkv", Size: 100}}, r); ok {
			t.Fatal("invalid file range was selected", title)
		}
	}
}
