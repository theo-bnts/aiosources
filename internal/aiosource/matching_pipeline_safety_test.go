package aiosource

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSeriesMatchingPublicationAndPackScopes(t *testing.T) {
	ctx, service, conn := testService(t)
	work := model.AIOWork{Type: "tv", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Serial"}, Years: []int{2018}, SeasonYears: map[int]int{2: 2020}, EpisodeYears: map[string]int{"2:1": 2020, "2:3": 2021}}
	if err := service.DB.SaveAIOWork(ctx, work, []string{"example serial"}); err != nil {
		t.Fatal(err)
	}
	other := work
	other.TMDB, other.IMDb, other.Years = "102", "tt2345678", []int{2024}
	other.SeasonYears, other.EpisodeYears = map[int]int{1: 2024}, nil
	if err := service.DB.SaveAIOWork(ctx, other, []string{"example serial"}); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		source, title, imdb, status string
		season, episode             *int
	}{
		{"native-episode", "Example.Serial.2021.1080p", "", "matched", matchingNumber(2), matchingNumber(3)},
		{"native-pack", "Example.Serial.2020.1080p", "", "matched", matchingNumber(2), nil},
		{"split-year-pack", "Example.Serial.S02.2021.1080p", "", "matched", nil, nil},
		{"identified-pack", "Example.Serial.S01-S03.COMPLETE", "tt1234567", "matched", matchingNumber(1), nil},
		{"ambiguous-pack", "Example.Serial.S01.COMPLETE", "", "ambiguous", nil, nil},
		{"wrong-season", "Example.Serial.S02E03.2021", "", "unmatched", matchingNumber(1), matchingNumber(3)},
		{"wrong-episode-year", "Example.Serial.S02E01.2021", "tt1234567", "unmatched", nil, nil},
	} {
		fields := map[string]any{"title": tc.title, "info_hash": fmt.Sprintf("%040x", i+1)}
		if tc.imdb != "" {
			fields["imdb_id"] = tc.imdb
		}
		if tc.season != nil {
			fields["season"] = *tc.season
		}
		if tc.episode != nil {
			fields["episode"] = *tc.episode
		}
		body, _ := json.Marshal(fields)
		if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('example',$1,$2)`, tc.source, body); err != nil {
			t.Fatal(err)
		}
		if err := service.Match(ctx); err != nil {
			t.Fatal(err)
		}
		var got string
		if err := conn.QueryRow(ctx, `SELECT status FROM ingest.aio_matches WHERE source_id=$1`, tc.source).Scan(&got); err != nil || got != tc.status {
			t.Fatalf("%s: %s, want %s (%v)", tc.source, got, tc.status, err)
		}
	}
	token, err := service.Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id       string
		returned int
	}{
		{"tt1234567:2:3", 4}, {"tt1234567:3:4", 1}, {"tt1234567:4:4", 0},
	} {
		streams, err := service.Streams(ctx, "http://localhost", token, "series", tc.id)
		if err != nil || len(streams) != tc.returned {
			t.Fatalf("published %s: %d, want %d (%v)", tc.id, len(streams), tc.returned, err)
		}
	}
}

func TestSplitYearSeasonRequestDoesNotResetDeferredMetadataRetries(t *testing.T) {
	ctx, service, conn := testService(t)
	work := model.AIOWork{Type: "tv", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Serial"}, Years: []int{2018}, SeasonYears: map[int]int{2: 2020}}
	if err := service.DB.SaveAIOWork(ctx, work, []string{"example serial"}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('example','pack','{"title":"Example.Serial.S02.2021","imdb_id":"tt1234567"}')`); err != nil {
		t.Fatal(err)
	}
	if err := service.Match(ctx); err != nil {
		t.Fatal(err)
	}
	jobs, err := service.DB.AIOMetadataJobs(ctx)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("missing season evidence was not requested: %+v %v", jobs, err)
	}
	if err := service.DB.FinishAIOMetadata(ctx, jobs[0], false); err != nil {
		t.Fatal(err)
	}
	var before time.Time
	if err := conn.QueryRow(ctx, `SELECT next_attempt FROM ingest.aio_metadata_jobs`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := conn.Exec(ctx, `UPDATE ingest.torrents SET fields=jsonb_set(fields,'{size}',to_jsonb($1::int))`, i); err != nil {
			t.Fatal(err)
		}
		if err := service.Match(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var after time.Time
	var attempts int
	var version int64
	if err := conn.QueryRow(ctx, `SELECT attempts,version,next_attempt FROM ingest.aio_metadata_jobs`).Scan(&attempts, &version, &after); err != nil || attempts != 1 || version != jobs[0].Version || !after.Equal(before) {
		t.Fatalf("repeated source updates reset retry: %d %d %v %v (%v)", attempts, version, before, after, err)
	}
	work.EpisodeYears = map[string]int{"2:3": 2021}
	if err := service.DB.SaveAIOWork(ctx, work, []string{"example serial"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Match(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := conn.QueryRow(ctx, `SELECT status FROM ingest.aio_matches`).Scan(&status); err != nil || status != "matched" {
		t.Fatal("new verified episode date did not recover the pack", status, err)
	}
	if err := service.DB.SaveAIOWork(ctx, work, []string{"example serial"}); err != nil {
		t.Fatal(err)
	}
	state, err := service.DB.AIOAutomationStatus(ctx)
	if err != nil || state.MatchingPending != 0 {
		t.Fatal("identical metadata requeued matching", state, err)
	}
}
